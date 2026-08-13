// Package integration exercises the full runtime against real application
// runtimes (Node, Next.js stand-in, Django stand-in, Java single-file app).
// Tests are skipped when a required toolchain is missing or with -short.
package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

type appCase struct {
	name    string
	dir     string
	command string
	args    []string
	ready   string
	tool    string
}

var cases = []appCase{
	{
		name: "node/npm", dir: "node-app", command: "npm", args: []string{"run", "dev"},
		ready: "listening on", tool: "node",
	},
	{
		name: "nextjs", dir: "nextjs-app", command: "npm", args: []string{"run", "dev"},
		ready: "Ready in", tool: "node",
	},
	{
		name: "django", dir: "django-app", command: "python3", args: []string{"manage.py", "runserver"},
		ready: "Starting development server", tool: "python3",
	},
	{
		name: "java", dir: "java-app", command: "java", args: []string{"App.java"},
		ready: "Started DemoApplication", tool: "java",
	},
}

func toolAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func TestExampleApps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}
	examplesDir := filepath.Join("..", "..", "examples")
	skipped := 0
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if !toolAvailable(tc.tool) {
				t.Skipf("%s not available", tc.tool)
			}
			appDir := filepath.Join(examplesDir, tc.dir)
			loaded, err := config.LoadFrom(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			// Make the runtime resolve relative workdirs against the app dir.
			loaded.ProjectDir = appDir
			rt := runtime.New(loaded, nil)
			t.Cleanup(func() { rt.Shutdown() })

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			// 1. start
			res, err := rt.Start(ctx, api.StartRequest{Command: tc.command, Args: tc.args})
			if err != nil {
				t.Fatal(err)
			}
			if res.Status != "running" {
				t.Fatalf("status = %q, want running", res.Status)
			}
			st, _ := rt.Status(res.ProcessID)
			if st.PID <= 0 {
				t.Fatalf("no pid: %+v", st)
			}

			// 2. wait_for_log ready (profile readiness patterns)
			w, err := rt.WaitForLog(ctx, api.WaitForLogRequest{
				ProcessID: res.ProcessID, Contains: tc.ready, TimeoutMS: 20000,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !w.Matched || w.Entry == nil {
				t.Fatalf("ready not matched: %+v", w)
			}
			t.Logf("ready line: %s [%s]", w.Entry.Line, w.Entry.Stream)

			// 3. wait_for_log with ready=true uses the detected profile
			wp, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{
				ProcessID: res.ProcessID, Ready: true, TimeoutMS: 20000,
			})
			if err != nil || !wp.Matched {
				t.Fatalf("profile-ready wait failed: %v %+v", err, wp)
			}

			// 4. get_logs
			logs, err := rt.GetLogs(api.GetLogsRequest{ProcessID: res.ProcessID, Lines: 50})
			if err != nil {
				t.Fatal(err)
			}
			if len(logs.Entries) == 0 {
				t.Fatal("no captured logs")
			}
			if logs.AvailableLines != len(logs.Entries) {
				t.Fatalf("available=%d returned=%d", logs.AvailableLines, len(logs.Entries))
			}

			// 5. restart preserves identity, logs accumulate, ready again
			firstInstance := res.InstanceID
			if err := rt.Restart(context.Background(), res.ProcessID); err != nil {
				t.Fatal(err)
			}
			st2, _ := rt.Status(res.ProcessID)
			if st2.InstanceID == firstInstance {
				t.Fatal("instance id unchanged after restart")
			}
			w2, err := rt.WaitForLog(context.Background(), api.WaitForLogRequest{
				ProcessID: res.ProcessID, Contains: tc.ready, TimeoutMS: 20000,
			})
			if err != nil || !w2.Matched {
				t.Fatalf("ready after restart failed: %v %+v", err, w2)
			}

			// 6. stop
			if err := rt.Stop(context.Background(), res.ProcessID); err != nil {
				t.Fatal(err)
			}
			st3, _ := rt.Status(res.ProcessID)
			if st3.Status != "stopped" {
				t.Fatalf("status = %q, want stopped", st3.Status)
			}
		})
	}
	if skipped == len(cases) {
		t.Skip("no toolchains available")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

// newRuntimeFromFixture loads agent-runtime.yaml from
// internal/integration/testdata/<fixture> (resolved relative to the package
// dir, which go test sets to the test's own directory) and returns a runtime
// bound to it. Fixture configs pin shell_env: none so env resolution is
// deterministic.
func newRuntimeFromFixture(t *testing.T, fixture string) *runtime.Runtime {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	rt := runtime.New(loaded, nil)
	t.Cleanup(func() { rt.Shutdown() })
	return rt
}

// waitRunning polls process_status until the process reports running.
func waitRunning(t *testing.T, rt *runtime.Runtime, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := rt.Status(id)
		if st.Status == "running" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %s did not reach running", id)
}

// cleanupProcess stops (if running) and removes a process so nothing leaks.
func cleanupProcess(rt *runtime.Runtime, id string) {
	rt.RemoveProcess(id, true)
}

// TestMonorepoIsolation runs two apps with nested workdirs, each with its own
// .env file setting SERVICE to a different value, plus a runtime.env shared
// default. Each app must see its own SERVICE and the shared default, and
// get_process_env must agree (spec mode, provenance included).
func TestMonorepoIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}
	rt := newRuntimeFromFixture(t, "monorepo")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	apiRes, err := rt.Start(ctx, api.StartRequest{App: "api"})
	if err != nil {
		t.Fatal(err)
	}
	workerRes, err := rt.Start(ctx, api.StartRequest{App: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupProcess(rt, apiRes.ProcessID)
		cleanupProcess(rt, workerRes.ProcessID)
	})

	// Each app sees its own SERVICE from its own .env...
	wa, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: apiRes.ProcessID, Contains: "SERVICE=api", TimeoutMS: 10000})
	if err != nil || !wa.Matched {
		t.Fatalf("api app missing SERVICE=api: %v %+v", err, wa)
	}
	ww, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: workerRes.ProcessID, Contains: "SERVICE=worker", TimeoutMS: 10000})
	if err != nil || !ww.Matched {
		t.Fatalf("worker app missing SERVICE=worker: %v %+v", err, ww)
	}
	// ...and both inherit the runtime.env shared default.
	for id, name := range map[string]string{apiRes.ProcessID: "api", workerRes.ProcessID: "worker"} {
		ws, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: id, Contains: "SHARED=global-default", TimeoutMS: 10000})
		if err != nil || !ws.Matched {
			t.Fatalf("%s app missing SHARED=global-default: %v %+v", name, err, ws)
		}
	}

	// get_process_env (spec mode) agrees: SERVICE comes from each app's own
	// env_file layer; workdirs resolve to each app's nested directory.
	envAPI, err := rt.ProcessEnv(api.ProcessEnvRequest{ProcessID: apiRes.ProcessID})
	if err != nil {
		t.Fatal(err)
	}
	envWorker, err := rt.ProcessEnv(api.ProcessEnvRequest{ProcessID: workerRes.ProcessID})
	if err != nil {
		t.Fatal(err)
	}
	assertEnvHas(t, envAPI, "SERVICE=api", "SHARED=global-default")
	assertEnvHas(t, envWorker, "SERVICE=worker", "SHARED=global-default")
	if envAPI.Source["SERVICE"] != "env_file" || envWorker.Source["SERVICE"] != "env_file" {
		t.Fatalf("SERVICE provenance = %q/%q, want env_file/env_file",
			envAPI.Source["SERVICE"], envWorker.Source["SERVICE"])
	}
	for id, want := range map[string]string{apiRes.ProcessID: "api", workerRes.ProcessID: "worker"} {
		st, err := rt.Status(id)
		if err != nil {
			t.Fatal(err)
		}
		wantAbs, err := filepath.Abs(filepath.Join("testdata", "monorepo", "services", want))
		if err != nil {
			t.Fatal(err)
		}
		wantDir, err := filepath.EvalSymlinks(wantAbs)
		if err != nil {
			t.Fatal(err)
		}
		if st.WorkDir != wantDir {
			t.Fatalf("%s workdir = %q, want %q", want, st.WorkDir, wantDir)
		}
	}
}

