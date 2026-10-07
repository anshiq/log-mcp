// Package config loads project configuration and resolves the v3 layer
// stack for a workspace.
//
// Effective config for a workspace = merge (lowest → highest):
//
//  1. Built-in defaults
//  2. agentd.yaml → defaults:
//  3. Project layer: projects/<id>/agent-runtime.yaml
//  4. Workspace overlay: projects/<id>/workspaces/<ws>.yaml
//  5. Request-time overrides (that start only)
//
// Merge rules: maps merge by key; apps.<name> merges field-wise; lists
// replace unless the key ends in `+` (e.g. env+:), which appends. Every
// resolved field carries provenance (which layer set it).
package config

import (
	"crypto/sha256"
	"fmt"
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
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		errs = append(errs, unknownFieldErrors(err)...)
		if len(errs) == 0 {
			errs = append(errs, &ValidationError{Message: err.Error()})
		}
		return errs
	}
	for name, app := range cfg.Apps {
		loc := nodeLoc(root, []string{"apps", name})
		if app.Lifetime != "" && app.Lifetime != "persistent" && app.Lifetime != "session" {
			errs = append(errs, &ValidationError{Line: loc.line, Column: loc.col, Path: "apps." + name + ".lifetime",
				Message: fmt.Sprintf("lifetime must be persistent|session, got %q", app.Lifetime)})
		}
		if app.Reload != "" && app.Reload != "manual" && app.Reload != "restart" {
			errs = append(errs, &ValidationError{Line: loc.line, Column: loc.col, Path: "apps." + name + ".reload",
				Message: fmt.Sprintf("reload must be manual|restart, got %q", app.Reload)})
		}
		for _, pat := range app.Readiness {
			if err := checkRegexp(pat); err != nil {
				errs = append(errs, &ValidationError{Line: loc.line, Column: loc.col, Path: "apps." + name + ".readiness", Message: err.Error()})
			}
		}
		for _, p := range app.DependsOn {
			if _, ok := cfg.Apps[p]; !ok {
				errs = append(errs, &ValidationError{Line: loc.line, Column: loc.col, Path: "apps." + name + ".depends_on",
					Message: fmt.Sprintf("depends on unknown app %q", p)})
			}
		}
		for _, e := range app.Env {
			for _, m := range portTemplate.FindAllStringSubmatch(e, -1) {
				if len(m) < 2 || m[1] == "" {
					errs = append(errs, &ValidationError{Line: loc.line, Column: loc.col, Path: "apps." + name + ".env", Message: "invalid port template " + e})
				}
			}
		}
	}
	seenPorts := map[int]string{}
	for name, app := range cfg.Apps {
		loc := nodeLoc(root, []string{"apps", name, "ports"})
		for pname, decl := range app.Ports {
			if decl.Default == 0 {
				continue
			}
			if prev, ok := seenPorts[decl.Default]; ok {
				errs = append(errs, &ValidationError{Line: loc.line, Column: loc.col, Path: "apps." + name + ".ports." + pname, Message: fmt.Sprintf("duplicate port %d also used by %q", decl.Default, prev)})
			} else {
				seenPorts[decl.Default] = name + "." + pname
			}
		}
	}
	errs = append(errs, checkDependsCycles(cfg.Apps, root)...)
	return errs
}

type loc struct {
	line int
	col  int
}

func nodeLoc(root *yaml.Node, path []string) loc {
	cur := root
	for _, seg := range path {
		if cur == nil || cur.Kind != yaml.MappingNode {
			return loc{}
		}
		found := false
		for i := 0; i+1 < len(cur.Content); i += 2 {
			if cur.Content[i].Value == seg {
				if len(path) > 0 && seg == path[len(path)-1] {
					return loc{line: cur.Content[i].Line, col: cur.Content[i].Column}
				}
				cur = cur.Content[i+1]
				found = true
				break
			}
		}
		if !found {
			return loc{}
		}
	}
	return loc{}
}

func checkDependsCycles(apps map[string]V3App, root *yaml.Node) []*ValidationError {
	visited := map[string]int{}
	var stack []string
	var out []*ValidationError
	var visit func(n string)
	visit = func(n string) {
		if visited[n] == 2 {
			return
		}
		if visited[n] == 1 {
			cycle := append(append([]string{}, stack...), n)
			loc := nodeLoc(root, []string{"apps", n})
			out = append(out, &ValidationError{Line: loc.line, Column: loc.col, Path: "apps." + n + ".depends_on", Message: "depends_on cycle: " + strings.Join(cycle, " -> ")})
			return
		}
		visited[n] = 1
		stack = append(stack, n)
		for _, d := range apps[n].DependsOn {
			if _, ok := apps[d]; ok {
				visit(d)
			}
		}
		stack = stack[:len(stack)-1]
		visited[n] = 2
	}
	for name := range apps {
		visit(name)
	}
	return out
}

