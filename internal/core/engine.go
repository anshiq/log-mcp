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
	"runtime/debug"
	"sync"
	"time"

	"agent-runtime/internal/project"
	"agent-runtime/internal/session"
	"agent-runtime/internal/store"
)

// Engine is the multi-tenant core of agent-runtime v3.
// One Engine per OS user hosts every project.
type Engine struct {
	store    *store.DB
	projects *project.Registry
	runtimes sync.Map // workspaceID -> *ProjectRuntime
	sessions *session.Registry
	degraded sync.Map // workspaceID -> *degradedInfo

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

type degradedInfo struct {
	at     time.Time
	reason string
	stack  string
}

// ProjectRuntime hosts the runtime for a single workspace.
// It mirrors today's runtime.Runtime minus globals; manager/policy/
// audit/watcher are wired in Phase 3 when the Connect handlers land.
// Until then the struct carries identity + lifecycle so the daemon,
// CLI and tests can resolve, load, unload and degrade per project.
type ProjectRuntime struct {
	wsID      string
	projectID string
	loadedAt  time.Time
	lastUsed  time.Time
	degraded  bool
	mu        sync.RWMutex
}

// New creates a new multi-tenant engine.
func New(db *store.DB) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{
		store:    db,
		projects: project.NewRegistry(storeAdapter{db}),
		sessions: session.NewRegistry(),
		ctx:      ctx,
		cancel:   cancel,
	}
	return e
}

// Store returns the state database (daemon is the only writer).
func (e *Engine) Store() *store.DB { return e.store }

// Sessions returns the session registry.
func (e *Engine) Sessions() *session.Registry { return e.sessions }

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
		pr.mu.Lock()
		pr.lastUsed = time.Now()
		pr.mu.Unlock()
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
		loadedAt:  time.Now(),
		lastUsed:  time.Now(),
	}
	e.runtimes.Store(workspaceID, rt)
	return rt, nil
}

// UnloadRuntime drops an idle runtime (kept while it has live processes
// or connected sessions; otherwise unloaded after 10 min per §3.6).
func (e *Engine) UnloadRuntime(workspaceID string) {
	e.runtimes.Delete(workspaceID)
}

// Close unloads all runtimes and stops the engine.
func (e *Engine) Close() error {
	e.cancel()
	e.runtimes.Range(func(_, _ any) bool { return true })
	return nil
}

// ResolveWorkspace resolves a path to a project and workspace.
func (e *Engine) ResolveWorkspace(path string) (project.ID, project.WorkspaceID, error) {
	return e.projects.Resolve(path)
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
