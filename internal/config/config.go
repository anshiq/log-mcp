// Package config loads project configuration and
// provides defaults for runtime behaviour.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that unmarshals from either a string ("5s") or a
// number (seconds).
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	switch node.Tag {
	case "!!str":
		v, err := time.ParseDuration(node.Value)
		if err != nil {
			return err
		}
		*d = Duration(v)
		return nil
	case "!!int":
		v, err := strconv.Atoi(node.Value)
		if err != nil {
			return err
		}
		*d = Duration(time.Duration(v) * time.Second)
		return nil
	}
	return fmt.Errorf("invalid duration %q", node.Value)
}

func (d Duration) Time() time.Duration { return time.Duration(d) }

// RuntimeConfig holds runtime-wide knobs.
type RuntimeConfig struct {
	LogBufferLines     int      `yaml:"log_buffer_lines"`
	ShutdownTimeout    Duration `yaml:"shutdown_timeout"`
	StopGrace          Duration `yaml:"stop_grace"`
	MaxLogLines        int      `yaml:"max_log_lines"`
	MaxLogBytes        int      `yaml:"max_log_bytes"`
	MaxExitedProcesses int      `yaml:"max_exited_processes"`
	ExitedTTL          Duration `yaml:"exited_ttl"`
	LogStore           string   `yaml:"log_store"`       // "memory" (default) | "sqlite"
	DBPath             string   `yaml:"db_path"`         // resolved against ProjectDir; default .agent-runtime/logs.db
	DBMaxAgeDays       *int     `yaml:"db_max_age_days"` // nil -> 7; 0 = keep forever
	DBMaxMB            *int64   `yaml:"db_max_mb"`       // nil -> 512; 0 = unlimited
	ShellEnv           string   `yaml:"shell_env"`       // "" or "login" -> capture login-shell env as base layer; "none" -> os.Environ()
	// Env is the runtime-wide env layer applied to every app/process: above
	// the base env (login-shell env or os.Environ()) and below app-level
	// env_file/env.
	Env []string `yaml:"env"`

	// Security is the enforceable security posture (trusted default, or
	// restricted for CI/shared machines). Enforcement lives in internal/policy.
	Security SecurityConfig `yaml:"security"`

	// Metrics is the optional Prometheus scrape listen address (e.g. ":9341");
	// empty disables the metrics endpoint.
	Metrics string `yaml:"metrics"`

	// LogForward enables best-effort, drop-on-backpressure forwarding of
	// captured log lines (stdout and/or OTLP) alongside the durable archive.
	LogForward LogForwardConfig `yaml:"log_forward"`

	// Daemon, when true, makes serve (and repl) thin clients of a long-lived
	// daemon that owns the managed processes, so they survive the MCP session.
	// Default false = session-scoped, exactly today's behavior.
	Daemon bool `yaml:"daemon"`

	// HTTP configures the optional streamable-HTTP transport (`serve --http`).
	// A bearer token is required — there is no unauthenticated HTTP mode, even
	// on localhost.
	HTTP HTTPConfig `yaml:"http"`
}

// HTTPConfig configures the streamable-HTTP MCP transport.
type HTTPConfig struct {
	// Addr is the default listen address for `serve --http` (e.g. ":7341").
	// The --http flag overrides it.
	Addr string `yaml:"addr"`
	// Token is the required bearer token (Authorization: Bearer <token>),
	// env-expanded (${VAR} / $VAR). Requests without it get 401.
	Token string `yaml:"token"`
}

// LogForwardConfig enables best-effort log forwarding. Forwarding is
// drop-on-backpressure like the SQLite enqueue path — it never buffers,
// batches for delivery, or blocks the runtime.
type LogForwardConfig struct {
	// OTLP is an OTLP/HTTP logs endpoint, e.g. "http://localhost:4318/v1/logs"
	// or a bare "host:port". Empty disables OTLP forwarding.
	OTLP string `yaml:"otlp"`
	// Stdout mirrors captured lines to agent-runtime's own stdout when true.
	Stdout bool `yaml:"stdout"`
}

