// Package project provides the project registry that maps
// working directories to stable project identities.
package project

// Registry resolves paths to projects and workspaces.
type Registry struct {
	store Store
}

func NewRegistry(store Store) *Registry {
	return &Registry{store: store}
}

// Resolve maps a working directory path to a project ID and workspace ID.
func (r *Registry) Resolve(path string) (ID, WorkspaceID, error) {
	resolver := NewResolver(r.store)
	resolution, err := resolver.Resolve(path)
	if err != nil {
		return "", "", err
	}
	return resolution.ProjectID, resolution.WorkspaceID, nil
}

// ResolveFull maps a path to the full resolution including NewlyCreated.
func (r *Registry) ResolveFull(path string) (*Resolution, error) {
	resolver := NewResolver(r.store)
	return resolver.Resolve(path)
}

// GetProject returns a project by ID.
func (r *Registry) GetProject(id ID) (*Project, error) {
	return r.store.GetProject(id)
}

// ListProjects returns all projects.
func (r *Registry) ListProjects() ([]*Project, error) {
	return r.store.ListProjects()
}

// CreateWorkspace creates a new workspace under a project.
func (r *Registry) CreateWorkspace(projectID ID, path string) (*Workspace, error) {
	return r.store.CreateWorkspace(projectID, path)
}

// GetWorkspace returns a workspace by ID.
func (r *Registry) GetWorkspace(id WorkspaceID) (*Workspace, error) {
	return r.store.GetWorkspaceAtID(id)
}

// EnsureProject ensures a project exists and returns its ID.
func (r *Registry) EnsureProject(name string) (ID, error) {
	p, err := r.store.GetProjectByName(name)
	if err == nil && p != nil {
		return p.ID, nil
	}
	p, err = r.store.CreateProject(name)
	if err != nil {
		return "", err
	}
	return p.ID, nil
}
