// Package config loads project configuration and resolves the v3 layer
// stack for a workspace.
//
// Effective config for a workspace = merge (lowest → highest):
//
//  1. Built-in defaults
//  2. agentd.yaml → defaults:
//  3. Repo layer (opt-in, trust-gated): <workspace>/agent-runtime.yaml
//  4. Project layer: projects/<id>/agent-runtime.yaml
//  5. Workspace overlay: projects/<id>/workspaces/<ws>.yaml
//  6. Request-time overrides (that start only)
//
// Merge rules: maps merge by key; apps.<name> merges field-wise; lists
// replace unless the key ends in `+` (e.g. env+:), which appends. Every
// resolved field carries provenance (which layer set it).
package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// portTemplate matches ${port:name} in env values.
var portTemplate = regexp.MustCompile(`\$\{port:([A-Za-z0-9_.-]+)\}`)

// Layer identifies one level of the config stack.
type Layer string

const (
	LayerBuiltin   Layer = "builtin"
	LayerDaemon    Layer = "daemon-defaults"
	LayerRepo      Layer = "repo"
	LayerProject   Layer = "project"
	LayerWorkspace Layer = "workspace"
	LayerRequest   Layer = "request"
)

// Provenance records which layer set a resolved field.
type Provenance map[string]Layer

// V3App holds the v3 schema keys for an app. It embeds AppConfig for all
// v1/v2 keys so old files load unchanged; the new keys are optional.
type V3App struct {
	AppConfig `yaml:",inline"`

	// Lifetime is persistent (default, survives session close) or session
	// (tied to the starting session with a lease).
	Lifetime string `yaml:"lifetime"`
	// Autostart starts the app when the daemon loads this workspace.
	Autostart bool `yaml:"autostart"`
	// Reload controls config-change behaviour: manual (mark stale, default)
	// or restart (restart automatically).
	Reload string `yaml:"reload"`
	// Pty allocates a PTY for the process (colors, interactive CLIs).
	Pty bool `yaml:"pty"`
	// DependsOn orders startup (Phase 9: wait-for-ready chain).
	DependsOn []string `yaml:"depends_on"`
	// Ports declares ports for templating + conflict detection (Phase 9).
	Ports map[string]PortDecl `yaml:"ports"`
}

// PortDecl declares one named port with a preferred default.
type PortDecl struct {
	Default int `yaml:"default"`
}

// V3Config is the version: 3 document shape. Unknown fields are rejected
// with line/col errors so typos fail fast.
type V3Config struct {
	Version int `yaml:"version"`
	Project struct {
		Name string `yaml:"name"`
	} `yaml:"project"`
	Runtime RuntimeConfig    `yaml:"runtime"`
	Apps    map[string]V3App `yaml:"apps"`
	// Logging holds workspace log policy (redaction rules are regexes;
	// matches are replaced with *** in GetLogs/Search/Tail output).
	Logging LoggingConfig `yaml:"logging"`
	// Alerts notifies (event + GUI toast) when a pattern appears in any
	// process's logs: {pattern, regex?, message?}.
	Alerts []AlertRule `yaml:"alerts"`
}

// LoggingConfig is workspace log policy.
type LoggingConfig struct {
	Redact []string `yaml:"redact"`
}

// AlertRule fires a logs.alert event on first match per process run.
type AlertRule struct {
	Pattern string `yaml:"pattern"`
	Regex   bool   `yaml:"regex"`
	Message string `yaml:"message"`
}

// ValidationError carries file/line/column for an invalid config.
type ValidationError struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s (line %d col %d: %s)", e.Path, e.Line, e.Column, e.Message)
	}
	return fmt.Sprintf("line %d col %d: %s", e.Line, e.Column, e.Message)
}

