package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SecurityMode enumerates the runtime security modes.
type SecurityMode string

const (
	// TrustedMode is the default posture: arbitrary exec, arbitrary workdirs,
	// pid attach, stdin and secret reveal are all permitted.
	TrustedMode SecurityMode = "trusted"

	// RestrictedMode is the enforceable posture for CI/shared machines:
	// start_process commands are confined to an allowlist, workdirs to a
	// confinement set, and pid attach, stdin and secret reveal can be switched
	// off.
	RestrictedMode SecurityMode = "restricted"
)

// projectPlaceholder is expanded to the project directory when resolving
// allowed workdirs, so a config stays portable across clones and paths.
const projectPlaceholder = "${project}"

// SecurityConfig holds the enforceable security posture for the runtime. It is
// wired into RuntimeConfig as `runtime.security`; enforcement itself lives in
// internal/policy, never in MCP handlers.
type SecurityConfig struct {
	// ModeValue is the effective security mode. The Go field is named ModeValue
	// (rather than Mode) so the required accessor method Mode() can exist; the
	// yaml key is "mode". Empty is defaulted to "trusted".
	ModeValue SecurityMode `yaml:"mode"`

	// AllowedCommands lists the command basenames (or exact paths) start_process
	// may launch in restricted mode, e.g. ["go", "npm", "./mvnw", "python"].
	// Matching is by filepath.Base against each entry, or exact-path equality.
	AllowedCommands []string `yaml:"allowed_commands"`

	// AllowedWorkdirs confines resolved workdirs in restricted mode. Each entry
	// may use the "${project}" placeholder (expanded against the project dir);
	// entries not containing it are treated as absolute paths or, when relative,
	// resolved against the project dir. A workdir is allowed when it equals an
	// entry or lies inside one (path confinement). Resolve produces the copy
	// the policy layer enforces against.
	AllowedWorkdirs []string `yaml:"allowed_workdirs"`

	// AllowPidAttach gates open_shell(pid=) OS-pid attach. nil means "not
	// restricted" (allowed); in restricted mode false denies attach.
	AllowPidAttach *bool `yaml:"allow_pid_attach"`
	// AllowStdin gates send_stdin. nil means "not restricted" (allowed); in
	// restricted mode false denies stdin writes.
	AllowStdin *bool `yaml:"allow_stdin"`
	// RevealSecrets gates get_process_env(reveal=true). nil means "not
	// restricted" (allowed); in restricted mode false hard-disables reveal.
	RevealSecrets *bool `yaml:"reveal_secrets"`
}

// defaults fills unset fields with safe defaults. Every nil pointer means
// "not restricted", so a v1 file loads unchanged into a permissive posture.
func (s *SecurityConfig) defaults() {
	if s.ModeValue == "" {
		s.ModeValue = TrustedMode
	}
	if s.AllowPidAttach == nil {
		s.AllowPidAttach = boolPtr(true)
	}
	if s.AllowStdin == nil {
		s.AllowStdin = boolPtr(true)
	}
	if s.RevealSecrets == nil {
		s.RevealSecrets = boolPtr(true)
	}
}

// validate checks the posture at load time so misconfigurations surface
// immediately, never at first denied operation. Restricted mode without any
// allowed command would deny every start_process, so it fails the load.
func (s *SecurityConfig) validate() error {
	switch s.ModeValue {
	case TrustedMode, RestrictedMode:
	default:
		return fmt.Errorf("invalid security mode %q (want trusted|restricted)", s.ModeValue)
	}
	if s.Restricted() && len(s.AllowedCommands) == 0 {
		return fmt.Errorf("security: restricted mode requires at least one allowed_command")
	}
	return nil
}

// Mode returns the effective mode as a string.
func (s *SecurityConfig) Mode() string { return string(s.ModeValue) }

// Restricted reports whether enforcement is active.
func (s *SecurityConfig) Restricted() bool { return s.ModeValue == RestrictedMode }

// PidAttachAllowed reports whether open_shell(pid=) is permitted. Always true
// in trusted mode, or when the knob is nil (nil means "not restricted").
func (s *SecurityConfig) PidAttachAllowed() bool {
	if s.AllowPidAttach == nil {
		return true
	}
	return !s.Restricted() || *s.AllowPidAttach
}

// StdinAllowed reports whether send_stdin is permitted. Always true in trusted
// mode, or when the knob is nil (nil means "not restricted").
func (s *SecurityConfig) StdinAllowed() bool {
	if s.AllowStdin == nil {
		return true
	}
	return !s.Restricted() || *s.AllowStdin
}

// RevealAllowed reports whether get_process_env(reveal=true) is permitted.
// Always true in trusted mode, or when the knob is nil (nil means "not
// restricted").
func (s *SecurityConfig) RevealAllowed() bool {
	if s.RevealSecrets == nil {
		return true
	}
	return !s.Restricted() || *s.RevealSecrets
}

// CommandAllowed reports whether a command may be started. In restricted mode
// the command's basename (or the exact command string) must appear in
// AllowedCommands. Trusted mode allows everything.
func (s *SecurityConfig) CommandAllowed(cmd string) bool {
	if !s.Restricted() {
		return true
	}
	base := filepath.Base(cmd)
	for _, a := range s.AllowedCommands {
		if a == cmd || a == base {
			return true
		}
	}
	return false
}

// WorkdirAllowed reports whether a resolved workdir is inside the confinement
// set. In restricted mode the directory must equal an entry of
// AllowedWorkdirs or lie beneath one. Callers must pass the Resolve'd copy so
// "${project}" is already expanded. Trusted mode allows everything.
func (s *SecurityConfig) WorkdirAllowed(dir string) bool {
	if !s.Restricted() {
		return true
	}
	dir = filepath.Clean(dir)
	for _, a := range s.AllowedWorkdirs {
		if workdirWithin(filepath.Clean(a), dir) {
			return true
		}
	}
	return false
}

// Resolve returns a copy of the config with AllowedWorkdirs made absolute:
// "${project}" is replaced with projectDir, and entries that are still relative
// are joined against it. All entries are cleaned. The returned copy is the one
// the policy layer should enforce against.
func (s *SecurityConfig) Resolve(projectDir string) *SecurityConfig {
	cp := *s
	cp.AllowedWorkdirs = make([]string, len(s.AllowedWorkdirs))
	for i, wd := range s.AllowedWorkdirs {
		wd = strings.ReplaceAll(wd, projectPlaceholder, projectDir)
		if !filepath.IsAbs(wd) {
			wd = filepath.Join(projectDir, wd)
		}
		cp.AllowedWorkdirs[i] = filepath.Clean(wd)
	}
	return &cp
}

// workdirWithin reports whether child equals parent or lies strictly beneath
// it, purely lexically after cleaning.
func workdirWithin(parent, child string) bool {
	if parent == child {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func boolPtr(b bool) *bool { return &b }
