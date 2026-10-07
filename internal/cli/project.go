package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	"agent-runtime/internal/core"
	"agent-runtime/internal/platform/paths"
	"agent-runtime/internal/project"
	"agent-runtime/internal/store"
	"agent-runtime/pkg/client"
)

// newProjectCmd adds the v3 `project` tree: resolve/ls/gc. Resolve works
// offline (direct store + resolver on the XDG paths) so Phase 1's exit
// criterion holds without a running daemon; ls prefers the daemon API.
func newProjectCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage v3 projects and workspaces (central registry)",
		Long:  "Resolve working directories to stable project identities (locator chain: path → dev/ino → git → fingerprints). State lives in ~/.local/share/agent-runtime/state.db, never in the repo.",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "resolve [dir]",
			Short: "Resolve a directory to project + workspace IDs",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				dir := "."
				if len(args) > 0 {
					dir = args[0]
				}
				if abs, err := toAbs(dir); err == nil {
					dir = abs
				}
				root := project.WorkspaceRoot(dir)
				// Prefer the daemon (single writer) when reachable.
				p := paths.User()
				if cl, err := client.EnsureDaemon(p.SocketPath()); err == nil {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					res, err := cl.ProjectService().Resolve(ctx, root)
					if err == nil {
						fmt.Printf("project %s\nworkspace %s\nroot %s\n", res.ProjectID, res.WorkspaceID, root)
						return nil
					}
					logger.Warn("daemon resolve failed, falling back to local store", "error", err.Error())
				}
				// Offline fallback: open the store directly (read-only path;
				// the daemon remains the only writer when running).
				db, err := store.Open(p.StateDBPath())
				if err != nil {
					return err
				}
				defer db.Close()
				engine := core.New(db)
				defer engine.Close()
				pid, wid, err := engine.ResolveWorkspace(root)
				if err != nil {
					return err
				}
				fmt.Printf("project %s\nworkspace %s\nroot %s\n", string(pid), string(wid), root)
				return nil
			},
		},
		&cobra.Command{
			Use:   "ls",
			Short: "List registered projects",
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				if cl, err := client.EnsureDaemon(p.SocketPath()); err == nil {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					ps, err := cl.ProjectService().ListProjects(ctx)
					if err == nil {
						for _, pr := range ps {
							fmt.Printf("%s\t%s\tworkspaces=%d\tlast_used=%d\n",
								pr.ID, pr.Name, pr.WorkspaceCount, pr.LastUsedAt)
						}
						return nil
					}
				}
				db, err := store.Open(p.StateDBPath())
				if err != nil {
					return err
				}
				defer db.Close()
				ps, err := db.ListProjects()
				if err != nil {
					return err
				}
				for _, pr := range ps {
					wss, _ := db.ListWorkspaces(pr.ID)
					fmt.Printf("%s\t%s\tworkspaces=%d\tlast_used=%d\n",
						pr.ID, pr.Name, len(wss), pr.LastUsedAt)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "gc",
			Short: "List workspaces whose paths no longer exist",
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				db, err := store.Open(p.StateDBPath())
				if err != nil {
					return err
				}
				defer db.Close()
				missing, err := db.GC()
				if err != nil {
					return err
				}
				// Refresh missing flags by scanning known workspaces.
				for _, pr := range mustList(db) {
					for _, ws := range mustWorkspaces(db, pr.ID) {
						if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
							_ = db.MarkWorkspaceMissing(ws.ID)
							fmt.Printf("missing\t%s\t%s\n", ws.ID, ws.Path)
						}
					}
				}
				for _, id := range missing {
					fmt.Printf("stale\t%s\n", id)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "show [project-id]",
			Short: "Show project detail (workspaces, config state)",
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				cl, err := client.EnsureDaemon(p.SocketPath())
				if err != nil {
					return err
				}
				pid := ""
				if len(args) > 0 {
					pid = args[0]
				} else {
					cwd, _ := os.Getwd()
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					res, err := cl.ProjectService().Resolve(ctx, cwd)
					if err != nil {
						return err
					}
					pid = res.ProjectID
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				proj, err := cl.ProjectService().GetProject(ctx, pid)
				if err != nil {
					return err
				}
				fmt.Printf("project %v\n", proj["project"])
				return nil
			},
		},
		&cobra.Command{
			Use:   "rename <project-id> <name>",
			Short: "Rename a project",
			Args:  cobra.ExactArgs(2),
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				db, err := store.Open(p.StateDBPath())
				if err != nil {
					return err
				}
				defer db.Close()
				pr, err := db.GetProject(args[0])
				if err != nil {
					return err
				}
				pr.Name = args[1]
				return db.UpdateProject(pr)
			},
		},
		&cobra.Command{
			Use:   "link <dir> <project-id>",
			Short: "Attach a workspace dir to another project (e.g. a fork)",
			Args:  cobra.ExactArgs(2),
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				db, err := store.Open(p.StateDBPath())
				if err != nil {
					return err
				}
				defer db.Close()
				abs, err := toAbs(args[0])
				if err != nil {
					return err
				}
				ws, err := db.LinkWorkspace(args[1], abs)
				if err != nil {
					return err
				}
				fmt.Printf("linked %s as %s\n", abs, ws.ID)
				return nil
			},
		},
		&cobra.Command{
			Use:   "unlink <workspace-id>",
			Short: "Detach a workspace (flags it missing; use gc to review)",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				db, err := store.Open(p.StateDBPath())
				if err != nil {
					return err
				}
				defer db.Close()
				return db.MarkWorkspaceMissing(args[0])
			},
		},
		&cobra.Command{
			Use:   "forget <project-id>",
			Short: "Forget a project (soft delete, 30-day trash)",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				cl, err := client.EnsureDaemon(p.SocketPath())
				if err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				return cl.ProjectService().Forget(ctx, args[0])
			},
		},
	)
	return cmd
}

func mustList(db *store.DB) []*store.Project {
	ps, _ := db.ListProjects()
	return ps
}

func mustWorkspaces(db *store.DB, pid string) []*store.Workspace {
	wss, _ := db.ListWorkspaces(pid)
	return wss
}

func toAbs(dir string) (string, error) {
	if dir == "" {
		return os.Getwd()
	}
	if len(dir) > 0 && dir[0] == '/' {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return dir, err
	}
	return cwd + "/" + dir, nil
}
