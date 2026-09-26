package project

import (
	"strings"
	"testing"
)

// FuzzNormalizeRemote: never panics; output never contains credentials,
// a scheme, or a .git suffix.
func FuzzNormalizeRemote(f *testing.F) {
	f.Add("git@github.com:owner/repo.git")
	f.Add("https://user:pass@github.com/o/r.git")
	f.Add("not a remote at all \n\x00")
	f.Fuzz(func(t *testing.T, raw string) {
		got := NormalizeRemote(raw)
		if got == "" {
			return
		}
		for _, bad := range []string{"://", "@", "\n", " "} {
			if strings.Contains(got, bad) {
				t.Fatalf("NormalizeRemote(%q) = %q contains %q", raw, got, bad)
			}
		}
		if strings.HasSuffix(got, ".git") {
			t.Fatalf("NormalizeRemote(%q) = %q keeps .git suffix", raw, got)
		}
		if !strings.Contains(got, "/") {
			t.Fatalf("NormalizeRemote(%q) = %q has no host/path split", raw, got)
		}
	})
}