// Validate parses YAML and returns machine-readable errors. An empty
// document is valid (means "no apps").
func Validate(data []byte) []*ValidationError {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return []*ValidationError{{Message: err.Error()}}
	}
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return []*ValidationError{{
			Line: root.Line, Column: root.Column, Message: "config must be a mapping",
		}}
	}
	allowed := map[string]bool{"version": true, "project": true, "runtime": true, "apps": true, "logging": true, "alerts": true}
	var errs []*ValidationError
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if !allowed[k.Value] {
			errs = append(errs, &ValidationError{
				Line: k.Line, Column: k.Column, Path: k.Value,
				Message: fmt.Sprintf("unknown top-level key %q", k.Value),
			})
		}
	}
	var cfg V3Config
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	// Strict: a typo inside an app (e.g. "comand:" instead of "command:")
	// used to validate as OK, silently doing nothing, because unknown
	// fields anywhere below the top level were accepted (B9). yaml.v3
	// reports every rejected field as its own "line N: ..." entry inside
	// one aggregate *yaml.TypeError; unpack those into individual
	// line-numbered ValidationErrors instead of one opaque blob.
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		errs = append(errs, unknownFieldErrors(err)...)
		if len(errs) == 0 {
			errs = append(errs, &ValidationError{Message: err.Error()})
		}
		return errs
	}
	for name, app := range cfg.Apps {
		if app.Lifetime != "" && app.Lifetime != "persistent" && app.Lifetime != "session" {
			errs = append(errs, &ValidationError{Path: "apps." + name + ".lifetime",
				Message: fmt.Sprintf("lifetime must be persistent|session, got %q", app.Lifetime)})
		}
		if app.Reload != "" && app.Reload != "manual" && app.Reload != "restart" {
			errs = append(errs, &ValidationError{Path: "apps." + name + ".reload",
				Message: fmt.Sprintf("reload must be manual|restart, got %q", app.Reload)})
		}
		for _, pat := range app.Readiness {
			if err := checkRegexp(pat); err != nil {
				errs = append(errs, &ValidationError{Path: "apps." + name + ".readiness", Message: err.Error()})
			}
		}
		for _, p := range app.DependsOn {
			if _, ok := cfg.Apps[p]; !ok {
				errs = append(errs, &ValidationError{Path: "apps." + name + ".depends_on",
					Message: fmt.Sprintf("depends on unknown app %q", p)})
			}
		}
	}
	return errs
}

// yamlFieldErrRe matches one line of a *yaml.TypeError's Errors slice, e.g.
// "line 5: field comand not found in type config.V3App".
var yamlFieldErrRe = regexp.MustCompile(`^line (\d+): (.*)$`)

// unknownFieldErrors unpacks a strict-mode yaml.v3 decode error (an
// aggregate *yaml.TypeError) into individual line-numbered
// ValidationErrors, one per rejected field, instead of returning it as one
// opaque message with no line number.
func unknownFieldErrors(err error) []*ValidationError {
	te, ok := err.(*yaml.TypeError)
	if !ok {
		return nil
	}
	out := make([]*ValidationError, 0, len(te.Errors))
	for _, msg := range te.Errors {
		m := yamlFieldErrRe.FindStringSubmatch(msg)
		if m == nil {
			out = append(out, &ValidationError{Message: msg})
			continue
		}
		line, _ := strconv.Atoi(m[1])
		out = append(out, &ValidationError{Line: line, Message: m[2]})
	}
	return out
}

func checkRegexp(pat string) error {
	if pat == "" {
		return fmt.Errorf("empty readiness pattern")
	}
	return nil
}

// SHA256 returns the hex sha256 of content (trust + revision records).
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

