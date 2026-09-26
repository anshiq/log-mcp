package server

import (
	"context"
	"os"
	"testing"
	"time"

	"agent-runtime/pkg/client"
)

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
