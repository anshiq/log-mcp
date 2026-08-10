// Package config loads the optional per-project agent-runtime.yaml file and
// provides defaults for runtime behaviour.
package config

import (
	"fmt"
	"os"
	"path/filepath"
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
	LogStore           string   `yaml:"log_store"`       // "memory" (default) | "sqlite"
	DBPath             string   `yaml:"db_path"`         // resolved against ProjectDir; default .agent-runtime/logs.db
	DBMaxAgeDays       *int     `yaml:"db_max_age_days"` // nil -> 7; 0 = keep forever
	DBMaxMB            *int64   `yaml:"db_max_mb"`       // nil -> 512; 0 = unlimited
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
}

// AppConfig declares a named application the runtime can manage.
type AppConfig struct {
	Type    string   `yaml:"type"`
	WorkDir string   `yaml:"workdir"`
	Command []string `yaml:"command"`
	EnvFile string   `yaml:"env_file"`
	Env     []string `yaml:"env"`
}

// Config is the parsed project configuration.
type Config struct {
	Runtime RuntimeConfig        `yaml:"runtime"`
	Apps    map[string]AppConfig `yaml:"apps"`
}

func (c *Config) defaults() {
	c.Runtime.defaults()
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

// LoadDefault finds agent-runtime.yaml by walking up from the current working
// directory. Returns an empty config (with defaults) when none is found.
func LoadDefault() (*Loaded, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return LoadFrom(cwd)
}

// LoadFrom finds agent-runtime.yaml starting at startDir and walking upward.
func LoadFrom(startDir string) (*Loaded, error) {
	dir := startDir
	for {
		for _, name := range []string{"agent-runtime.yaml", "agent-runtime.yml"} {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				return LoadFile(path)
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	cfg := Config{}
	cfg.defaults()
	return &Loaded{Config: cfg, ProjectDir: startDir}, nil
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