// assertEnvHas fails the test unless env contains every listed entry.
func assertEnvHas(t *testing.T, env *api.ProcessEnvResult, want ...string) {
	t.Helper()
	for _, w := range want {
		found := false
		for _, kv := range env.Env {
			if kv == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("env %v missing %q", env.Env, w)
		}
	}
}

// TestEnvPrecedenceEndToEnd exercises the full precedence chain through a
// fixture app: request env beats app env beats runtime.env.
func TestEnvPrecedenceEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}
	rt := newRuntimeFromFixture(t, "precedence")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// request env wins over the app env block and runtime.env.
	res1, err := rt.Start(ctx, api.StartRequest{App: "echo", Env: []string{"PREC=req"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupProcess(rt, res1.ProcessID) })
	w1, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res1.ProcessID, Contains: "PREC=req", TimeoutMS: 10000})
	if err != nil || !w1.Matched {
		t.Fatalf("request env did not win: %v %+v", err, w1)
	}
	env1, err := rt.ProcessEnv(api.ProcessEnvRequest{ProcessID: res1.ProcessID})
	if err != nil {
		t.Fatal(err)
	}
	assertEnvHas(t, env1, "PREC=req")
	if env1.Source["PREC"] != "request.env" {
		t.Fatalf("PREC provenance = %q, want request.env", env1.Source["PREC"])
	}

	// Without a request override the app env block wins over runtime.env.
	res2, err := rt.Start(ctx, api.StartRequest{App: "echo"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupProcess(rt, res2.ProcessID) })
	w2, err := rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res2.ProcessID, Contains: "PREC=app", TimeoutMS: 10000})
	if err != nil || !w2.Matched {
		t.Fatalf("app env did not win without request env: %v %+v", err, w2)
	}
	env2, err := rt.ProcessEnv(api.ProcessEnvRequest{ProcessID: res2.ProcessID})
	if err != nil {
		t.Fatal(err)
	}
	assertEnvHas(t, env2, "PREC=app")
	if env2.Source["PREC"] != "app.env" {
		t.Fatalf("PREC provenance = %q, want app.env", env2.Source["PREC"])
	}
}
