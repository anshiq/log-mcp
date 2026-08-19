package config

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParseEnvFile feeds arbitrary bytes through ParseEnvFile to ensure it never
// panics on malformed dotenv content.
func FuzzParseEnvFile(f *testing.F) {
	seeds := []string{
		"FOO=bar\nBAZ=qux\n",
		"# comment\n\nKEY=\"quoted value\"\n",
		"NO_EQ\n=empty\n=notempty\n",
		"KEY=val with spaces  \n",
		"",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".env")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		_, _ = ParseEnvFile(path)
	})
}

// FuzzParseNulEnv feeds arbitrary bytes through ParseNulEnv (the `env -0`
// parser) to ensure it never panics.
func FuzzParseNulEnv(f *testing.F) {
	seeds := []string{
		"HOME=/root\x00PATH=/usr/bin\x00",
		"\x00\x00KEY=VALUE\x00",
		"NO_EQ\x00=emptykey\x00",
		"",
		"A=1\x00A=2\x00",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseNulEnv(data)
	})
}
