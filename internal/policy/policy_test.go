package policy

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"agent-runtime/internal/config"
)

// restricted returns a restricted SecurityConfig the way a yaml file would
// produce it after defaults: pointers left nil mean "not restricted".
func restricted() config.SecurityConfig {
	return config.SecurityConfig{
		ModeValue:       config.RestrictedMode,
		AllowedCommands: []string{"go", "npm", "./mvnw", "python"},
		AllowedWorkdirs: []string{"${project}"},
	}
}

func TestTrustedModeDeniesNothing(t *testing.T) {
	sec := config.SecurityConfig{ModeValue: config.TrustedMode}
	p := New(sec, "/tmp/proj", nil)

	if d := p.DenyCommand("rm -rf /"); d != nil {
		t.Fatalf("trusted mode denied command: %v", d)
	}
	if d := p.DenyWorkdir("/etc"); d != nil {
		t.Fatalf("trusted mode denied workdir: %v", d)
	}
	if d := p.DenyPidAttach(); d != nil {
		t.Fatalf("trusted mode denied pid attach: %v", d)
	}
	if d := p.DenyStdin(); d != nil {
		t.Fatalf("trusted mode denied stdin: %v", d)
	}
	if d := p.DenyReveal(); d != nil {
		t.Fatalf("trusted mode denied reveal: %v", d)
	}
	if p.Sec.Restricted() {
		t.Fatal("trusted mode reported restricted")
	}
	if p.Sec.Mode() != string(config.TrustedMode) {
		t.Fatalf("Mode() = %q, want trusted", p.Sec.Mode())
	}
}

func TestRestrictedDeniesNonAllowlistedCommand(t *testing.T) {
	p := New(restricted(), "/tmp/proj", nil)
	d := p.DenyCommand("bash")
	if d == nil {
		t.Fatal("expected denial for non-allowlisted command")
	}
	if d.Rule != RuleCommands {
		t.Fatalf("rule = %q, want %q", d.Rule, RuleCommands)
	}
	if !strings.Contains(d.Error(), "policy_denied") {
		t.Fatalf("error %q missing policy_denied prefix", d.Error())
	}
	if !IsDenial(d) {
		t.Fatal("IsDenial returned false for a *Denial")
	}
	if !errors.As(d, new(*Denial)) {
		t.Fatal("errors.As failed to unwrap *Denial")
	}
}

func TestRestrictedAllowsAllowlistedCommands(t *testing.T) {
	p := New(restricted(), "/tmp/proj", nil)
	for _, cmd := range []string{"go", "npm", "python", "./mvnw", "/usr/local/bin/go", "/opt/x/npm"} {
		if d := p.DenyCommand(cmd); d != nil {
			t.Fatalf("command %q denied: %v", cmd, d)
		}
	}
}

func TestRestrictedDeniesOutOfTreeWorkdir(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "proj")
	p := New(restricted(), proj, nil)

	if d := p.DenyWorkdir(filepath.Join(proj, "backend")); d != nil {
		t.Fatalf("in-tree workdir denied: %v", d)
	}
	if d := p.DenyWorkdir(proj); d != nil {
		t.Fatalf("project root denied: %v", d)
	}

	out := filepath.Join(filepath.Dir(proj), "elsewhere")
	if d := p.DenyWorkdir(out); d == nil {
		t.Fatal("expected denial for out-of-tree workdir")
	} else if d.Rule != RuleWorkdirs {
		t.Fatalf("rule = %q, want %q", d.Rule, RuleWorkdirs)
	}

	// A sibling whose name merely shares a prefix must not pass.
	if d := p.DenyWorkdir(proj + "-evil"); d == nil {
		t.Fatal("expected denial for prefix-sibling workdir")
	}
}

func TestRestrictedAllowsDeclaredExtraWorkdir(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	shared := filepath.Join(base, "shared")
	sec := config.SecurityConfig{
		ModeValue:       config.RestrictedMode,
		AllowedCommands: []string{"go"},
		AllowedWorkdirs: []string{"${project}", "${project}/../shared"},
	}
	p := New(sec, proj, nil)
	if d := p.DenyWorkdir(shared); d != nil {
		t.Fatalf("declared shared workdir denied: %v", d)
	}
}

func TestRestrictedPidAttachStdinRevealGates(t *testing.T) {
	sec := restricted()
	sec.AllowPidAttach = boolPtr(false)
	sec.AllowStdin = boolPtr(false)
	sec.RevealSecrets = boolPtr(false)
	p := New(sec, "/tmp/proj", nil)

	if d := p.DenyPidAttach(); d == nil || d.Rule != RulePidAttach {
		t.Fatalf("pid attach not denied: %v", d)
	}
	if d := p.DenyStdin(); d == nil || d.Rule != RuleStdin {
		t.Fatalf("stdin not denied: %v", d)
	}
	if d := p.DenyReveal(); d == nil || d.Rule != RuleReveal {
		t.Fatalf("reveal not denied: %v", d)
	}

	// Explicitly enabled knobs lift the gates.
	sec2 := restricted()
	sec2.AllowPidAttach = boolPtr(true)
	sec2.AllowStdin = boolPtr(true)
	sec2.RevealSecrets = boolPtr(true)
	p2 := New(sec2, "/tmp/proj", nil)
	for name, d := range map[string]*Denial{
		"pid attach": p2.DenyPidAttach(),
		"stdin":      p2.DenyStdin(),
		"reveal":     p2.DenyReveal(),
	} {
		if d != nil {
			t.Fatalf("%s denied despite explicit enable: %v", name, d)
		}
	}
}

func TestNilKnobsDefaultToAllowedInRestrictedMode(t *testing.T) {
	p := New(restricted(), "/tmp/proj", nil)
	if d := p.DenyPidAttach(); d != nil {
		t.Fatalf("nil allow_pid_attach should default to allowed: %v", d)
	}
	if d := p.DenyStdin(); d != nil {
		t.Fatalf("nil allow_stdin should default to allowed: %v", d)
	}
	if d := p.DenyReveal(); d != nil {
		t.Fatalf("nil reveal_secrets should default to allowed: %v", d)
	}
}

func TestResolveExpandsProjectPlaceholder(t *testing.T) {
	sec := config.SecurityConfig{
		ModeValue:       config.RestrictedMode,
		AllowedCommands: []string{"go"},
		AllowedWorkdirs: []string{"${project}", "${project}/../shared", "relative", "/abs/path"},
	}
	resolved := sec.Resolve("/home/dev/proj")
	want := map[string]string{
		"/home/dev/proj":           "/home/dev/proj",
		"/home/dev/proj/../shared": "/home/dev/shared",
		"relative":                 "/home/dev/proj/relative",
		"/abs/path":                "/abs/path",
	}
	for entry, exp := range want {
		found := false
		for _, r := range resolved.AllowedWorkdirs {
			if r == exp {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("resolved workdirs %v do not contain %q (entry %q)", resolved.AllowedWorkdirs, exp, entry)
		}
	}
	if resolved == &sec {
		t.Fatal("Resolve must return a copy, not the receiver")
	}
	if len(resolved.AllowedWorkdirs) != len(sec.AllowedWorkdirs) {
		t.Fatal("Resolve changed the workdir count")
	}
}

func boolPtr(b bool) *bool { return &b }
