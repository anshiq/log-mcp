// Package core contains the multi-tenant engine that hosts N
// project runtimes. A single Engine owns every managed process
// across all projects, the log pipeline, the event bus, and the
// state database.
//
// Isolation: every goroutine started for a project runs under Go()
// which recovers panics, scopes degradation to that ProjectRuntime,
// and keeps the rest of the daemon serving (§3.9).
package core

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"agent-runtime/internal/project"
	iruntime "agent-runtime/internal/runtime"
	"agent-runtime/internal/session"
	"agent-runtime/internal/store"
)

// Engine is the multi-tenant core of agent-runtime v3.
// One Engine per OS user hosts every project.
type Engine struct {
	store    *store.DB
	dataDir  string
	logger   *slog.Logger
	version  string
	projects *project.Registry
	runtimes sync.Map // workspaceID -> *ProjectRuntime
	sessions *session.Registry
	mcpSubs  MCPSubscriptions
	degraded sync.Map // workspaceID -> *degradedInfo
	// forwarded tracks workspaces with an active event forwarder.
	forwarded map[string]bool
	// alerted tracks workspaces with an active alert poller.
	alerted map[string]bool

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

type degradedInfo struct {
	at     time.Time
	reason string
	stack  string
}

// Options configures the engine.
type Options struct {
	DataDir string
	Logger  *slog.Logger
	Version string
}

// New creates a new multi-tenant engine.
func New(db *store.DB) *Engine {
	return NewWithOptions(db, Options{})
}

// NewWithOptions creates an engine with data dir / logger / version.
func NewWithOptions(db *store.DB, opt Options) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	if opt.Logger == nil {
		opt.Logger = slog.Default()
	}
	e := &Engine{
		store:    db,
		dataDir:  opt.DataDir,
		logger:   opt.Logger,
		version:  opt.Version,
		projects: project.NewRegistry(storeAdapter{db}),
		sessions: session.NewRegistry(),
		ctx:      ctx,
		cancel:   cancel,
	}
	go e.reapLoop()
	return e
}

// Store returns the state database (daemon is the only writer).
func (e *Engine) Store() *store.DB { return e.store }

// DataDir returns the daemon data dir (central storage root).
func (e *Engine) DataDir() string { return e.dataDir }

// Sessions returns the session registry.
func (e *Engine) Sessions() *session.Registry { return e.sessions }

// MCPSubscriptions returns the pull-model subscription tracker.
func (e *Engine) MCPSubscriptions() *MCPSubscriptions { return &e.mcpSubs }

// Go runs fn scoped to a project: panics are recovered, logged with a
// stack, counted as degraded, and never take the daemon down.
func (e *Engine) Go(workspaceID string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				e.markDegraded(workspaceID, fmt.Sprintf("%v", r), string(debug.Stack()))
			}
		}()
		fn()
	}()
}

func (e *Engine) markDegraded(workspaceID, reason, stack string) {
	e.degraded.Store(workspaceID, &degradedInfo{at: time.Now(), reason: reason, stack: stack})
	if rt, ok := e.runtimes.Load(workspaceID); ok {
		rt.(*ProjectRuntime).mu.Lock()
		rt.(*ProjectRuntime).degraded = true
		rt.(*ProjectRuntime).mu.Unlock()
	}
}

// IsDegraded reports whether a workspace runtime is degraded.
func (e *Engine) IsDegraded(workspaceID string) (bool, string) {
	if v, ok := e.degraded.Load(workspaceID); ok {
		return true, v.(*degradedInfo).reason
	}
	return false, ""
}

