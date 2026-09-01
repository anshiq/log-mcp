// Package policy is the enforcement layer for the runtime's security posture.
// It imports only internal/config and the standard library, so it can be
// safely used by the runtime facade without coupling to MCP handlers, the
// process manager or the log store. Enforcement decisions are structured
// errors (Denial); the facade surfaces them verbatim to callers and the audit
// log records them.
package policy

import (
	"errors"
	"fmt"
	"log/slog"

	"agent-runtime/internal/config"
)

// Denial is a structured enforcement refusal. It implements error so it can be
// returned and wrapped like any other; the message is always prefixed with
// "policy_denied" so callers can detect a refusal with errors.Is/errors.As or
// by string match, independent of the rule that fired.
type Denial struct {
	// Rule names the config knob that fired, e.g. "allowed_commands",
	// "allowed_workdirs", "allow_pid_attach", "allow_stdin" or "reveal_secrets".
	Rule string
	// Detail is a human-readable description of what was refused.
	Detail string
}

// Error implements the error interface.
func (d *Denial) Error() string {
	return fmt.Sprintf("policy_denied: %s: %s", d.Rule, d.Detail)
}

// IsDenial reports whether err is, or wraps, a *Denial.
func IsDenial(err error) bool {
	var d *Denial
	return errors.As(err, &d)
}

// AsDenial unwraps a *Denial from err, reporting whether it was found.
func AsDenial(err error) (*Denial, bool) {
	var d *Denial
	if errors.As(err, &d) {
		return d, true
	}
	return nil, false
}

// Rule identifiers used as Denial.Rule values. They mirror the config knob
// that fired so operators can map a refusal straight back to the yaml.
const (
	RuleCommands  = "allowed_commands"
	RuleWorkdirs  = "allowed_workdirs"
	RulePidAttach = "allow_pid_attach"
	RuleStdin     = "allow_stdin"
	RuleReveal    = "reveal_secrets"
)

// Policy answers the per-operation allow/deny questions of the runtime facade
// against a resolved SecurityConfig. It is safe for concurrent use: the config
// is immutable after New, so the checks are pure reads.
type Policy struct {
	// Sec is the Resolve'd security config; AllowedWorkdirs is absolute.
	Sec config.SecurityConfig
	// ProjectDir is the project root AllowedWorkdirs was resolved against.
	ProjectDir string

	logger *slog.Logger
}

// New builds a Policy from a security config and the project directory it is
// enforced in. The workdir allowlist is resolved (${project} expanded,
// relatives made absolute) here, so callers can construct the policy once and
// reuse it. logger may be nil; denials are logged at debug level when present.
func New(sec config.SecurityConfig, projectDir string, logger *slog.Logger) *Policy {
	if logger == nil {
		logger = slog.Default()
	}
	return &Policy{
		Sec:        *sec.Resolve(projectDir),
		ProjectDir: projectDir,
		logger:     logger,
	}
}

// DenyCommand returns a Denial when cmd may not be started, or nil when it is
// allowed (including always in trusted mode).
func (p *Policy) DenyCommand(cmd string) *Denial {
	if p.Sec.CommandAllowed(cmd) {
		return nil
	}
	return p.deny(RuleCommands, fmt.Sprintf("command %q is not in allowed_commands", cmd))
}

// DenyWorkdir returns a Denial when dir lies outside the allowed workdirs, or
// nil when it is confined (including always in trusted mode).
func (p *Policy) DenyWorkdir(dir string) *Denial {
	if p.Sec.WorkdirAllowed(dir) {
		return nil
	}
	return p.deny(RuleWorkdirs, fmt.Sprintf("workdir %q is outside allowed_workdirs", dir))
}

// DenyPidAttach returns a Denial when open_shell(pid=) is disabled in
// restricted mode, or nil when attach is permitted.
func (p *Policy) DenyPidAttach() *Denial {
	if p.Sec.PidAttachAllowed() {
		return nil
	}
	return p.deny(RulePidAttach, "open_shell(pid=) attach is disabled in restricted mode")
}

// DenyStdin returns a Denial when send_stdin is disabled in restricted mode,
// or nil when stdin writes are permitted.
func (p *Policy) DenyStdin() *Denial {
	if p.Sec.StdinAllowed() {
		return nil
	}
	return p.deny(RuleStdin, "send_stdin is disabled in restricted mode")
}

// DenyReveal returns a Denial when get_process_env(reveal=true) is disabled in
// restricted mode, or nil when secrets may be revealed.
func (p *Policy) DenyReveal() *Denial {
	if p.Sec.RevealAllowed() {
		return nil
	}
	return p.deny(RuleReveal, "reveal=true is disabled in restricted mode")
}

func (p *Policy) deny(rule, detail string) *Denial {
	d := &Denial{Rule: rule, Detail: detail}
	p.logger.Debug("policy denied", "rule", rule, "detail", detail)
	return d
}