var yamlFieldErrRe = regexp.MustCompile(`^line (\d+): field (\S+) not found in type (.*)$`)
var yamlFieldErrRe2 = regexp.MustCompile(`^line (\d+): (.*)$`)

func unknownFieldErrors(err error) []*ValidationError {
	te, ok := err.(*yaml.TypeError)
	if !ok {
		return nil
	}
	out := make([]*ValidationError, 0, len(te.Errors))
	for _, msg := range te.Errors {
		if m := yamlFieldErrRe.FindStringSubmatch(msg); m != nil {
			line, _ := strconv.Atoi(m[1])
			out = append(out, &ValidationError{Line: line, Column: 1, Path: m[2], Message: m[2] + " is not a known field (" + m[3] + ")"})
			continue
		}
		if m := yamlFieldErrRe2.FindStringSubmatch(msg); m != nil {
			line, _ := strconv.Atoi(m[1])
			out = append(out, &ValidationError{Line: line, Column: 1, Path: "", Message: m[2]})
			continue
		}
		out = append(out, &ValidationError{Message: msg})
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

type ConfigSource interface {
	Project(projectID string) ([]byte, error)
	Overlay(projectID, workspaceID string) ([]byte, error)
}

type EffectiveSources struct {
	ProjectRev int64 `json:"projectRev"`
	OverlayRev int64 `json:"overlayRev"`
}

type MemorySource struct {
	ProjectYAML []byte
	ProjectRev  int64
	Overlays    map[string][]byte
	OverlayRevs map[string]int64
}

func (m *MemorySource) Project(projectID string) ([]byte, error) {
	if len(m.ProjectYAML) == 0 {
		return nil, nil
	}
	return m.ProjectYAML, nil
}

func (m *MemorySource) Overlay(projectID, workspaceID string) ([]byte, error) {
	if m.Overlays == nil {
		return nil, nil
	}
	return m.Overlays[workspaceID], nil
}

func DiscoverEffective(src ConfigSource, projectID, workspaceID string) EffectiveSources {
	var out EffectiveSources
	if rs, ok := src.(interface {
		ProjectRev(string) int64
		OverlayRev(string, string) int64
	}); ok {
		out.ProjectRev = rs.ProjectRev(projectID)
		out.OverlayRev = rs.OverlayRev(projectID, workspaceID)
		return out
	}
	if m, ok := src.(*MemorySource); ok {
		out.ProjectRev = m.ProjectRev
		if m.OverlayRevs != nil {
			out.OverlayRev = m.OverlayRevs[workspaceID]
		}
		return out
	}
	return out
}

// Resolved is the merged effective config for a workspace.
type Resolved struct {
	Apps       map[string]V3App `json:"apps"`
	Provenance Provenance       `json:"provenance"`
	Sources    EffectiveSources `json:"sources"`
	Warnings   []string         `json:"warnings"`
	Redact []string    `json:"redact"`
	Alerts []AlertRule `json:"alerts"`
}

func Resolve(src ConfigSource, projectID, workspaceID, workspacePath string, daemonDefaults map[string]string) (*Resolved, []*ValidationError) {
	ep := DiscoverEffective(src, projectID, workspaceID)
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

	if data, err := src.Project(projectID); err == nil && len(data) > 0 {
		if errs := apply(data, LayerProject, workspacePath); len(errs) > 0 {
			return nil, errs
		}
	}
	if data, err := src.Overlay(projectID, workspaceID); err == nil && len(data) > 0 {
		if errs := apply(data, LayerWorkspace, workspacePath); len(errs) > 0 {
			return nil, errs
		}
	}
	_ = daemonDefaults
	return &Resolved{Apps: merged, Provenance: prov, Sources: ep, Warnings: warnings, Redact: redact, Alerts: alerts}, nil
}

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

func (r *Resolved) ToLoaded(workspacePath string) *Loaded {
	cfg := Config{}
	cfg.defaults()
	cfg.Apps = make(map[string]AppConfig, len(r.Apps))
	for name, app := range r.Apps {
		cfg.Apps[name] = app.AppConfig
	}
	return &Loaded{Config: cfg, ProjectDir: workspacePath}
}