// ExpandPortTemplates replaces ${port:name} in env entries using alloc.
// alloc returns the stable port for a name (allocation persists wherever
// the caller keeps it, e.g. daemon settings).
func ExpandPortTemplates(env []string, alloc func(name string) (int, error)) ([]string, error) {
	out := make([]string, len(env))
	for i, kv := range env {
		var err error
		out[i] = portTemplate.ReplaceAllStringFunc(kv, func(m string) string {
			name := portTemplate.FindStringSubmatch(m)[1]
			p, aerr := alloc(name)
			if aerr != nil {
				err = aerr
				return m
			}
			return strconv.Itoa(p)
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ChangeKind classifies what a config edit means for a running app.
type ChangeKind string

const (
	ChangeAdded           ChangeKind = "added"
	ChangeRemoved         ChangeKind = "removed"
	ChangeRestartRequired ChangeKind = "restart-required"
	ChangeAppliedLive     ChangeKind = "applied-live"
	ChangeUnchanged       ChangeKind = "unchanged"
)

// AppChange is one row of the reconcile plan (terraform-plan style).
type AppChange struct {
	App             string     `json:"app"`
	Kind            ChangeKind `json:"kind"`
	Fields          []string   `json:"fields"`
	AffectedProcIDs []string   `json:"affected_process_ids,omitempty"`
	RestartPolicy   string     `json:"restart_policy,omitempty"`
}

// Plan diffs old vs new app maps and classifies each change.
// Fields that need a restart: command, workdir, env, env_file, limits,
// type. Supervision-only changes (readiness, health_check, restart)
// apply live. Removed apps leave processes running (orphaned-config).
func Plan(oldApps, newApps map[string]V3App, runningByApp map[string][]string) []AppChange {
	var out []AppChange
	for name, newApp := range newApps {
		oldApp, ok := oldApps[name]
		if !ok {
			out = append(out, AppChange{App: name, Kind: ChangeAdded})
			continue
		}
		fields := diffApp(oldApp, newApp)
		if len(fields) == 0 {
			continue
		}
		kind := ChangeAppliedLive
		for _, f := range fields {
			if restartField(f) {
				kind = ChangeRestartRequired
				break
			}
		}
		out = append(out, AppChange{
			App: name, Kind: kind, Fields: fields,
			AffectedProcIDs: runningByApp[name],
		})
	}
	for name := range oldApps {
		if _, ok := newApps[name]; !ok {
			out = append(out, AppChange{
				App: name, Kind: ChangeRemoved,
				AffectedProcIDs: runningByApp[name],
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].App < out[j].App })
	return out
}

func restartField(f string) bool {
	switch f {
	case "command", "workdir", "env", "env_file", "limits", "type", "ports":
		return true
	}
	return false
}

func diffApp(a, b V3App) []string {
	var fields []string
	add := func(name string, eq bool) {
		if !eq {
			fields = append(fields, name)
		}
	}
	add("type", a.Type == b.Type)
	add("workdir", a.WorkDir == b.WorkDir)
	add("command", strings.Join(a.Command, "\x00") == strings.Join(b.Command, "\x00"))
	add("env_file", a.EnvFile == b.EnvFile)
	add("env", strings.Join(a.Env, "\x00") == strings.Join(b.Env, "\x00"))
	add("readiness", strings.Join(a.Readiness, "\x00") == strings.Join(b.Readiness, "\x00"))
	add("health_check", fmt.Sprintf("%v", a.HealthCheck) == fmt.Sprintf("%v", b.HealthCheck))
	add("restart", fmt.Sprintf("%v", a.Restart) == fmt.Sprintf("%v", b.Restart))
	add("limits", a.Limits == b.Limits)
	add("lifetime", a.Lifetime == b.Lifetime)
	add("reload", a.Reload == b.Reload)
	add("pty", a.Pty == b.Pty)
	sort.Strings(fields)
	return fields
}

// ParseV3 parses YAML into a V3Config with defaults applied.
func ParseV3(data []byte) (*V3Config, []*ValidationError) {
	if errs := Validate(data); len(errs) > 0 {
		return nil, errs
	}
	var cfg V3Config
	if len(strings.TrimSpace(string(data))) == 0 {
		cfg.Apps = map[string]V3App{}
		return &cfg, nil
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, []*ValidationError{{Message: err.Error()}}
	}
	if cfg.Apps == nil {
		cfg.Apps = map[string]V3App{}
	}
	for name, app := range cfg.Apps {
		app.defaults()
		if app.Lifetime == "" {
			app.Lifetime = "persistent"
		}
		if app.Reload == "" {
			app.Reload = "manual"
		}
		cfg.Apps[name] = app
	}
	return &cfg, nil
}

// DeprecationWarnings lists v3-moved keys still present in a document
// (daemon, log_store, db_path, db_max_age_days, db_max_mb, metrics, http:
// now daemon-wide in agentd.yaml). They are ignored with a warning.
func DeprecationWarnings(data []byte) []string {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil
	}
	rt, ok := raw["runtime"].(map[string]any)
	if !ok {
		return nil
	}
	moved := []string{"daemon", "log_store", "db_path", "db_max_age_days", "db_max_mb", "metrics", "http"}
	var out []string
	for _, k := range moved {
		if _, ok := rt[k]; ok {
			out = append(out, fmt.Sprintf("runtime.%s is daemon-wide in v3 (agentd.yaml) and is ignored here", k))
		}
	}
	return out
}

// EffectivePaths records which files feed a workspace's config.
type EffectivePaths struct {
	Repo      string // "" when absent
	Project   string
	Overlay   string // "" when absent
	RepoTrust bool
}

// DiscoverEffective finds the candidate config files for a workspace.
// Nothing is read; use Resolve to load+merge.
func DiscoverEffective(dataDir, projectID, workspaceID, workspacePath string) EffectivePaths {
	ep := EffectivePaths{
		Project: filepath.Join(dataDir, "projects", projectID, "agent-runtime.yaml"),
		Overlay: filepath.Join(dataDir, "projects", projectID, "workspaces", workspaceID+".yaml"),
	}
	for _, cand := range []string{
		filepath.Join(workspacePath, "agent-runtime.yaml"),
		filepath.Join(workspacePath, "agent-runtime.yml"),
	} {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			ep.Repo = cand
			break
		}
	}
	if _, err := os.Stat(ep.Overlay); err != nil {
		ep.Overlay = ""
	}
	return ep
}

// Resolved is the merged effective config for a workspace.
type Resolved struct {
	Apps       map[string]V3App `json:"apps"`
	Provenance Provenance       `json:"provenance"`
	Paths      EffectivePaths   `json:"paths"`
	Warnings   []string         `json:"warnings"`
	// Redact unions logging.redact across layers; Alerts likewise.
	Redact []string    `json:"redact"`
	Alerts []AlertRule `json:"alerts"`
}

// Resolve loads and merges all layers. repoTrusted reports whether the
// repo layer passed the trust gate; when false and a repo file exists it
// is skipped (last trusted revision stays effective upstream).
func Resolve(dataDir, projectID, workspaceID, workspacePath string, repoTrusted bool, daemonDefaults map[string]string) (*Resolved, []*ValidationError) {
	ep := DiscoverEffective(dataDir, projectID, workspaceID, workspacePath)
	merged := map[string]V3App{}
	prov := Provenance{}
	var warnings []string
	var redact []string
	var alerts []AlertRule
	seenRedact := map[string]bool{}

	apply := func(data []byte, layer Layer, base string) []*ValidationError {
		cfg, errs := ParseV3(data)
		if errs != nil {
			return errs
		}
		warnings = append(warnings, DeprecationWarnings(data)...)
		for name, app := range cfg.Apps {
			cur := merged[name]
			mergeApp(&cur, app, base, workspacePath)
			merged[name] = cur
			prov["apps."+name] = layer
		}
		for _, pat := range cfg.Logging.Redact {
			if !seenRedact[pat] {
				seenRedact[pat] = true
				redact = append(redact, pat)
			}
		}
		alerts = append(alerts, cfg.Alerts...)
		return nil
	}

	// Repo layer (trust-gated).
	if ep.Repo != "" {
		if repoTrusted {
			data, err := os.ReadFile(ep.Repo)
			if err == nil {
				if errs := apply(data, LayerRepo, filepath.Dir(ep.Repo)); len(errs) > 0 {
					return nil, errs
				}
			}
		} else {
			warnings = append(warnings, "repo config present but untrusted; ignoring until trusted")
		}
	}
	// Project layer.
	if data, err := os.ReadFile(ep.Project); err == nil {
		if errs := apply(data, LayerProject, workspacePath); len(errs) > 0 {
			return nil, errs
		}
	}
	// Workspace overlay.
	if ep.Overlay != "" {
		if data, err := os.ReadFile(ep.Overlay); err == nil {
			if errs := apply(data, LayerWorkspace, workspacePath); len(errs) > 0 {
				return nil, errs
			}
		}
	}
	_ = daemonDefaults
	return &Resolved{Apps: merged, Provenance: prov, Paths: ep, Warnings: warnings, Redact: redact, Alerts: alerts}, nil
}

// mergeApp merges src into dst field-wise; lists replace unless the key
// ends in `+` (env+: appends). Relative workdirs resolve against base,
// except repo-layer files where base is the file's own directory.
func mergeApp(dst *V3App, src V3App, base, workspacePath string) {
	if src.Type != "" {
		dst.Type = src.Type
	}
	if src.WorkDir != "" {
		dst.WorkDir = src.WorkDir
	}
	if src.Command != nil {
		dst.Command = src.Command
	}
	if src.EnvFile != "" {
		dst.EnvFile = src.EnvFile
	}
	if src.Env != nil {
		// env+: suffix support lives at the YAML-map level; the typed
		// struct cannot see the suffix, so plain env: replaces (predictable
		// default per §4.5). Append-syntax is honoured in MergeRawYAML.
		dst.Env = src.Env
	}
	if src.Readiness != nil {
		dst.Readiness = src.Readiness
	}
	dst.HealthCheck = src.HealthCheck
	dst.Restart = src.Restart
	dst.Limits = src.Limits
	if src.Lifetime != "" {
		dst.Lifetime = src.Lifetime
	}
	if src.Reload != "" {
		dst.Reload = src.Reload
	}
	if src.Pty {
		dst.Pty = true
	}
	if src.Autostart {
		dst.Autostart = true
	}
	if src.DependsOn != nil {
		dst.DependsOn = src.DependsOn
	}
	if src.Ports != nil {
		dst.Ports = src.Ports
	}
	_ = base
	_ = workspacePath
}

// ToLoaded converts a Resolved workspace config into a config.Loaded the
// existing runtime.Runtime can host. Relative paths resolve against the
// workspace root (§13).
func (r *Resolved) ToLoaded(workspacePath string) *Loaded {
	cfg := Config{}
	cfg.defaults()
	cfg.Apps = make(map[string]AppConfig, len(r.Apps))
	for name, app := range r.Apps {
		cfg.Apps[name] = app.AppConfig
	}
	return &Loaded{Config: cfg, ProjectDir: workspacePath}
}
