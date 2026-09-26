package server

import (
	"context"
	"os"
	"testing"
	"time"

	"agent-runtime/pkg/client"
)

// TestGetSchema_IndependentOfWorkingDirectory is the regression test for
// B8: GetSchema used to os.ReadFile a path relative to the daemon's cwd
// (docs/schema/agent-runtime.v3.json), so any installed daemon not run
// from inside the repo checkout silently got a 2-property stub instead of
// the real schema. The schema is now embedded at compile time, so it must
// come back complete even when cwd is somewhere with no such file.
func TestGetSchema_IndependentOfWorkingDirectory(t *testing.T) {
	eng, done := testEngine(t)
	defer done()

	elsewhere := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(elsewhere); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	cl := client.New(sockPath(t, eng), "test/cli")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var out struct {
		Schema map[string]any `json:"schema"`
	}
	if err := cl.Call(ctx, "ConfigService", "GetSchema", map[string]any{}, &out); err != nil {
		t.Fatalf("GetSchema: %v", err)
	}
	props, ok := out.Schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties: %v", out.Schema)
	}
	for _, key := range []string{"version", "project", "runtime", "apps", "logging", "alerts"} {
		if _, ok := props[key]; !ok {
			t.Errorf("schema.properties missing %q (stub fallback?)", key)
		}
	}
}
