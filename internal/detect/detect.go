package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agent-runtime/internal/config"
	"agent-runtime/internal/profile"
)

type DetectedApp struct {
	Name      string
	Type      string
	Workdir   string
	Command   []string
	Readiness []string
}

var relevantFiles = []string{
	"package.json",
	"go.mod",
	"manage.py",
	"pyproject.toml",
	"requirements.txt",
	"pom.xml",
	"build.gradle",
	"build.gradle.kts",
	"Procfile",
	"Makefile",
	"docker-compose.yml",
	"docker-compose.yaml",
	"mvnw",
	"gradlew",
	"pnpm-lock.yaml",
	"yarn.lock",
	"bun.lockb",
	"bun.lock",
}

func Detect(workdir string) []DetectedApp {
	st, err := os.Stat(workdir)
	if err != nil || !st.IsDir() {
		return nil
	}
	dirs := []string{workdir}
	entries, err := os.ReadDir(workdir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				dirs = append(dirs, filepath.Join(workdir, e.Name()))
			}
		}
	}
	reg := profile.Default()
	var out []DetectedApp
	seen := map[string]bool{}
	add := func(app DetectedApp) {
		if len(app.Command) == 0 || app.Name == "" {
			return
		}
		base := app.Name
		n := base
		i := 2
		for seen[n] {
			n = base + "-" + itoa(i)
			i++
		}
		app.Name = n
		seen[n] = true
		out = append(out, app)
	}
	for _, dir := range dirs {
		rel, _ := filepath.Rel(workdir, dir)
		if rel == "." {
			rel = "."
		}
		nameBase := filepath.Base(dir)
		if dir == workdir {
			nameBase = filepath.Base(filepath.Clean(workdir))
			if nameBase == "/" || nameBase == "." || nameBase == "" {
				nameBase = "app"
			}
		}
		if v := detectNode(dir, rel, nameBase, reg); v != nil {
			add(*v)
		}
		for _, v := range detectGo(dir, rel, nameBase, reg) {
			add(v)
		}
		if v := detectDjango(dir, rel, nameBase, reg); v != nil {
			add(*v)
		}
		if v := detectPython(dir, rel, nameBase, reg); v != nil {
			if !hasFile(dir, "manage.py") {
				add(*v)
			}
		}
		if v := detectJava(dir, rel, nameBase, reg); v != nil {
			add(*v)
		}
		for _, v := range detectProcfile(dir, rel, reg) {
			add(v)
		}
		if v := detectMakefile(dir, rel, nameBase, reg); v != nil {
			if !hasFile(dir, "package.json") && !hasFile(dir, "go.mod") && !hasFile(dir, "manage.py") && !hasFile(dir, "pom.xml") {
				add(*v)
			}
		}
		for _, v := range detectCompose(dir, rel, reg) {
			add(v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return strings.TrimSpace(strings.Join([]string{string(rune('0' + i/10)), string(rune('0' + i%10))}, ""))
}

func hasFile(dir, name string) bool {
	st, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !st.IsDir()
}

func readinessFor(reg *profile.Registry, typ string) []string {
	if p := reg.Lookup(typ); p != nil {
		return append([]string(nil), p.Readiness...)
	}
	return nil
}

func detectNode(dir, rel, nameBase string, reg *profile.Registry) *DetectedApp {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	scripts, _ := raw["scripts"].(map[string]any)
	get := func(k string) bool {
		if scripts == nil {
			return false
		}
		_, ok := scripts[k]
		return ok
	}
	script := ""
	for _, cand := range []string{"dev", "start", "serve"} {
		if get(cand) {
			script = cand
			break
		}
	}
	if script == "" {
		return nil
	}
	typ := "node"
	if strings.Contains(string(data), `"next"`) {
		typ = "nextjs"
	}
	wd := rel
	if rel == "." {
		wd = "."
	}
	return &DetectedApp{
		Name:      nameBase,
		Type:      typ,
		Workdir:   wd,
		Command:   nodeRun(dir, script),
		Readiness: readinessFor(reg, typ),
	}
}

func nodeRun(dir, script string) []string {
	switch {
	case hasFile(dir, "pnpm-lock.yaml"):
		return []string{"pnpm", "run", script}
	case hasFile(dir, "yarn.lock"):
		return []string{"yarn", script}
	case hasFile(dir, "bun.lockb") || hasFile(dir, "bun.lock"):
		return []string{"bun", "run", script}
	default:
		return []string{"npm", "run", script}
	}
}

func detectGo(dir, rel, nameBase string, reg *profile.Registry) []DetectedApp {
	if !hasFile(dir, "go.mod") {
		return nil
	}
	wd := rel
	if rel == "." {
		wd = "."
	}
	cmdDir := filepath.Join(dir, "cmd")
	entries, err := os.ReadDir(cmdDir)
	if err == nil {
		var out []DetectedApp
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(cmdDir, e.Name(), "main.go")); err != nil {
				continue
			}
			p := wd
			if wd == "." {
				p = "./cmd/" + e.Name()
			} else {
				p = wd + "/cmd/" + e.Name()
			}
			_ = p
			out = append(out, DetectedApp{
				Name:      e.Name(),
				Type:      "go",
				Workdir:   wd,
				Command:   []string{"go", "run", "./cmd/" + e.Name()},
				Readiness: readinessFor(reg, "go"),
			})
		}
		if len(out) > 0 {
			return out
		}
	}
	return []DetectedApp{{
		Name:      nameBase,
		Type:      "go",
		Workdir:   wd,
		Command:   []string{"go", "run", "."},
		Readiness: readinessFor(reg, "go"),
	}}
}

