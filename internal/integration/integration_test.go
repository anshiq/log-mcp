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