// GetOrCreateRuntime returns or creates a ProjectRuntime for the
// given workspace. Lazy-initialized; double-checked under lock.
func (e *Engine) GetOrCreateRuntime(workspaceID string) (*ProjectRuntime, error) {
	if rt, ok := e.runtimes.Load(workspaceID); ok {
		pr := rt.(*ProjectRuntime)
		pr.touch()
		return pr, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if rt, ok := e.runtimes.Load(workspaceID); ok {
		return rt.(*ProjectRuntime), nil
	}

	ws, err := e.store.GetWorkspace(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("core: workspace %s: %w", workspaceID, err)
	}

	rt := &ProjectRuntime{
		wsID:      ws.ID,
		projectID: ws.ProjectID,
		wsPath:    ws.Path,
		dataDir:   e.dataDir,
		logger:    e.logger,
		loadedAt:  time.Now(),
		lastUsed:  time.Now(),
		stale:     map[string]bool{},
		allocPort: e.AllocatePort,
		trusted: func(workspaceID, path, sha string) bool {
			ok, err := e.store.IsTrusted(workspaceID, path)
			if err != nil || !ok {
				return false
			}
			have, found, err := e.store.RepoTrustSHA(workspaceID, path)
			if err != nil || !found {
				return false
			}
			return have == sha
		},
	}
	e.runtimes.Store(workspaceID, rt)
	_ = e.store.TouchProject(ws.ProjectID)
	return rt, nil
}

// forwardEvents persists a runtime's manager-bus events into the events
// table so WatchEvents/ListEvents serve history + live from one cursor.
// At most one forwarder runs per workspace (guarded by forwarded set).
func (e *Engine) forwardEvents(pr *ProjectRuntime) {
	e.mu.Lock()
	if e.forwarded == nil {
		e.forwarded = map[string]bool{}
	}
	if e.forwarded[pr.wsID] {
		e.mu.Unlock()
		return
	}
	e.forwarded[pr.wsID] = true
	e.mu.Unlock()

	bus := pr.ManagerBus()
	if bus == nil {
		e.mu.Lock()
		delete(e.forwarded, pr.wsID)
		e.mu.Unlock()
		return
	}
	ch, unsub := bus.Subscribe()
	e.Go(pr.wsID, func() {
		defer unsub()
		for ev := range ch {
			_, _ = e.store.AppendEvent(
				ev.Timestamp.UnixNano(), string(ev.Type),
				pr.projectID, pr.wsID, ev.ProcessID, ev.InstanceID, "", "",
			)
		}
	})
}

// UnloadRuntime drops an idle runtime and shuts its processes down.
func (e *Engine) UnloadRuntime(workspaceID string) {
	if rt, ok := e.runtimes.LoadAndDelete(workspaceID); ok {
		rt.(*ProjectRuntime).shutdown()
	}
}

// reapLoop unloads runtimes idle for 10+ minutes (no live processes and
// no connected sessions) and reaps dead sessions for lease expiry.
func (e *Engine) reapLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-t.C:
			e.reapOnce()
		}
	}
}

func (e *Engine) reapOnce() {
	for _, id := range e.sessions.ReapExpired(5 * time.Minute) {
		e.logger.Info("reaped idle session", "session", id)
	}
	e.runtimes.Range(func(key, val any) bool {
		pr := val.(*ProjectRuntime)
		if time.Since(pr.IdleSince()) < 10*time.Minute {
			return true
		}
		if len(e.sessions.ListByWorkspace(key.(string))) > 0 {
			return true
		}
		if rt, err := pr.Runtime(); err == nil {
			if procs, err := rt.List(); err == nil && len(procs.Processes) > 0 {
				return true
			}
		}
		e.UnloadRuntime(key.(string))
		return true
	})
}

// Close unloads all runtimes and stops the engine.
func (e *Engine) Close() error {
	e.cancel()
	e.runtimes.Range(func(key, val any) bool {
		val.(*ProjectRuntime).shutdown()
		e.runtimes.Delete(key)
		return true
	})
	return nil
}

// RangeRuntimes iterates loaded runtimes. The callback returns false to stop.
func (e *Engine) RangeRuntimes(fn func(*ProjectRuntime) bool) {
	e.runtimes.Range(func(_, val any) bool {
		return fn(val.(*ProjectRuntime))
	})
}

