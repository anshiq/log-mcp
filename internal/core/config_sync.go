package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/detect"
	"agent-runtime/internal/store"
	"gopkg.in/yaml.v3"
)

type dbConfigSource struct {
	db *store.DB
}

func (s *dbConfigSource) Project(projectID string) ([]byte, error) {
	if s.db == nil {
		return nil, nil
	}
	data, _, err := s.db.GetConfig(projectID, "project", "")
	return data, err
}

func (s *dbConfigSource) Overlay(projectID, workspaceID string) ([]byte, error) {
	if s.db == nil {
		return nil, nil
	}
	data, _, err := s.db.GetConfig(projectID, "workspace", workspaceID)
	return data, err
}

func (s *dbConfigSource) ProjectRev(projectID string) int64 {
	if s.db == nil {
		return 0
	}
	_, rev, _ := s.db.GetConfig(projectID, "project", "")
	return rev
}

func (s *dbConfigSource) OverlayRev(projectID, workspaceID string) int64 {
	if s.db == nil {
		return 0
	}
	_, rev, _ := s.db.GetConfig(projectID, "workspace", workspaceID)
	return rev
}

var projectLocks sync.Map

func projectLock(projectID string) *sync.Mutex {
	v, _ := projectLocks.LoadOrStore(projectID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

type proposalItem struct {
	App    string `json:"app"`
	Action string `json:"action"`
	Reason string `json:"reason"`
}

type storedProposal struct {
	ID        string         `json:"id"`
	YAML      string         `json:"yaml"`
	Summary   []proposalItem `json:"summary"`
	CreatedAt int64          `json:"createdAt"`
}

func hashApp(app config.V3App) string {
	norm := config.V3App{}
	norm.Type = app.Type
	norm.WorkDir = app.WorkDir
	if norm.WorkDir == "." {
		norm.WorkDir = ""
	}
	norm.Command = append([]string(nil), app.Command...)
	norm.Readiness = append([]string(nil), app.Readiness...)
	b, err := yaml.Marshal(norm)
	if err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%v", norm)))
		return fmt.Sprintf("%x", sum)
	}
	return config.SHA256(b)
}

func EnsureConfig(pr *ProjectRuntime) {
	if pr == nil || pr.store == nil {
		return
	}
	projectID := pr.projectID
	wsPath := pr.wsPath
	mu := projectLock(projectID)
	mu.Lock()
	defer mu.Unlock()
	sig := detect.Signature(wsPath)
	syncRow, err := pr.store.GetConfigSync(projectID)
	if err != nil {
		return
	}
	if sig != "" && sig == syncRow.RunSignature {
		return
	}
	detected := detect.Detect(wsPath)
	projYAML, _, _ := pr.store.GetConfig(projectID, "project", "")
	curApps := map[string]config.V3App{}
	if len(projYAML) > 0 {
		if cfg, errs := config.ParseV3(projYAML); errs == nil && cfg != nil {
			curApps = cfg.Apps
		} else {
			_ = pr.store.PutConfigSync(projectID, sig, syncRow.AutoAppsJSON)
			return
		}
	}
	autoMap := map[string]string{}
	if syncRow.AutoAppsJSON != "" {
		_ = json.Unmarshal([]byte(syncRow.AutoAppsJSON), &autoMap)
	}
	isAuto := func(name string) bool {
		cur, ok := curApps[name]
		if !ok {
			return false
		}
		h, ok := autoMap[name]
		if !ok {
			return false
		}
		return h == hashApp(cur)
	}
	detMap := map[string]detect.DetectedApp{}
	for _, d := range detected {
		detMap[d.Name] = d
	}
	v3FromDetected := func(d detect.DetectedApp) config.V3App {
		wd := d.Workdir
		if wd == "." {
			wd = ""
		}
		return config.V3App{
			AppConfig: config.AppConfig{
				Type:      d.Type,
				WorkDir:   wd,
				Command:   d.Command,
				Readiness: d.Readiness,
			},
		}
	}
	appsEqual := func(cur config.V3App, d detect.DetectedApp) bool {
		want := v3FromDetected(d)
		if strings.Join(cur.Command, "\x00") != strings.Join(want.Command, "\x00") {
			return false
		}
		cw := cur.WorkDir
		ww := want.WorkDir
		if cw == "." {
			cw = ""
		}
		if ww == "." {
			ww = ""
		}
		if cw != ww {
			return false
		}
		if cur.Type != want.Type {
			return false
		}
		if strings.Join(cur.Readiness, "\x00") != strings.Join(want.Readiness, "\x00") {
			return false
		}
		return true
	}
	type change struct {
		name string
		kind string
	}
	var actualChanges []change
	var proposalChanges []proposalItem
	needsActual := false
	needsProposal := false
	for name, d := range detMap {
		cur, ok := curApps[name]
		if !ok {
			actualChanges = append(actualChanges, change{name, "add"})
			needsActual = true
			continue
		}
		if isAuto(name) {
			if !appsEqual(cur, d) {
				actualChanges = append(actualChanges, change{name, "update"})
				needsActual = true
			}
		} else {
			if !appsEqual(cur, d) {
				needsProposal = true
				proposalChanges = append(proposalChanges, proposalItem{App: name, Action: "update", Reason: "detected command differs from your edited app"})
			}
		}
	}
	for name := range curApps {
		if _, ok := detMap[name]; !ok && isAuto(name) {
			actualChanges = append(actualChanges, change{name, "remove"})
			needsActual = true
		} else if _, ok := detMap[name]; !ok && !isAuto(name) {
			if _, inAuto := autoMap[name]; inAuto {
				continue
			}
		}
	}
	for name := range curApps {
		if _, ok := detMap[name]; !ok && !isAuto(name) {
			if _, hadAuto := autoMap[name]; hadAuto {
				needsProposal = true
				proposalChanges = append(proposalChanges, proposalItem{App: name, Action: "remove", Reason: "no longer detected; your edited app kept"})
			}
		}
	}
	if len(detected) == 0 && len(curApps) == 0 {
		_ = pr.store.PutConfigSync(projectID, sig, syncRow.AutoAppsJSON)
		return
	}
	if !needsActual && !needsProposal && len(projYAML) > 0 {
		_ = pr.store.PutConfigSync(projectID, sig, syncRow.AutoAppsJSON)
		return
	}
	if len(projYAML) == 0 && len(detected) > 0 {
		newYAML, newAuto := buildFreshYAML(detMap, v3FromDetected)
		rev, err := pr.store.PutConfigTx(projectID, "project", "", newYAML, config.SHA256([]byte(newYAML)), true, "", "auto", "", "auto-generate config")
		if err == nil && rev != 0 {
			aj, _ := json.Marshal(newAuto)
			_ = pr.store.PutConfigSync(projectID, sig, string(aj))
			pr.reloadFromDB()
		} else {
			_ = pr.store.PutConfigSync(projectID, sig, syncRow.AutoAppsJSON)
		}
		return
	}
	if needsActual {
		actualYAML := applyChangesToYAML(projYAML, curApps, detMap, v3FromDetected, true, isAuto, appsEqual)
		if actualYAML != string(projYAML) {
			rev, err := pr.store.PutConfigTx(projectID, "project", "", actualYAML, config.SHA256([]byte(actualYAML)), true, "", "auto", "", "auto-sync config")
			if err == nil && rev != 0 {
				newApps := map[string]config.V3App{}
				if cfg, errs := config.ParseV3([]byte(actualYAML)); errs == nil {
					newApps = cfg.Apps
				}
				newAuto := map[string]string{}
				for k, v := range autoMap {
					if _, ok := newApps[k]; ok {
						newAuto[k] = v
					}
				}
				for _, ch := range actualChanges {
					if ch.kind == "remove" {
						delete(newAuto, ch.name)
						continue
					}
					if app, ok := newApps[ch.name]; ok {
						newAuto[ch.name] = hashApp(app)
					}
				}
				aj, _ := json.Marshal(newAuto)
				_ = pr.store.PutConfigSync(projectID, sig, string(aj))
				pr.reloadFromDB()
				syncRow.AutoAppsJSON = string(aj)
				curApps = newApps
				projYAML = []byte(actualYAML)
			}
		} else {
			_ = pr.store.PutConfigSync(projectID, sig, syncRow.AutoAppsJSON)
		}
	}
	if needsProposal {
		proposedYAML := applyChangesToYAML(projYAML, curApps, detMap, v3FromDetected, false, isAuto, appsEqual)
		sort.Slice(proposalChanges, func(i, j int) bool { return proposalChanges[i].App < proposalChanges[j].App })
		prop := storedProposal{
			ID:        config.SHA256([]byte(proposedYAML))[:16],
			YAML:      proposedYAML,
			Summary:   proposalChanges,
			CreatedAt: time.Now().Unix(),
		}
		pj, _ := json.Marshal(prop)
		_ = pr.store.SetProposal(projectID, string(pj))
		if !needsActual {
			_ = pr.store.PutConfigSync(projectID, sig, syncRow.AutoAppsJSON)
		}
		return
	}
	if !needsProposal {
		_ = pr.store.ClearProposal(projectID)
	}
	_ = pr.store.PutConfigSync(projectID, sig, syncRow.AutoAppsJSON)
}

func buildFreshYAML(detMap map[string]detect.DetectedApp, conv func(detect.DetectedApp) config.V3App) (string, map[string]string) {
	apps := map[string]config.V3App{}
	auto := map[string]string{}
	for name, d := range detMap {
		v := conv(d)
		apps[name] = v
		auto[name] = hashApp(v)
	}
	return encodeAppsYAML(apps), auto
}

func encodeAppsYAML(apps map[string]config.V3App) string {
	type doc struct {
		Version int                     `yaml:"version"`
		Apps    map[string]config.V3App `yaml:"apps"`
	}
	d := doc{Version: 3, Apps: apps}
	b, err := yaml.Marshal(&d)
	if err != nil {
		return "version: 3\napps: {}\n"
	}
	return string(b)
}

func applyChangesToYAML(curYAML []byte, curApps map[string]config.V3App, detMap map[string]detect.DetectedApp, conv func(detect.DetectedApp) config.V3App, autoOnly bool, isAuto func(string) bool, appsEqual func(config.V3App, detect.DetectedApp) bool) string {
	if len(strings.TrimSpace(string(curYAML))) == 0 {
		apps := map[string]config.V3App{}
		for name, d := range detMap {
			apps[name] = conv(d)
		}
		return encodeAppsYAML(apps)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(curYAML, &root); err != nil || len(root.Content) == 0 {
		apps := map[string]config.V3App{}
		for k, v := range curApps {
			apps[k] = v
		}
		for name, d := range detMap {
			if _, ok := apps[name]; !ok {
				apps[name] = conv(d)
			}
		}
		return encodeAppsYAML(apps)
	}
	docNode := root.Content[0]
	appsNode := findMapValue(docNode, "apps")
	if appsNode == nil {
		appsNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		docNode.Content = append(docNode.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "apps"}, appsNode)
	}
	setApp := func(name string, app config.V3App) {
		b, _ := yaml.Marshal(app)
		var n yaml.Node
		_ = yaml.Unmarshal(b, &n)
		var val *yaml.Node
		if len(n.Content) > 0 {
			val = n.Content[0]
			if val.Kind == yaml.MappingNode && len(val.Content) == 0 {
				val = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
		} else {
			val = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		for i := 0; i+1 < len(appsNode.Content); i += 2 {
			if appsNode.Content[i].Value == name {
				appsNode.Content[i+1] = val
				return
			}
		}
		appsNode.Content = append(appsNode.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, val)
	}
	removeApp := func(name string) {
		for i := 0; i+1 < len(appsNode.Content); i += 2 {
			if appsNode.Content[i].Value == name {
				appsNode.Content = append(appsNode.Content[:i], appsNode.Content[i+2:]...)
				return
			}
		}
	}
	for name, d := range detMap {
		cur, ok := curApps[name]
		if !ok {
			setApp(name, conv(d))
			continue
		}
		if autoOnly {
			if isAuto(name) && !appsEqual(cur, d) {
				setApp(name, conv(d))
			}
		} else {
			if !appsEqual(cur, d) {
				setApp(name, conv(d))
			}
		}
	}
	for name := range curApps {
		if _, ok := detMap[name]; !ok && isAuto(name) {
			removeApp(name)
		}
	}
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	_ = enc.Encode(docNode)
	_ = enc.Close()
	return sb.String()
}

func findMapValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func (e *Engine) ApplyProjectYAML(projectID, layer, workspaceID, yamlText, source, session, message string, restartAffected bool) (int64, []string, error) {
	if errs := config.Validate([]byte(yamlText)); len(errs) > 0 {
		return 0, nil, errs[0]
	}
	mu := projectLock(projectID)
	mu.Lock()
	defer mu.Unlock()
	rev, err := e.store.PutConfigTx(projectID, layer, workspaceID, yamlText, config.SHA256([]byte(yamlText)), true, "", source, session, message)
	if err != nil {
		return 0, nil, err
	}
	wss, _ := e.store.ListWorkspaces(projectID)
	var restarted []string
	for _, ws := range wss {
		prVal, ok := e.runtimes.Load(ws.ID)
		if !ok {
			continue
		}
		pr := prVal.(*ProjectRuntime)
		oldApps := map[string]config.V3App{}
		if res := pr.ResolvedConfig(); res != nil {
			oldApps = res.Apps
		}
		pr.reloadFromDB()
		if !restartAffected {
			continue
		}
		res := pr.ResolvedConfig()
		if res == nil {
			continue
		}
		running := map[string][]string{}
		if rows, err := e.store.ListProcesses(ws.ID); err == nil {
			for _, row := range rows {
				if row.App != "" {
					running[row.App] = append(running[row.App], row.ID)
				}
			}
		}
		for _, ch := range config.Plan(oldApps, res.Apps, running) {
			if ch.Kind != config.ChangeRestartRequired {
				continue
			}
			rt, err := pr.Runtime()
			if err != nil {
				continue
			}
			for _, pid := range ch.AffectedProcIDs {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := rt.Restart(ctx, pid); err == nil {
					restarted = append(restarted, pid)
				}
				cancel()
			}
		}
	}
	return rev, restarted, nil
}

func LearnFromRawStart(e *Engine, workspaceID string, command []string, workdir, sessionID string) {
	if len(command) == 0 {
		return
	}
	prVal, ok := e.runtimes.Load(workspaceID)
	if !ok {
		return
	}
	pr := prVal.(*ProjectRuntime)
	mu := projectLock(pr.projectID)
	mu.Lock()
	defer mu.Unlock()
	res := pr.ResolvedConfig()
	if res == nil {
		return
	}
	normWD := normalizeWorkdir(pr.wsPath, workdir)
	for _, app := range res.Apps {
		aw := app.WorkDir
		if aw == "" {
			aw = "."
		}
		if strings.Join(app.Command, "\x00") == strings.Join(command, "\x00") && aw == normWD {
			return
		}
	}
	base := command[0]
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if base == "" {
		base = "app"
	}
	name := base
	if _, exists := res.Apps[name]; exists {
		for i := 2; ; i++ {
			cand := fmt.Sprintf("%s-%d", base, i)
			if _, exists := res.Apps[cand]; !exists {
				name = cand
				break
			}
		}
	}
	projYAML, _, _ := e.store.GetConfig(pr.projectID, "project", "")
	curApps := map[string]config.V3App{}
	if len(projYAML) > 0 {
		if cfg, errs := config.ParseV3(projYAML); errs == nil {
			curApps = cfg.Apps
		}
	}
	wd := normWD
	if wd == "." {
		wd = ""
	}
	newApp := config.V3App{}
	newApp.Command = append([]string(nil), command...)
	newApp.WorkDir = wd
	curApps[name] = newApp
	newYAML := encodeAppsYAML(curApps)
	rev, err := e.store.PutConfigTx(pr.projectID, "project", "", newYAML, config.SHA256([]byte(newYAML)), true, "", "learned", sessionID, "learned from raw start")
	if err != nil || rev == 0 {
		return
	}
	syncRow, _ := e.store.GetConfigSync(pr.projectID)
	autoMap := map[string]string{}
	if syncRow != nil && syncRow.AutoAppsJSON != "" {
		_ = json.Unmarshal([]byte(syncRow.AutoAppsJSON), &autoMap)
	}
	autoMap[name] = hashApp(newApp)
	aj, _ := json.Marshal(autoMap)
	sig := detect.Signature(pr.wsPath)
	_ = e.store.PutConfigSync(pr.projectID, sig, string(aj))
	pr.reloadFromDB()
}

func normalizeWorkdir(wsPath, wd string) string {
	if wd == "" {
		return "."
	}
	if len(wd) > 0 && wd[0] == '/' {
		rel := wd
		if wsPath != "" && strings.HasPrefix(wd, wsPath) {
			rel = strings.TrimPrefix(wd, wsPath)
			rel = strings.TrimPrefix(rel, "/")
			if rel == "" {
				return "."
			}
			return rel
		}
		return rel
	}
	if wd == "." || wd == "./" {
		return "."
	}
	return strings.TrimPrefix(wd, "./")
}