func (r *RuntimeConfig) defaults() {
	if r.LogBufferLines <= 0 {
		r.LogBufferLines = 10000
	}
	if r.ShutdownTimeout == 0 {
		r.ShutdownTimeout = Duration(5 * time.Second)
	}
	if r.StopGrace == 0 {
		r.StopGrace = Duration(5 * time.Second)
	}
	if r.MaxLogLines <= 0 {
		r.MaxLogLines = 2000
	}
	if r.MaxLogBytes <= 0 {
		r.MaxLogBytes = 512 * 1024
	}
	if r.MaxExitedProcesses <= 0 {
		r.MaxExitedProcesses = 50
	}
	if r.ExitedTTL == 0 {
		r.ExitedTTL = Duration(5 * time.Minute)
	}
	if r.LogStore == "" {
		r.LogStore = "memory"
	}
	if r.DBMaxAgeDays == nil {
		v := 7
		r.DBMaxAgeDays = &v
	}
	if r.DBMaxMB == nil {
		v := int64(512)
		r.DBMaxMB = &v
	}
	if r.ShellEnv == "" {
		r.ShellEnv = "login"
	}
	r.Security.defaults()
}

// AppConfig declares a named application the runtime can manage.
type AppConfig struct {
	Type    string   `yaml:"type"`
	WorkDir string   `yaml:"workdir"`
	Command []string `yaml:"command"`
	EnvFile string   `yaml:"env_file"`
	Env     []string `yaml:"env"`

	// Readiness overrides the profile-detected readiness regexes for this app.
	// Each string is compiled once at config load; a bad pattern fails the
	// whole config load rather than surfacing at first start.
	Readiness []string `yaml:"readiness"`

	// HealthCheck is the optional continuous health probe for this app.
	HealthCheck HealthCheck `yaml:"health_check"`

	// Restart is the optional declarative auto-restart policy for this app.
	Restart Restart `yaml:"restart"`

	// Limits are the optional per-app resource limits (Phase 7): cpu cores
	// (e.g. "1.0", "500m") and memory (e.g. "512M", "1G"). Linux-first via
	// cgroup v2; degrades gracefully to unlimited when unsupported.
	Limits Limits `yaml:"limits"`
}

// Limits declares optional per-app resource caps. Both fields are optional;
// empty/zero means "unlimited" for that resource.
type Limits struct {
	// CPU is the CPU limit in cores: a float (e.g. "1.0", "0.5") or a
	// milli-core suffix (e.g. "500m"). Empty = unlimited.
	CPU string `yaml:"cpu"`
	// Memory is the memory limit as a size (e.g. "512M", "1G", "256MiB").
	// Empty = unlimited.
	Memory string `yaml:"memory"`
}

// ParseMemorySize parses a memory size string ("512M", "1G", "256MiB",
// "1073741824") into bytes. Suffixes: b, k/kib, m/mib, g/gib, t/tib
// (case-insensitive; no suffix = bytes).
func ParseMemorySize(s string) (int64, error) {
	v, err := parseSize(s, map[string]int64{
		"": 1, "b": 1, "k": 1 << 10, "kib": 1 << 10, "kb": 1e3,
		"m": 1 << 20, "mib": 1 << 20, "mb": 1e6,
		"g": 1 << 30, "gib": 1 << 30, "gb": 1e9,
		"t": 1 << 40, "tib": 1 << 40, "tb": 1e12,
	})
	if err != nil {
		return 0, fmt.Errorf("invalid memory limit %q: %w", s, err)
	}
	return v, nil
}

