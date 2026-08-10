package config

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// EnvLayer is one ordered layer of "KEY=VALUE" environment entries plus a
// provenance label used to track which layer a key ultimately came from.
type EnvLayer struct {
	Name string   // provenance label, e.g. "shell", "runtime.env", "env_file", "app.env", "request.env"
	Vars []string // "KEY=VALUE" entries, lower layers first
}

// MergeEnv merges environment layers left to right. Later layers override
// earlier ones for the same key. The returned merged slice is "KEY=VALUE"
// entries in first-seen key order (the order a key first appears across
// layers), with each key appearing exactly once. source maps each key to the
// Name of the layer that won for it. Keys are case-sensitive; entries with an
// empty key (e.g. "=x") or without '=' are skipped; values are preserved
// verbatim (no expansion).
func MergeEnv(layers ...EnvLayer) (merged []string, source map[string]string) {
	var order []string
	values := make(map[string]string)
	source = make(map[string]string)
	for _, layer := range layers {
		for _, kv := range layer.Vars {
			eq := strings.IndexByte(kv, '=')
			if eq <= 0 { // no '=' or empty key
				continue
			}
			key := kv[:eq]
			if _, seen := values[key]; !seen {
				order = append(order, key)
			}
			values[key] = kv[eq+1:]
			source[key] = layer.Name
		}
	}
	merged = make([]string, 0, len(order))
	for _, key := range order {
		merged = append(merged, key+"="+values[key])
	}
	return merged, source
}

// MergeEnvVars merges environment slices without tracking provenance. Each
// slice is wrapped as EnvLayer{Name: "layerN"} (1-indexed) and only the merged
// result is returned.
func MergeEnvVars(layers ...[]string) []string {
	envLayers := make([]EnvLayer, 0, len(layers))
	for i, vars := range layers {
		envLayers = append(envLayers, EnvLayer{Name: fmt.Sprintf("layer%d", i+1), Vars: vars})
	}
	merged, _ := MergeEnv(envLayers...)
	return merged
}

// CaptureShellEnv runs `shell -lic 'env -0'` to capture the login-shell
// environment as "KEY=VALUE" entries. If shell is empty, "/bin/sh" is used.
// The timeout bounds the whole capture; a non-positive timeout defaults to 3
// seconds. On any failure (exec error, nonzero exit, parse error) it returns
// nil, err; the caller decides whether to fall back.
func CaptureShellEnv(shell string, timeout time.Duration) ([]string, error) {
	if shell == "" {
		shell = "/bin/sh"
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, "-lic", "env -0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("capture shell env: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("capture shell env: %w", err)
	}
	return ParseNulEnv(out)
}

// ParseNulEnv parses NUL-separated "KEY=VALUE" environment data (e.g. the
// output of `env -0` or the contents of /proc/<pid>/environ). Empty chunks
// and entries without '=' (or with an empty key) are skipped.
func ParseNulEnv(data []byte) ([]string, error) {
	var out []string
	for _, chunk := range bytes.Split(data, []byte{0}) {
		if len(chunk) == 0 {
			continue
		}
		kv := string(chunk)
		if eq := strings.IndexByte(kv, '='); eq <= 0 {
			continue
		}
		out = append(out, kv)
	}
	return out, nil
}

// secretKeyPattern matches environment variable names whose values should be
// redacted in logs. Matching is case-insensitive.
var secretKeyPattern = regexp.MustCompile(`(?i)(secret|token|password|passwd|credential|api[_-]?key|access[_-]?key|private[_-]?key|auth[_-]?token)`)

// RedactEnv replaces the values of entries whose key matches a secret pattern
// with "***". When reveal is true the input slice is returned unchanged with a
// count of zero. Keys that do not match pass through verbatim.
func RedactEnv(vars []string, reveal bool) (redacted []string, n int) {
	if reveal {
		return vars, 0
	}
	redacted = make([]string, len(vars))
	copy(redacted, vars)
	for i, kv := range redacted {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		key := kv[:eq]
		if secretKeyPattern.MatchString(key) {
			redacted[i] = key + "=***"
			n++
		}
	}
	return redacted, n
}