// LoadedRuntimes returns all currently loaded runtimes.
func (e *Engine) LoadedRuntimes() []*ProjectRuntime {
	var out []*ProjectRuntime
	e.RangeRuntimes(func(pr *ProjectRuntime) bool {
		out = append(out, pr)
		return true
	})
	return out
}

// ResolveWorkspace resolves a path to a project and workspace.
func (e *Engine) ResolveWorkspace(path string) (project.ID, project.WorkspaceID, error) {
	return e.projects.Resolve(path)
}

// RuntimeFor returns the hosted process runtime for a workspace, loading
// it on first use and attaching the event forwarder.
func (e *Engine) RuntimeFor(workspaceID string) (*ProjectRuntime, *iruntime.Runtime, error) {
	pr, err := e.GetOrCreateRuntime(workspaceID)
	if err != nil {
		return nil, nil, err
	}
	rt, err := pr.Runtime()
	if err != nil {
		return nil, nil, err
	}
	e.forwardEvents(pr)
	e.ensureAlerts(pr)
	return pr, rt, nil
}

// ensureAlerts starts the log-alert poller once per workspace.
func (e *Engine) ensureAlerts(pr *ProjectRuntime) {
	e.mu.Lock()
	if e.alerted == nil {
		e.alerted = map[string]bool{}
	}
	if e.alerted[pr.wsID] {
		e.mu.Unlock()
		return
	}
	e.alerted[pr.wsID] = true
	e.mu.Unlock()
	e.Go(pr.wsID, func() { e.runAlerts(pr) })
}

// storeAdapter implements project.Store over *store.DB, converting
// string IDs to the typed IDs. It lives here (not in project or store)
// to avoid an import cycle.
type storeAdapter struct {
	db *store.DB
}

func (a storeAdapter) ResolveWorkspace(path string) (*project.Workspace, error) {
	ws, err := a.db.ResolveWorkspace(path)
	if err != nil {
		return nil, err
	}
	return cvtWorkspace(ws), nil
}

func (a storeAdapter) GetWorkspaceAtID(id project.WorkspaceID) (*project.Workspace, error) {
	ws, err := a.db.GetWorkspaceAtID(string(id))
	if err != nil {
		return nil, err
	}
	return cvtWorkspace(ws), nil
}

func (a storeAdapter) CreateProject(name string) (*project.Project, error) {
	p, err := a.db.CreateProject(name)
	if err != nil {
		return nil, err
	}
	return cvtProject(p), nil
}

func (a storeAdapter) GetProjectByName(name string) (*project.Project, error) {
	p, err := a.db.GetProjectByName(name)
	if err != nil {
		return nil, err
	}
	return cvtProject(p), nil
}

func (a storeAdapter) GetProject(id project.ID) (*project.Project, error) {
	p, err := a.db.GetProject(string(id))
	if err != nil {
		return nil, err
	}
	return cvtProject(p), nil
}

func (a storeAdapter) CreateWorkspace(projectID project.ID, path string) (*project.Workspace, error) {
	ws, err := a.db.CreateWorkspace(string(projectID), path)
	if err != nil {
		return nil, err
	}
	return cvtWorkspace(ws), nil
}

func (a storeAdapter) UpdateWorkspaceLastSeen(id project.WorkspaceID) error {
	return a.db.UpdateWorkspaceLastSeen(string(id))
}

func (a storeAdapter) ListProjects() ([]*project.Project, error) {
	ps, err := a.db.ListProjects()
	if err != nil {
		return nil, err
	}
	out := make([]*project.Project, 0, len(ps))
	for _, p := range ps {
		out = append(out, cvtProject(p))
	}
	return out, nil
}

func (a storeAdapter) UpdateProject(p *project.Project) error {
	return a.db.UpdateProject(&store.Project{ID: string(p.ID), Name: p.Name})
}

func (a storeAdapter) DeleteProject(id project.ID) error {
	return a.db.DeleteProject(string(id))
}