// ParseCPU parses a CPU limit ("1.0", "0.5", "500m") into a fraction of cores.
// The "m" suffix means milli-cores.
func ParseCPU(s string) (float64, error) {
	t := strings.TrimSpace(strings.ToLower(s))
	if t == "" {
		return 0, nil
	}
	if strings.HasSuffix(t, "m") {
		ms, err := strconv.ParseFloat(strings.TrimSuffix(t, "m"), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid cpu limit %q: %w", s, err)
		}
		if ms < 0 {
			return 0, fmt.Errorf("invalid cpu limit %q: negative", s)
		}
		return ms / 1000, nil
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid cpu limit %q: %w", s, err)
	}
	if v < 0 {
		return 0, fmt.Errorf("invalid cpu limit %q: negative", s)
	}
	return v, nil
}

func parseSize(s string, units map[string]int64) (int64, error) {
	t := strings.TrimSpace(strings.ToLower(s))
	if t == "" {
		return 0, fmt.Errorf("empty")
	}
	// Split trailing unit letters from the numeric prefix.
	i := 0
	for i < len(t) && (t[i] >= '0' && t[i] <= '9' || t[i] == '.') {
		i++
	}
	num := t[:i]
	unit := strings.TrimSpace(t[i:])
	mul, ok := units[unit]
	if !ok {
		return 0, fmt.Errorf("unknown unit %q", unit)
	}
	if num == "" {
		return 0, fmt.Errorf("missing number")
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, err
	}
	if f < 0 {
		return 0, fmt.Errorf("negative")
	}
	return int64(f * float64(mul)), nil
}

// HealthCheck configures the continuous HTTP/TCP probe run once a process is
// ready. Exactly one of HTTP or TCP should be set.
type HealthCheck struct {
	HTTP string `yaml:"http"` // "http://localhost:8080/healthz"
	TCP  string `yaml:"tcp"`  // "localhost:8080"

	Interval Duration `yaml:"interval"` // default 10s
	Timeout  Duration `yaml:"timeout"`  // default 3s

	// FailureThreshold is the number of consecutive failed probes that mark the
	// process unhealthy (default 3).
	FailureThreshold int `yaml:"failure_threshold"`
}

// RestartPolicy is the declarative auto-restart policy.
type RestartPolicy string

const (
	RestartNever     RestartPolicy = "never"
	RestartOnFailure RestartPolicy = "on-failure"
	RestartAlways    RestartPolicy = "always"
)

// Restart configures declarative auto-restart with exponential backoff.
type Restart struct {
	Policy RestartPolicy `yaml:"policy"` // never | on-failure | always; "" defaults to never
	// Backoff is the capped-exponential step schedule in seconds or duration
	// strings, e.g. [1s, 2s, 5s, 15s, 60s]. Default [1s, 2s, 5s, 15s, 60s].
	Backoff []Duration `yaml:"backoff"`
	// MaxRestarts caps restarts within a rolling 10-minute window; beyond it
	// the process is marked crashed. Default 10; 0 disables the budget.
	MaxRestarts int `yaml:"max_restarts"`
}

// defaults fills unset supervision fields with safe defaults. All are optional
// so a v1 file loads unchanged (every v2 field zero-valued is inert).
func (a *AppConfig) defaults() {
	if a.Restart.Policy == "" {
		a.Restart.Policy = RestartNever
	}
	if len(a.Restart.Backoff) == 0 {
		a.Restart.Backoff = []Duration{
			Duration(1 * time.Second), Duration(2 * time.Second),
			Duration(5 * time.Second), Duration(15 * time.Second),
			Duration(60 * time.Second),
		}
	}
	if a.Restart.MaxRestarts == 0 {
		a.Restart.MaxRestarts = 10
	}
	if a.HealthCheck.Interval == 0 {
		a.HealthCheck.Interval = Duration(10 * time.Second)
	}
	if a.HealthCheck.Timeout == 0 {
		a.HealthCheck.Timeout = Duration(3 * time.Second)
	}
	if a.HealthCheck.FailureThreshold == 0 {
		a.HealthCheck.FailureThreshold = 3
	}
}

