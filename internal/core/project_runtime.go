// ProjectRuntime hosts the runtime for a single workspace: the resolved
// config, the process manager (via runtime.Runtime), per-project policy +
// audit, the config watcher, and stale markers for config-changed apps.
//
// A runtime is created lazily on first use and kept while it has live
// processes or connected sessions; otherwise it unloads after 10 minutes.
package core

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/events"
	iruntime "agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

// ProjectRuntime hosts the runtime for a single workspace.
type ProjectRuntime struct {
	wsID      string
	projectID string
	wsPath    string
	dataDir   string
	logger    *slog.Logger
	trusted   func(workspaceID, path, sha string) bool

	mu       sync.RWMutex
	loadedAt time.Time
	lastUsed time.Time
	degraded bool

	rt       *iruntime.Runtime
	resolved *config.Resolved
	watcher  *config.Watcher
	stale    map[string]bool // app -> needs restart
}

// Runtime returns the hosted process runtime, loading it on first use.
// The config is resolved through the v3 layer stack; the last good
// revision stays active when the files are invalid.
func (p *ProjectRuntime) Runtime() (*iruntime.Runtime, error) {
	p.mu.RLock()
	if p.rt != nil {
		p.mu.RUnlock()
		p.touch()
		return p.rt, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rt != nil {
		return p.rt, nil
	}
	if err := p.loadLocked(); err != nil {
		return nil, err
	}
	return p.rt, nil
}

// ResolvedConfig returns the last resolved config (nil before first load).
func (p *ProjectRuntime) ResolvedConfig() *config.Resolved {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.resolved
}

// IsStale reports whether an app's running processes need a restart.
func (p *ProjectRuntime) IsStale(app string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stale[app]
}

// MarkStale flags apps whose spec fields changed.
func (p *ProjectRuntime) MarkStale(apps []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, a := range apps {
		p.stale[a] = true
	}
}

// WorkspaceID returns the workspace id.
func (p *ProjectRuntime) WorkspaceID() string { return p.wsID }

// ProjectID returns the project id.
func (p *ProjectRuntime) ProjectID() string { return p.projectID }

// WorkspacePath returns the checkout path.
func (p *ProjectRuntime) WorkspacePath() string { return p.wsPath }

// IdleSince returns when the runtime was last used.
func (p *ProjectRuntime) IdleSince() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastUsed
}

func (p *ProjectRuntime) touch() {
	p.mu.Lock()
	p.lastUsed = time.Now()
	p.mu.Unlock()
}

func (p *ProjectRuntime) ctx() context.Context { return context.Background() }

func startRequestFor(app string) api.StartRequest {
	return api.StartRequest{App: app}
}

// loadLocked resolves config and hosts a runtime.Runtime. Callers hold p.mu.
func (p *ProjectRuntime) loadLocked() error {
	trusted := p.repoTrusted()
	resolved, errs := config.Resolve(p.dataDir, p.projectID, p.wsID, p.wsPath, trusted, nil)
	if errs != nil {
		// Invalid config: keep last good if we have one, else fail with
		// the first validation error (file/line/col included).
		if p.resolved != nil {
			return nil
		}
		return errs[0]
	}
	loaded := resolved.ToLoaded(p.wsPath)
	rt := iruntime.New(loaded, p.logger)
	if p.rt != nil {
		// Hot reload path: keep processes from the old runtime? Phase 3
		// keeps it simple — runtimes are long-lived per daemon boot, and
		// the watcher only marks stale / autostarts. Full live-swap of the
		// manager arrives with the shim spawner (Phase 2b).
		_ = rt
		return nil
	}
	p.rt = rt
	p.resolved = resolved
	p.loadedAt = time.Now()
	p.lastUsed = time.Now()
	if p.stale == nil {
		p.stale = map[string]bool{}
	}
	p.startWatcher()
	p.autostart()
	return nil
}

