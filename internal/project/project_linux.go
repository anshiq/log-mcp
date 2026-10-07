//go:build linux
// +build linux

package project

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ID is a ULID-based project identifier.
type ID string

// WorkspaceID is a ULID-based workspace identifier.
type WorkspaceID string

// Project holds the project-level config and metadata.
type Project struct {
	ID         ID     `json:"id"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
	LastUsedAt int64  `json:"last_used_at"`
	ConfigMode string `json:"config_mode"` // import | repo-linked
	ActiveRev  int    `json:"active_rev"`
	DeletedAt  int64  `json:"deleted_at"`
}

// Workspace holds a concrete checkout on disk.
type Workspace struct {
	ID           WorkspaceID `json:"id"`
	ProjectID    ID          `json:"project_id"`
	Path         string      `json:"path"`
	Dev          uint64      `json:"dev"`
	Ino          uint64      `json:"ino"`
	GitCommonDir string      `json:"git_common_dir"`
	GitWorktree  bool        `json:"git_worktree"`
	BranchHint   string      `json:"branch_hint"`
	Confirmed    bool        `json:"confirmed"`
	CreatedAt    int64       `json:"created_at"`
	LastSeenAt   int64       `json:"last_seen_at"`
	MissingSince int64       `json:"missing_since"`
}

// Fingerprint is a git fingerprint for a project.
type Fingerprint struct {
	ProjectID ID     `json:"project_id"`
	Kind      string `json:"kind"` // root_commit | remote
	Value     string `json:"value"`
	Weak      bool   `json:"weak"`
}

// Resolution captures the outcome of resolving a path.
type Resolution struct {
	ProjectID          ID
	WorkspaceID        WorkspaceID
	ProjectName        string
	NewlyCreated       bool
	UnresolvedLocators []string
}

// Resolver resolves working directories to projects and workspaces.
// It implements the §4.3.2 locator chain:
//
//	exact path → (dev,ino) moved → git_common sibling → fingerprints → new.
type Resolver struct {
	store Store
	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

type cacheKey struct {
	root string
	dev  uint64
	ino  uint64
}

type cacheEntry struct {
	projectID   ID
	workspaceID WorkspaceID
}

// Store is the interface for project/workspace data access.
// It is implemented by internal/store via the adapter in internal/core
// (string IDs convert to ID/WorkspaceID), and by fakes in tests.
type Store interface {
	ResolveWorkspace(path string) (*Workspace, error)
	GetWorkspaceAtID(id WorkspaceID) (*Workspace, error)
	CreateProject(name string) (*Project, error)
	GetProjectByName(name string) (*Project, error)
	GetProject(id ID) (*Project, error)
	CreateWorkspace(projectID ID, path string) (*Workspace, error)
	UpdateWorkspaceLastSeen(id WorkspaceID) error
	ListProjects() ([]*Project, error)
	UpdateProject(p *Project) error
	DeleteProject(id ID) error
	ListWorkspaces(projectID ID) ([]*Workspace, error)
	LinkWorkspace(projectID ID, path string) (*Workspace, error)
	ForgetProject(id ID) error
	GC() ([]string, error)
	AddFingerprint(projectID ID, kind, value string, weak bool) error
	GetProjectByFingerprints(fingerprints []Fingerprint) ([]*Project, error)
	// Locator-chain extensions (optional; resolver degrades gracefully
	// when a Store does not implement them — see storeExtras).
	FindWorkspaceByDevIno(dev, ino uint64) (*Workspace, error)
	FindWorkspacesByGitCommon(common string) ([]*Workspace, error)
	UpdateWorkspacePath(id WorkspaceID, path string, dev, ino uint64, gitCommon string) error
	TouchProject(id ID) error
}

// storeExtras is the optional part of Store. A plain type assertion
// keeps old fakes compiling: if the store lacks these, those steps
// are skipped.
type storeExtras interface {
	FindWorkspaceByDevIno(dev, ino uint64) (*Workspace, error)
	FindWorkspacesByGitCommon(common string) ([]*Workspace, error)
	UpdateWorkspacePath(id WorkspaceID, path string, dev, ino uint64, gitCommon string) error
	TouchProject(id ID) error
}

// NewResolver creates a project resolver.
func NewResolver(store Store) *Resolver {
	return &Resolver{
		store: store,
		cache: make(map[cacheKey]cacheEntry),
	}
}

// WorkspaceRoot determines the workspace root for a cwd: explicit override → git toplevel → cwd itself. Always canonicalised.
func WorkspaceRoot(cwd string) string {
	if v := os.Getenv("AGENT_RUNTIME_PROJECT"); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			if c, err := filepath.EvalSymlinks(abs); err == nil {
				return filepath.Clean(c)
			}
			return filepath.Clean(abs)
		}
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		canonical = abs
	}
	canonical = filepath.Clean(canonical)

	// Git toplevel (worktree-aware via .git file handling).
	if top := gitToplevel(canonical); top != "" {
		return top
	}
	return canonical
}

func gitToplevel(start string) string {
	dir := start
	for {
		gd := filepath.Join(dir, ".git")
		if st, err := os.Stat(gd); err == nil {
			if st.IsDir() {
				return dir
			}
			// Worktree link: .git is a file; the checkout root is still dir.
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Resolve maps root to a project and workspace.
func (r *Resolver) Resolve(root string) (*Resolution, error) {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		// Path may not exist yet (e.g. `project link` pre-creation);
		// fall back to absolute clean path.
		abs, aerr := filepath.Abs(root)
		if aerr != nil {
			return nil, fmt.Errorf("resolve: %w", err)
		}
		canonical = abs
	}
	canonical = filepath.Clean(canonical)

	info, statErr := os.Stat(canonical)
	var dev, ino uint64
	if statErr == nil {
		dev, ino = statDevIno(info.Sys())
	}

	// Warm cache: <5ms budget path, no git probing.
	r.mu.Lock()
	if e, ok := r.cache[cacheKey{canonical, dev, ino}]; ok {
		r.mu.Unlock()
		_ = r.store.UpdateWorkspaceLastSeen(e.workspaceID)
		return &Resolution{ProjectID: e.projectID, WorkspaceID: e.workspaceID}, nil
	}
	r.mu.Unlock()

	// 1. Exact match by path.
	if ws, err := r.store.ResolveWorkspace(canonical); err == nil && ws != nil {
		r.remember(canonical, dev, ino, ws.ProjectID, ws.ID)
		_ = r.store.UpdateWorkspaceLastSeen(ws.ID)
		_ = r.touchProject(ws.ProjectID)
		return &Resolution{ProjectID: ws.ProjectID, WorkspaceID: ws.ID}, nil
	}

	ex, _ := r.extras()

	// 2. Moved: same (dev,ino), old path gone → rebind.
	if statErr == nil && ex != nil {
		if ws, err := ex.FindWorkspaceByDevIno(dev, ino); err == nil && ws != nil {
			if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
				gitCommon, worktree := probeGit(canonical)
				_ = ex.UpdateWorkspacePath(ws.ID, canonical, dev, ino, gitCommon)
				if worktree != ws.GitWorktree && worktree {
					// best-effort; non-fatal
				}
				r.remember(canonical, dev, ino, ws.ProjectID, ws.ID)
				_ = r.store.UpdateWorkspaceLastSeen(ws.ID)
				return &Resolution{ProjectID: ws.ProjectID, WorkspaceID: ws.ID}, nil
			}
		}
	}

	// 3. Git: sibling worktree, re-clone, or second clone.
	if ex != nil {
		if gitDir := findGitDir(canonical); gitDir != "" {
			common := gitCommonDir(gitDir)
			worktree := isWorktree(gitDir)
			// 3a. Sibling worktree of a known clone.
			if siblings, err := ex.FindWorkspacesByGitCommon(common); err == nil && len(siblings) > 0 {
				// Same common dir, different path → new workspace, same project.
				for _, s := range siblings {
					if s.Path == canonical {
						r.remember(canonical, dev, ino, s.ProjectID, s.ID)
						_ = r.store.UpdateWorkspaceLastSeen(s.ID)
						return &Resolution{ProjectID: s.ProjectID, WorkspaceID: s.ID}, nil
					}
				}
				ws, err := r.store.CreateWorkspace(siblings[0].ProjectID, canonical)
				if err != nil {
					return nil, err
				}
				_ = ex.UpdateWorkspacePath(ws.ID, canonical, dev, ino, common)
				_ = r.recordFingerprints(ws.ProjectID, canonical)
				r.remember(canonical, dev, ino, ws.ProjectID, ws.ID)
				return &Resolution{ProjectID: ws.ProjectID, WorkspaceID: ws.ID, NewlyCreated: true}, nil
			}
			// 3b/3c. Fingerprint match.
			fps, weak := probeFingerprints(canonical)
			_ = weak
			if len(fps) > 0 {
				if candidates, err := r.store.GetProjectByFingerprints(fps); err == nil && len(candidates) > 0 {
					chosen, ambiguous := pickCandidate(candidates, ex)
					if !ambiguous && chosen != nil {
						if r.allWorkspacesMissing(chosen.ID) {
							// Re-clone: exactly one candidate, all workspaces
							// missing → rebind first workspace.
							wss, _ := r.store.ListWorkspaces(chosen.ID)
							if len(wss) > 0 {
								_ = ex.UpdateWorkspacePath(wss[0].ID, canonical, dev, ino, common)
								_ = r.store.UpdateWorkspaceLastSeen(wss[0].ID)
								r.remember(canonical, dev, ino, chosen.ID, wss[0].ID)
								return &Resolution{ProjectID: chosen.ID, WorkspaceID: wss[0].ID}, nil
							}
						}
						// Second clone: live workspaces → new workspace, same project.
						ws, err := r.store.CreateWorkspace(chosen.ID, canonical)
						if err != nil {
							return nil, err
						}
						_ = ex.UpdateWorkspacePath(ws.ID, canonical, dev, ino, common)
						_ = r.recordFingerprints(chosen.ID, canonical)
						r.remember(canonical, dev, ino, chosen.ID, ws.ID)
						return &Resolution{ProjectID: chosen.ID, WorkspaceID: ws.ID, NewlyCreated: true}, nil
					}
					// Ambiguous: fall through to create unconfirmed workspace
					// under a fresh project (GUI/CLI surfaces "link?" prompt).
					p, err := r.store.CreateProject(filepath.Base(canonical))
					if err != nil {
						return nil, err
					}
					ws, err := r.store.CreateWorkspace(p.ID, canonical)
					if err != nil {
						return nil, err
					}
					_ = ex.UpdateWorkspacePath(ws.ID, canonical, dev, ino, common)
					_ = r.recordFingerprints(p.ID, canonical)
					r.remember(canonical, dev, ino, p.ID, ws.ID)
					return &Resolution{ProjectID: p.ID, WorkspaceID: ws.ID, ProjectName: p.Name,
						NewlyCreated: true, UnresolvedLocators: []string{"ambiguous-fingerprint"}}, nil
				}
			}
			_ = worktree
		}
	}

	// 4. New project + workspace.
	project, err := r.store.CreateProject(filepath.Base(canonical))
	if err != nil {
		return nil, err
	}
	workspace, err := r.store.CreateWorkspace(project.ID, canonical)
	if err != nil {
		return nil, err
	}
	if ex != nil {
		gitCommon, _ := probeGit(canonical)
		_ = ex.UpdateWorkspacePath(workspace.ID, canonical, dev, ino, gitCommon)
		_ = r.recordFingerprints(project.ID, canonical)
	}
	r.remember(canonical, dev, ino, project.ID, workspace.ID)
	return &Resolution{
		ProjectID:    project.ID,
		WorkspaceID:  workspace.ID,
		ProjectName:  project.Name,
		NewlyCreated: true,
	}, nil
}

func (r *Resolver) remember(root string, dev, ino uint64, p ID, w WorkspaceID) {
	r.mu.Lock()
	r.cache[cacheKey{root, dev, ino}] = cacheEntry{projectID: p, workspaceID: w}
	r.mu.Unlock()
}

func (r *Resolver) extras() (storeExtras, bool) {
	ex, ok := r.store.(storeExtras)
	return ex, ok
}

func (r *Resolver) touchProject(id ID) error {
	if ex, ok := r.extras(); ok {
		return ex.TouchProject(id)
	}
	return nil
}

func (r *Resolver) recordFingerprints(projectID ID, dir string) error {
	fps, weak := probeFingerprints(dir)
	for _, fp := range fps {
		_ = weak
		if err := r.store.AddFingerprint(projectID, fp.Kind, fp.Value, fp.Weak); err != nil {
			return err
		}
	}
	return nil
}

func pickCandidate(candidates []*Project, ex storeExtras) (*Project, bool) {
	if len(candidates) == 1 {
		return candidates[0], false
	}
	return nil, true
}

func (r *Resolver) allWorkspacesMissing(projectID ID) bool {
	wss, err := r.store.ListWorkspaces(projectID)
	if err != nil || len(wss) == 0 {
		return false
	}
	for _, w := range wss {
		if _, err := os.Stat(w.Path); err == nil {
			return false
		}
	}
	return true
}

func allMissing(ex storeExtras, projectID ID) bool {
	return false
}

// allMissingList is the real check used by Resolve: every workspace path
// of the candidate project is gone from disk.

// probeGit returns (commonDir, isWorktree) without shelling out.
func probeGit(dir string) (string, bool) {
	gd := findGitDir(dir)
	if gd == "" {
		return "", false
	}
	return gitCommonDir(gd), isWorktree(gd)
}

func probeFingerprints(dir string) ([]Fingerprint, bool) {
	var out []Fingerprint
	weak := false
	for _, rc := range rootCommits(dir) {
		out = append(out, Fingerprint{Kind: "root_commit", Value: rc})
	}
	remotes := gitRemotes(dir)
	if len(remotes) == 0 {
		weak = true
	}
	for _, rm := range remotes {
		out = append(out, Fingerprint{Kind: "remote", Value: rm})
	}
	if len(out) == 0 {
		return nil, true
	}
	// If root commits unavailable (shallow), mark remote-only as weak.
	if len(rootCommits(dir)) == 0 {
		weak = true
		for i := range out {
			out[i].Weak = true
		}
	}
	return out, weak
}

// rootCommits returns `git rev-list --max-parents=0 HEAD` (may be several
// after subtree merges). Empty on shallow clones or non-git dirs.
func rootCommits(dir string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--max-parents=0", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var res []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			res = append(res, line)
		}
	}
	return res
}

// gitRemotes returns normalized remote URLs (`git remote -v`, origin hint
// ignored — all remotes stored).
func gitRemotes(dir string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "remote", "-v")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var res []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n := NormalizeRemote(fields[1])
		if n != "" && !seen[n] {
			seen[n] = true
			res = append(res, n)
		}
	}
	return res
}

// NormalizeRemote collapses ssh/https forms to host/owner/repo:
// lowercase host, strip .git, drop credentials, unify separators.
func NormalizeRemote(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// scp-like: git@host:owner/repo(.git)
	if !strings.Contains(s, "://") {
		if at := strings.LastIndex(s, "@"); at >= 0 {
			s = s[at+1:]
		}
		// host:path -> host/path
		if i := strings.Index(s, ":"); i >= 0 {
			s = s[:i] + "/" + s[i+1:]
		}
		s = strings.TrimSuffix(s, ".git")
		s = strings.Trim(s, "/")
		parts := strings.Split(s, "/")
		if len(parts) < 2 {
			return ""
		}
		host := strings.ToLower(strings.TrimSpace(parts[0]))
		if host == "" || strings.Contains(host, "@") || strings.ContainsAny(host, " \t\n/:") {
			return ""
		}
		var rest []string
		for _, seg := range parts[1:] {
			seg = strings.TrimSpace(seg)
			if seg == "" || strings.Contains(seg, "@") || strings.ContainsAny(seg, " \t\n") {
				return ""
			}
			rest = append(rest, seg)
		}
		if host == "" {
			return ""
		}
		return host + "/" + strings.Join(rest, "/")
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	p := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if host == "" || p == "" {
		return ""
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || strings.Contains(seg, "@") || strings.ContainsAny(seg, " \t\n") {
			return ""
		}
	}
	return host + "/" + p
}

// statDevIno extracts dev and ino from a syscall.Stat_t.
func statDevIno(sys interface{}) (uint64, uint64) {
	if st, ok := sys.(*syscall.Stat_t); ok && st != nil {
		return uint64(st.Dev), uint64(st.Ino)
	}
	return 0, 0
}

// findGitDir walks up from root looking for a .git directory or file.
// Pure-Go walk (no git subprocess on the hot path); handles worktree
// links (.git files).
func findGitDir(root string) string {
	dir := root
	for {
		gd := filepath.Join(dir, ".git")
		if _, err := os.Lstat(gd); err == nil {
			return gd
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// gitCommonDir returns the common directory shared by all worktrees of
// one clone. For a plain repo it is the .git dir itself; for a worktree
// link it resolves gitdir:<path> and strips /worktrees/<name>.
func gitCommonDir(gitDir string) string {
	fi, err := os.Stat(gitDir)
	if err != nil {
		return gitDir
	}
	if fi.IsDir() {
		return gitDir
	}
	data, err := os.ReadFile(gitDir)
	if err != nil {
		return gitDir
	}
	target := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "gitdir:") {
			target = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
			break
		}
	}
	if target == "" {
		return gitDir
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(gitDir), target)
	}
	target = filepath.Clean(target)
	// <common>/worktrees/<name> -> <common>
	if base := filepath.Base(filepath.Dir(target)); base == "worktrees" {
		return filepath.Dir(filepath.Dir(target))
	}
	return target
}

func isWorktree(gitDir string) bool {
	fi, err := os.Stat(gitDir)
	if err != nil {
		return false
	}
	return !fi.IsDir()
}