func detectDjango(dir, rel, nameBase string, reg *profile.Registry) *DetectedApp {
	if !hasFile(dir, "manage.py") {
		return nil
	}
	wd := rel
	if rel == "." {
		wd = "."
	}
	return &DetectedApp{
		Name:      nameBase,
		Type:      "django",
		Workdir:   wd,
		Command:   []string{"python", "manage.py", "runserver"},
		Readiness: readinessFor(reg, "django"),
	}
}

func detectPython(dir, rel, nameBase string, reg *profile.Registry) *DetectedApp {
	if !hasFile(dir, "pyproject.toml") && !hasFile(dir, "requirements.txt") {
		return nil
	}
	wd := rel
	if rel == "." {
		wd = "."
	}
	for _, cand := range []string{"app.py", "main.py", "server.py", "wsgi.py"} {
		if hasFile(dir, cand) {
			py := "python"
			return &DetectedApp{
				Name:      nameBase,
				Type:      "python",
				Workdir:   wd,
				Command:   []string{py, cand},
				Readiness: readinessFor(reg, "python"),
			}
		}
	}
	data := ""
	if b, err := os.ReadFile(filepath.Join(dir, "requirements.txt")); err == nil {
		data += string(b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "pyproject.toml")); err == nil {
		data += string(b)
	}
	lower := strings.ToLower(data)
	switch {
	case strings.Contains(lower, "uvicorn") || strings.Contains(lower, "fastapi"):
		return &DetectedApp{
			Name:      nameBase,
			Type:      "python",
			Workdir:   wd,
			Command:   []string{"uvicorn", "main:app", "--reload"},
			Readiness: readinessFor(reg, "python"),
		}
	case strings.Contains(lower, "flask"):
		return &DetectedApp{
			Name:      nameBase,
			Type:      "python",
			Workdir:   wd,
			Command:   []string{"flask", "run"},
			Readiness: readinessFor(reg, "python"),
		}
	}
	return nil
}

func detectJava(dir, rel, nameBase string, reg *profile.Registry) *DetectedApp {
	if !hasFile(dir, "pom.xml") && !hasFile(dir, "build.gradle") && !hasFile(dir, "build.gradle.kts") {
		return nil
	}
	wd := rel
	if rel == "." {
		wd = "."
	}
	var cmd []string
	switch {
	case hasFile(dir, "mvnw"):
		cmd = []string{"./mvnw", "spring-boot:run"}
	case hasFile(dir, "gradlew"):
		cmd = []string{"./gradlew", "bootRun"}
	case hasFile(dir, "pom.xml"):
		cmd = []string{"mvn", "spring-boot:run"}
	default:
		cmd = []string{"./gradlew", "bootRun"}
	}
	return &DetectedApp{
		Name:      nameBase,
		Type:      "spring-boot",
		Workdir:   wd,
		Command:   cmd,
		Readiness: readinessFor(reg, "spring-boot"),
	}
}

func detectProcfile(dir, rel string, reg *profile.Registry) []DetectedApp {
	data, err := os.ReadFile(filepath.Join(dir, "Procfile"))
	if err != nil {
		return nil
	}
	wd := rel
	if rel == "." {
		wd = "."
	}
	prof := reg.Detect(dir)
	typ := "generic"
	if prof != nil && prof.Name != "" && prof.Name != "generic" {
		typ = prof.Name
	}
	var out []DetectedApp
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colon])
		cmdStr := strings.TrimSpace(line[colon+1:])
		if name == "" || cmdStr == "" {
			continue
		}
		parts := strings.Fields(cmdStr)
		if len(parts) == 0 {
			continue
		}
		out = append(out, DetectedApp{
			Name:      name,
			Type:      typ,
			Workdir:   wd,
			Command:   parts,
			Readiness: readinessFor(reg, typ),
		})
	}
	return out
}