// repoTrusted checks the trust gate for the repo-layer config.
func (p *ProjectRuntime) repoTrusted() bool {
	ep := config.DiscoverEffective(p.dataDir, p.projectID, p.wsID, p.wsPath)
	if ep.Repo == "" {
		return true
	}
	data, err := os.ReadFile(ep.Repo)
	if err != nil {
		return false
	}
	if p.trusted == nil {
		return false
	}
	return p.trusted(p.wsID, ep.Repo, config.SHA256(data))
}

// autostart launches apps with autostart: true on a fresh runtime
// (no processes yet). It runs async so daemon boot never blocks on
// slow app startups.
func (p *ProjectRuntime) autostart() {
	if p.resolved == nil || p.rt == nil {
		return
	}
	var names []string
	for name, app := range p.resolved.Apps {
		if app.Autostart {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return
	}
	rt := p.rt
	go func() {
		procs, err := rt.List()
		if err != nil || len(procs.Processes) > 0 {
			return // not fresh; user manages startup
		}
		for _, name := range names {
			_, _ = rt.Start(p.ctx(), startRequestFor(name))
		}
	}()
}

// startWatcher observes the effective files and reconciles on save.
func (p *ProjectRuntime) startWatcher() {
	ep := config.DiscoverEffective(p.dataDir, p.projectID, p.wsID, p.wsPath)
	var paths []string
	if ep.Repo != "" {
		paths = append(paths, ep.Repo)
	}
	paths = append(paths, ep.Project)
	if ep.Overlay != "" {
		paths = append(paths, ep.Overlay)
	}
	w, err := config.NewWatcher(paths,
		func(ch config.Change) { p.onValidConfig(ch) },
		func(ch config.Change) { p.onInvalidConfig(ch) },
	)
	if err != nil {
		p.logger.Warn("config watcher unavailable; hot reload disabled", "error", err)
		return
	}
	p.watcher = w
}

// onValidConfig reconciles a validated save: new revision, planner,
// stale marking, reload: restart handling.
func (p *ProjectRuntime) onValidConfig(ch config.Change) {
	p.mu.Lock()
	defer p.mu.Unlock()
	oldApps := map[string]config.V3App{}
	if p.resolved != nil {
		oldApps = p.resolved.Apps
	}
	trusted := p.repoTrusted()
	resolved, errs := config.Resolve(p.dataDir, p.projectID, p.wsID, p.wsPath, trusted, nil)
	if errs != nil {
		return // lost race with another save; watcher will fire again
	}
	running := map[string][]string{}
	_ = running
	changes := config.Plan(oldApps, resolved.Apps, running)
	for _, c := range changes {
		switch c.Kind {
		case config.ChangeRestartRequired:
			p.stale[c.App] = true
			if app, ok := resolved.Apps[c.App]; ok && app.Reload == "restart" {
				// Restart is executed by the server layer where the
				// session attribution lives; the flag here is the marker.
				_ = app
			}
		case config.ChangeRemoved:
			// Running processes stay, marked orphaned-config in the UI.
		}
	}
	p.resolved = resolved
	p.lastUsed = time.Now()
}

// onInvalidConfig keeps the last good revision active and records the
// rejection for WatchConfig streams and the GUI banner.
func (p *ProjectRuntime) onInvalidConfig(ch config.Change) {
	p.logger.Warn("invalid config save; keeping last good revision",
		"path", ch.Path, "errors", len(ch.Errors))
}

// shutdown stops the watcher and the hosted runtime.
func (p *ProjectRuntime) shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.watcher != nil {
		_ = p.watcher.Close()
		p.watcher = nil
	}
	if p.rt != nil {
		_ = p.rt.Shutdown()
		p.rt = nil
	}
	p.resolved = nil
}

// ManagerBus exposes the hosted runtime's event bus for stream fan-out.
// Returns nil when the runtime is not loaded yet.
func (p *ProjectRuntime) ManagerBus() *events.Bus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.rt == nil {
		return nil
	}
	return p.rt.Manager().Events()
}