func (a storeAdapter) ListWorkspaces(projectID project.ID) ([]*project.Workspace, error) {
	wss, err := a.db.ListWorkspaces(string(projectID))
	if err != nil {
		return nil, err
	}
	out := make([]*project.Workspace, 0, len(wss))
	for _, w := range wss {
		out = append(out, cvtWorkspace(w))
	}
	return out, nil
}

func (a storeAdapter) LinkWorkspace(projectID project.ID, path string) (*project.Workspace, error) {
	ws, err := a.db.LinkWorkspace(string(projectID), path)
	if err != nil {
		return nil, err
	}
	return cvtWorkspace(ws), nil
}

func (a storeAdapter) ForgetProject(id project.ID) error {
	return a.db.ForgetProject(string(id))
}

func (a storeAdapter) GC() ([]string, error) { return a.db.GC() }

func (a storeAdapter) AddFingerprint(projectID project.ID, kind, value string, weak bool) error {
	return a.db.AddFingerprint(string(projectID), kind, value, weak)
}

func (a storeAdapter) GetProjectByFingerprints(fps []project.Fingerprint) ([]*project.Project, error) {
	in := make([]store.Fingerprint, 0, len(fps))
	for _, f := range fps {
		in = append(in, store.Fingerprint{ProjectID: string(f.ProjectID), Kind: f.Kind, Value: f.Value, Weak: f.Weak})
	}
	ps, err := a.db.GetProjectByFingerprints(in)
	if err != nil {
		return nil, err
	}
	out := make([]*project.Project, 0, len(ps))
	for _, p := range ps {
		out = append(out, cvtProject(p))
	}
	return out, nil
}

func (a storeAdapter) TrustRepo(ws project.WorkspaceID, path, sha string, by string) error {
	return a.db.TrustRepo(string(ws), path, sha, by)
}

func (a storeAdapter) IsTrusted(ws project.WorkspaceID, path string) (bool, error) {
	return a.db.IsTrusted(string(ws), path)
}

func (a storeAdapter) FindWorkspaceByDevIno(dev, ino uint64) (*project.Workspace, error) {
	ws, err := a.db.FindWorkspaceByDevIno(dev, ino)
	if err != nil {
		return nil, err
	}
	return cvtWorkspace(ws), nil
}

func (a storeAdapter) FindWorkspacesByGitCommon(common string) ([]*project.Workspace, error) {
	wss, err := a.db.FindWorkspacesByGitCommon(common)
	if err != nil {
		return nil, err
	}
	out := make([]*project.Workspace, 0, len(wss))
	for _, w := range wss {
		out = append(out, cvtWorkspace(w))
	}
	return out, nil
}

func (a storeAdapter) UpdateWorkspacePath(id project.WorkspaceID, path string, dev, ino uint64, gitCommon string) error {
	return a.db.UpdateWorkspacePath(string(id), path, dev, ino, gitCommon)
}

func (a storeAdapter) TouchProject(id project.ID) error {
	return a.db.TouchProject(string(id))
}

func cvtProject(p *store.Project) *project.Project {
	return &project.Project{
		ID:         project.ID(p.ID),
		Name:       p.Name,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
		LastUsedAt: p.LastUsedAt,
		ConfigMode: p.ConfigMode,
		ActiveRev:  p.ActiveRev,
		DeletedAt:  p.DeletedAt,
	}
}

func cvtWorkspace(w *store.Workspace) *project.Workspace {
	return &project.Workspace{
		ID:           project.WorkspaceID(w.ID),
		ProjectID:    project.ID(w.ProjectID),
		Path:         w.Path,
		Dev:          w.Dev,
		Ino:          w.Ino,
		GitCommonDir: w.GitCommonDir,
		GitWorktree:  w.GitWorktree,
		BranchHint:   w.BranchHint,
		Confirmed:    w.Confirmed,
		CreatedAt:    w.CreatedAt,
		LastSeenAt:   w.LastSeenAt,
		MissingSince: w.MissingSince,
	}
}