// validate compiles readiness regexes and checks policy strings at load time.
// A bad readiness pattern or unknown policy fails the load with the app name so
// misconfigurations surface immediately, never at first start.
func (a *AppConfig) validate() error {
	for _, pat := range a.Readiness {
		if _, err := regexp.Compile(pat); err != nil {
			return fmt.Errorf("readiness pattern %q: %w", pat, err)
		}
	}
	switch a.Restart.Policy {
	case RestartNever, RestartOnFailure, RestartAlways:
	default:
		return fmt.Errorf("invalid restart policy %q (want never|on-failure|always)", a.Restart.Policy)
	}
	if a.HealthCheck.HTTP != "" && a.HealthCheck.TCP != "" {
		return fmt.Errorf("health_check: set only one of http or tcp")
	}
	if a.HealthCheck.Interval <= 0 || a.HealthCheck.Timeout <= 0 {
		return fmt.Errorf("health_check: interval and timeout must be positive")
	}
	if a.HealthCheck.Timeout >= a.HealthCheck.Interval {
		return fmt.Errorf("health_check: timeout (%v) must be shorter than interval (%v)", a.HealthCheck.Timeout.Time(), a.HealthCheck.Interval.Time())
	}
	if a.Limits.CPU != "" {
		if _, err := ParseCPU(a.Limits.CPU); err != nil {
			return err
		}
	}
	if a.Limits.Memory != "" {
		if _, err := ParseMemorySize(a.Limits.Memory); err != nil {
			return err
		}
	}
	return nil
}

// SupportedVersion is the highest config schema version this build understands.
// Version 1 (no version key) and version 2 parse identically; unknown future
// versions fail load rather than being silently mis-read.
const SupportedVersion = 2

// Config is the parsed project configuration.
type Config struct {
	// Version is the config schema version. Absent (0) or 1 means the v1
	// schema (parsed unchanged); 2 opts into v2 semantics (supervision
	// extensions remain optional, so a v1 file still loads as valid v2).
	Version int                  `yaml:"version"`
	Runtime RuntimeConfig        `yaml:"runtime"`
	Apps    map[string]AppConfig `yaml:"apps"`
}

func (c *Config) defaults() {
	if c.Version == 0 {
		c.Version = 1
	}
	c.Runtime.defaults()
	for i := range c.Apps {
		app := c.Apps[i]
		app.defaults()
		c.Apps[i] = app
	}
}

// validate enforces schema-version and per-app rules after defaults are
// applied. Unknown versions fail load so a config written for a newer
// agent-runtime is never silently mis-parsed.
func (c *Config) validate() error {
	if c.Version > SupportedVersion {
		return fmt.Errorf("config version %d is newer than this build supports (max %d)", c.Version, SupportedVersion)
	}
	if c.Version < 1 {
		return fmt.Errorf("invalid config version %d", c.Version)
	}
	if err := c.Runtime.Security.validate(); err != nil {
		return fmt.Errorf("runtime.security: %w", err)
	}
	for name := range c.Apps {
		app := c.Apps[name]
		if err := app.validate(); err != nil {
			return fmt.Errorf("app %q: %w", name, err)
		}
	}
	return nil
}

// Loaded bundles a parsed config with the project root used to resolve
// relative paths.
type Loaded struct {
	Config     Config
	ProjectDir string
	Path       string
}

// Names lists configured apps in sorted order.
func (l *Loaded) Names() []string {
	names := make([]string, 0, len(l.Config.Apps))
	for n := range l.Config.Apps {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func EmptyLoaded(projectDir string) *Loaded {
	cfg := Config{}
	cfg.defaults()
	return &Loaded{Config: cfg, ProjectDir: projectDir}
}

// LoadFile loads a config from an explicit path.
func LoadFile(path string) (*Loaded, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Loaded{Config: cfg, ProjectDir: filepath.Dir(path), Path: path}, nil
}

// ParseEnvFile reads a dotenv-style file into KEY=VALUE pairs. Lines starting
// with '#' are ignored; surrounding quotes are stripped. No shell expansion is
// performed.
func ParseEnvFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			continue
		}
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		out = append(out, key+"="+val)
	}
	return out, nil
}
