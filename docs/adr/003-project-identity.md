# ADR-003: Project Identity via Locator Chain

## Status
Accepted

## Context
Config lives as YAML text in state.db project_configs (see ADR-004) and state in state.db. Mapping a working directory to a project that handles moves, re-clones, and worktrees requires a stable way to map a working directory to a project that handles moves, re-clones, and worktrees.

## Decision
Use a **two-level model** plus a **locator chain**:

```
Project (logical app; owns config + history)  proj_<ulid>
   └── Workspace (a concrete checkout on disk)  ws_<ulid>
          locators: path, (dev,ino), git_common_dir, git_worktree_dir
```

Resolution algorithm:
1. Exact match by canonical path
2. Match by `(dev, inode)` for moves
3. Git-based matching via `git_common_dir` + fingerprints
4. Create new project/workspace

## Consequences
- Same repo from subdirectory → same project
- Repo moved/renamed → same project
- Repo re-cloned → same project (with confirmation)
- Two independent clones → two workspaces of one project
- `git worktree add` → separate workspace
- Non-git dir → works, keyed by path + inode
- Resolution budget: < 5 ms warm, < 50 ms cold

## Alternatives Considered
- Absolute path: Breaks on move/rename
- Latest commit hash: Changes on every commit
- Remote URL alone: Collides for clones
- Marker file with UUID: Writes into repo (rejected)
- Inode alone: Changes on re-clone

## References
- Plan.md §4
- Plan.md §12 (data model)