func detectMakefile(dir, rel, nameBase string, reg *profile.Registry) *DetectedApp {
	data, err := os.ReadFile(filepath.Join(dir, "Makefile"))
	if err != nil {
		if _, err2 := os.ReadFile(filepath.Join(dir, "makefile")); err2 != nil {
			return nil
		} else {
			data, _ = os.ReadFile(filepath.Join(dir, "makefile"))
		}
	}
	wd := rel
	if rel == "." {
		wd = "."
	}
	text := string(data)
	hasDev := false
	hasRun := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "dev:") || strings.HasPrefix(line, "dev ") {
			hasDev = true
		}
		if strings.HasPrefix(line, "run:") || strings.HasPrefix(line, "run ") {
			hasRun = true
		}
	}
	target := ""
	if hasDev {
		target = "dev"
	} else if hasRun {
		target = "run"
	} else {
		return nil
	}
	return &DetectedApp{
		Name:      nameBase,
		Type:      "generic",
		Workdir:   wd,
		Command:   []string{"make", target},
		Readiness: readinessFor(reg, "generic"),
	}
}

func detectCompose(dir, rel string, reg *profile.Registry) []DetectedApp {
	var data []byte
	var err error
	for _, n := range []string{"docker-compose.yml", "docker-compose.yaml"} {
		if b, e := os.ReadFile(filepath.Join(dir, n)); e == nil {
			data = b
			err = nil
			break
		} else {
			err = e
		}
	}
	if err != nil || len(data) == 0 {
		return nil
	}
	wd := rel
	if rel == "." {
		wd = "."
	}
	services := composeServices(data)
	var out []DetectedApp
	for _, s := range services {
		out = append(out, DetectedApp{
			Name:      s,
			Type:      "generic",
			Workdir:   wd,
			Command:   []string{"docker", "compose", "up", s},
			Readiness: readinessFor(reg, "generic"),
		})
	}
	return out
}

func composeServices(data []byte) []string {
	var raw map[string]any
	var parsed map[string]any
	_ = raw
	_ = parsed
	text := string(data)
	lines := strings.Split(text, "\n")
	inServices := false
	services := []string{}
	baseIndent := -1
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		if strings.TrimSpace(trimmed) == "" || strings.HasPrefix(strings.TrimSpace(trimmed), "#") {
			continue
		}
		indent := len(trimmed) - len(strings.TrimLeft(trimmed, " "))
		stripped := strings.TrimSpace(trimmed)
		if stripped == "services:" {
			inServices = true
			baseIndent = indent
			continue
		}
		if !inServices {
			continue
		}
		if indent <= baseIndent && stripped != "" {
			if strings.Contains(stripped, ":") && indent == baseIndent {
				continue
			}
			if indent <= baseIndent && !strings.HasPrefix(stripped, "-") {
				if len(services) > 0 {
					break
				}
			}
		}
		if indent > baseIndent && strings.HasSuffix(stripped, ":") && !strings.Contains(strings.TrimSuffix(stripped, ":"), " ") {
			name := strings.TrimSuffix(stripped, ":")
			name = strings.TrimSpace(name)
			if name != "" && name != "services" {
				services = append(services, name)
			}
		}
	}
	return services
}

func Signature(workdir string) string {
	st, err := os.Stat(workdir)
	if err != nil || !st.IsDir() {
		return ""
	}
	type entry struct {
		rel  string
		data []byte
	}
	var entries []entry
	collect := func(dir string) {
		relDir, _ := filepath.Rel(workdir, dir)
		for _, name := range relevantFiles {
			full := filepath.Join(dir, name)
			b, err := os.ReadFile(full)
			if err != nil {
				continue
			}
			rel := name
			if relDir != "." && relDir != "" {
				rel = filepath.ToSlash(filepath.Join(relDir, name))
			}
			if name == "package.json" {
				if s, err := scriptsOnly(b); err == nil {
					b = s
				}
			}
			entries = append(entries, entry{rel: rel, data: b})
		}
	}
	collect(workdir)
	if ents, err := os.ReadDir(workdir); err == nil {
		for _, e := range ents {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				collect(filepath.Join(workdir, e.Name()))
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	var buf []byte
	for _, e := range entries {
		buf = append(buf, []byte(e.rel)...)
		buf = append(buf, 0)
		buf = append(buf, e.data...)
		buf = append(buf, 0)
	}
	if len(buf) == 0 {
		return config.SHA256([]byte("empty"))
	}
	return config.SHA256(buf)
}

func scriptsOnly(data []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return data, err
	}
	s, ok := raw["scripts"]
	if !ok {
		return []byte("{}"), nil
	}
	return s, nil
}
