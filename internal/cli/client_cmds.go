// One-shot CLI commands on pkg/client: ps, logs, start, stop, restart,
// config, sessions. Every command resolves the workspace (flag --project,
// AGENT_RUNTIME_PROJECT, else cwd) and talks to the per-user daemon.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	"agent-runtime/internal/platform/paths"
	"agent-runtime/pkg/client"
)

func daemonClient() (*client.Client, error) {
	p := paths.User()
	return client.EnsureDaemon(p.SocketPath())
}

func resolveWorkspace(cl *client.Client, project string) (string, string, error) {
	if project == "" {
		if v := os.Getenv("AGENT_RUNTIME_PROJECT"); v != "" {
			project = v
		} else if cwd, err := os.Getwd(); err == nil {
			project = cwd
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := cl.ProjectService().Resolve(ctx, project)
	if err != nil {
		return "", "", err
	}
	return res.ProjectID, res.WorkspaceID, nil
}

func ctxWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func newDaemonPsCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var project string
	var all bool
	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List managed processes (daemon)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := daemonClient()
			if err != nil {
				return err
			}
			_, ws, err := resolveWorkspace(cl, project)
			if err != nil {
				return err
			}
			ctx, cancel := ctxWithTimeout(10 * time.Second)
			defer cancel()
			items, err := cl.ProcessService().List(ctx, ws, all)
			if err != nil {
				return err
			}
			if len(items) == 0 {
				fmt.Println("no processes")
				return nil
			}
			for _, p := range items {
				fmt.Printf("%s\t%v\t%v\t%v\n", p["process_id"], p["status"], p["command"], p["pid"])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "workspace path or id")
	cmd.Flags().BoolVar(&all, "all", false, "all workspaces")
	return cmd
}

func newDaemonLogsCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var lines int
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <process_id>",
		Short: "Show process logs (daemon)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := daemonClient()
			if err != nil {
				return err
			}
			ctx, cancel := ctxWithTimeout(30 * time.Second)
			defer cancel()
			if !follow {
				res, err := cl.LogService().GetLogs(ctx, args[0], lines)
				if err != nil {
					return err
				}
				if entries, ok := res["entries"].([]any); ok {
					for _, e := range entries {
						if m, ok := e.(map[string]any); ok {
							fmt.Printf("%v\n", m["line"])
						}
					}
				}
				return nil
			}
			ch, err := cl.LogService().Tail(ctx, map[string]any{
				"processIds": []string{args[0]}, "backlog": lines,
			})
			if err != nil {
				return err
			}
			for msg := range ch {
				if line, ok := msg["line"].(string); ok {
					fmt.Println(line)
				} else if lines_, ok := msg["lines"].([]any); ok {
					for _, l := range lines_ {
						fmt.Printf("%v\n", l)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "tail lines")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow live")
	return cmd
}

func newDaemonStartCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var project, app, workdir string
	var env []string
	cmd := &cobra.Command{
		Use:   "start --app <name> | -- <command...>",
		Short: "Start a process (daemon)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := daemonClient()
			if err != nil {
				return err
			}
			_, ws, err := resolveWorkspace(cl, project)
			if err != nil {
				return err
			}
			envMap := map[string]string{}
			for _, kv := range env {
				for i := 0; i < len(kv); i++ {
					if kv[i] == '=' {
						envMap[kv[:i]] = kv[i+1:]
						break
					}
				}
			}
			ctx, cancel := ctxWithTimeout(30 * time.Second)
			defer cancel()
			res, err := cl.ProcessService().Start(ctx, &client.StartRequest{
				WorkspaceID: ws, App: app, Command: args, WorkDir: workdir, Env: envMap,
			})
			if err != nil {
				return err
			}
			fmt.Printf("%s (instance %s, pid %d)\n", res.ProcessID, res.InstanceID, res.PID)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "workspace path or id")
	cmd.Flags().StringVar(&app, "app", "", "configured app name")
	cmd.Flags().StringVar(&workdir, "workdir", "", "working directory")
	cmd.Flags().StringArrayVarP(&env, "env", "e", nil, "KEY=VALUE (repeatable)")
	return cmd
}

func newDaemonStopCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop <process_id>",
		Short: "Stop a process (daemon)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := daemonClient()
			if err != nil {
				return err
			}
			ctx, cancel := ctxWithTimeout(30 * time.Second)
			defer cancel()
			if err := cl.ProcessService().Stop(ctx, args[0]); err != nil {
				return err
			}
			fmt.Println("stopped", args[0])
			return nil
		},
	}
	return cmd
}

func newDaemonRestartCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart <process_id>",
		Short: "Restart a process (daemon)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := daemonClient()
			if err != nil {
				return err
			}
			ctx, cancel := ctxWithTimeout(30 * time.Second)
			defer cancel()
			res, err := cl.ProcessService().Restart(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("restarted %s (instance %v)\n", args[0], res["newInstanceId"])
			return nil
		},
	}
	return cmd
}

func newDaemonConfigCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show resolved config / validate YAML (daemon)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show resolved apps + provenance",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := daemonClient()
			if err != nil {
				return err
			}
			_, ws, err := resolveWorkspace(cl, project)
			if err != nil {
				return err
			}
			ctx, cancel := ctxWithTimeout(10 * time.Second)
			defer cancel()
			res, err := cl.ConfigService().Get(ctx, ws)
			if err != nil {
				return err
			}
			fmt.Printf("config: %v\n", res["configPath"])
			if apps, ok := res["apps"].(map[string]any); ok {
				for name := range apps {
					prov := ""
					if pv, ok := res["provenance"].(map[string]any); ok {
						prov, _ = pv["apps."+name].(string)
					}
					fmt.Printf("  %s (from %s)\n", name, prov)
				}
			}
			return nil
		},
	})
	return cmd
}

func newDaemonSessionsCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List connected sessions (daemon)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := daemonClient()
			if err != nil {
				return err
			}
			_, ws, err := resolveWorkspace(cl, project)
			if err != nil {
				return err
			}
			ctx, cancel := ctxWithTimeout(10 * time.Second)
			defer cancel()
			sessions, err := cl.SessionService().List(ctx, ws)
			if err != nil {
				return err
			}
			for _, se := range sessions {
				fmt.Printf("%v\t%v\t%v\n", se["id"], se["kind"], se["harness"])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "workspace path or id")
	return cmd
}
