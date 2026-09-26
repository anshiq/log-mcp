// migrate, project trust and project export commands (§14).
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
	"agent-runtime/internal/migrate"
	"agent-runtime/internal/platform/paths"
	"agent-runtime/internal/store"
	"agent-runtime/pkg/client"
)

func newMigrateCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var dryRun, allKnown, cleanup, yes bool
	cmd := &cobra.Command{
		Use:   "migrate [--dry-run] [--all-known] [--cleanup] [--yes] [<dir>...]",
		Short: "Migrate v2 projects to the v3 central store",
		Long: `Resolve each dir to a v3 project, import its agent-runtime.yaml
(trust-gated), import .agent-runtime/logs.db + audit.log, and hand over
any running v2 daemon. With --all-known, discover projects from harness
configs too. With --cleanup, remove legacy .agent-runtime dirs after
confirmation.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := paths.User()
			db, err := store.Open(p.StateDBPath())
			if err != nil {
				return err
			}
			defer db.Close()
			eng := core.NewWithOptions(db, core.Options{DataDir: p.Data, Logger: logger})
			defer eng.Close()
			dirs := args
			if allKnown {
				dirs = append(dirs, migrate.FindKnownDirs()...)
			}
			if len(dirs) == 0 {
				cwd, _ := os.Getwd()
				dirs = []string{cwd}
			}
			opts := migrate.Options{DryRun: dryRun, AllKnown: allKnown, Cleanup: cleanup, Yes: yes}
			for _, dir := range dirs {
				rep, err := migrate.Migrate(eng, p.Data, dir, opts)
				if err != nil {
					return err
				}
				fmt.Printf("%s: configs=%d logs=%d audit=%d daemons=%d skipped=%v\n",
					dir, rep.ConfigsImported, rep.LogsImported, rep.AuditImported,
					rep.DaemonsStopped, rep.Skipped)
				if cleanup && !dryRun {
					legacy := dir + "/.agent-runtime"
					fmt.Printf("remove %s? [y/N] ", legacy)
					var ans string
					fmt.Scanln(&ans)
					if ans == "y" || ans == "yes" {
						_ = os.RemoveAll(legacy)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report without writing")
	cmd.Flags().BoolVar(&allKnown, "all-known", false, "discover projects from harness configs")
	cmd.Flags().BoolVar(&cleanup, "cleanup", false, "offer to remove legacy .agent-runtime dirs")
	cmd.Flags().BoolVar(&yes, "yes", false, "trust repo configs non-interactively")
	return cmd
}

func newProjectTrustCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trust",
		Short: "Trust the repo-layer config for this workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := paths.User()
			cl, err := client.EnsureDaemon(p.SocketPath())
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			res, err := cl.ProjectService().Resolve(ctx, cwd)
			if err != nil {
				return err
			}
			cfg, err := cl.ConfigService().Get(ctx, res.WorkspaceID)
			if err != nil {
				return err
			}
			repo, _ := cfg["repoPath"].(string)
			if repo == "" {
				fmt.Println("no repo config for this workspace")
				return nil
			}
			data, err := os.ReadFile(repo)
			if err != nil {
				return err
			}
			sha := config.SHA256(data)
			if err := cl.ProjectService().TrustRepo(ctx, res.WorkspaceID, repo, sha); err != nil {
				return err
			}
			fmt.Printf("trusted %s\n", repo)
			return nil
		},
	}
}
