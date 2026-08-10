// Package profile provides runtime "profiles" describing common application
// frameworks. Profiles live ABOVE the generic process manager: they inform
// command selection, readiness detection and shutdown grace, but never touch
// process lifecycle logic directly.
package profile

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Rule is a single file-based detection heuristic.
type Rule struct {
	File     string
	Contains string // substring that must appear in the file ("" = existence only)
	Weight   int
}

// Profile describes how to run and recognize one class of application.
type Profile struct {
	Name      string
	Rules     []Rule
	Readiness []string // regex strings
	StopGrace time.Duration
	ready     []*regexp.Regexp
	command   func(workdir string) []string
}

// Ready reports whether a single log line indicates the app is up.
func (p *Profile) Ready(line string) bool {
	for _, re := range p.ready {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// Command returns a sensible default start command for the profile in the given
// working directory. Returns nil when the profile has no default.
func (p *Profile) Command(workdir string) []string {
	if p.command == nil {
		return nil
	}
	return p.command(workdir)
}

// StopGrace returns the graceful shutdown grace period, defaulting to 5s.
func (p *Profile) Grace() time.Duration {
	if p.StopGrace > 0 {
		return p.StopGrace
	}
	return 5 * time.Second
}

// Registry holds the known profiles and performs detection.
type Registry struct {
	profiles []*Profile
	byName   map[string]*Profile
}

func (r *Registry) Lookup(name string) *Profile {
	return r.byName[name]
}

func (r *Registry) List() []*Profile {
	out := make([]*Profile, 0, len(r.profiles))
	for _, p := range r.profiles {
		if p.Name == Generic.Name {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Detect scores every profile against the files in workdir and returns the best
// match. Falls back to generic when nothing matches.
func (r *Registry) Detect(workdir string) *Profile {
	best := Generic
	bestScore := 0
	for _, p := range r.profiles {
		score := r.score(workdir, p)
		if score > bestScore {
			best = p
			bestScore = score
		}
	}
	return best
}

func (r *Registry) score(workdir string, p *Profile) int {
	score := 0
	for _, rule := range p.Rules {
		path := filepath.Join(workdir, rule.File)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if rule.Contains == "" || strings.Contains(string(data), rule.Contains) {
			score += rule.Weight
		}
	}
	return score
}

func mustCompileAll(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, regexp.MustCompile(p))
	}
	return out
}

func hasLockfile(workdir string, names ...string) bool {
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(workdir, n)); err == nil {
			return true
		}
	}
	return false
}

func newProfile(name string, rules []Rule, readiness []string, command func(string) []string) *Profile {
	return &Profile{
		Name:      name,
		Rules:     rules,
		Readiness: readiness,
		ready:     mustCompileAll(readiness),
		command:   command,
	}
}

// Generic is the fallback profile used when nothing is detected.
var Generic = &Profile{Name: "generic", ready: nil}

// Default returns the registry with the built-in profiles.
func Default() *Registry {
	profiles := []*Profile{
		newProfile("nextjs",
			[]Rule{{File: "package.json", Contains: `"next"`, Weight: 4}},
			[]string{
				`(?i)ready in`,
				`(?i)started server`,
				`(?i)local:\s*https?://`,
				`(?i)compiled successfully`,
				`(?i)ready`,
			},
			func(dir string) []string { return nodeRun(dir, "dev") },
		),
		newProfile("spring-boot",
			[]Rule{
				{File: "pom.xml", Weight: 2},
				{File: "build.gradle", Weight: 2},
				{File: "build.gradle.kts", Weight: 2},
			},
			[]string{
				`Started \S+ in \d+(\.\d+)?s?`,
				`Tomcat started on port`,
				`Netty started on port`,
				`Jetty started on port`,
				`(UnderTow|Undertow) started on port`,
			},
			springBootCommand,
		),
		newProfile("django",
			[]Rule{{File: "manage.py", Weight: 3}},
			[]string{
				`Starting development server at`,
				`Quit the server with`,
				`Uvicorn running on http`,
			},
			func(string) []string { return []string{"python", "manage.py", "runserver"} },
		),
		newProfile("node",
			[]Rule{{File: "package.json", Weight: 1}},
			[]string{
				`(?i)listening on`,
				`(?i)server started`,
				`(?i)started server`,
				`(?i)ready`,
			},
			func(dir string) []string { return nodeRun(dir, "dev") },
		),
		newProfile("python",
			[]Rule{
				{File: "requirements.txt", Weight: 1},
				{File: "pyproject.toml", Weight: 1},
			},
			[]string{
				`(?i)running on http`,
				`(?i)listening on`,
				`(?i)server started`,
			},
			nil,
		),
		newProfile("go",
			[]Rule{{File: "go.mod", Weight: 1}},
			[]string{
				`(?i)listening on`,
				`(?i)server started`,
			},
			func(string) []string { return []string{"go", "run", "."} },
		),
	}

	byName := make(map[string]*Profile, len(profiles)+1)
	for _, p := range profiles {
		byName[p.Name] = p
	}
	byName["generic"] = Generic

	// Deterministic order for ties: stable by name.
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })

	return &Registry{profiles: profiles, byName: byName}
}

// nodeRun picks the package manager based on lockfiles present in dir.
func nodeRun(dir, script string) []string {
	switch {
	case hasLockfile(dir, "pnpm-lock.yaml", "pnpm-workspace.yaml"):
		return []string{"pnpm", "run", script}
	case hasLockfile(dir, "yarn.lock"):
		return []string{"yarn", script}
	case hasLockfile(dir, "bun.lockb", "bun.lock"):
		return []string{"bun", "run", script}
	default:
		return []string{"npm", "run", script}
	}
}

func springBootCommand(dir string) []string {
	switch {
	case hasLockfile(dir, "mvnw", "mvnw.cmd"):
		return []string{"./mvnw", "spring-boot:run"}
	case hasLockfile(dir, "gradlew", "gradlew.bat"):
		return []string{"./gradlew", "bootRun"}
	case hasLockfile(dir, "pom.xml"):
		return []string{"mvn", "spring-boot:run"}
	default:
		return []string{"mvn", "spring-boot:run"}
	}
}
