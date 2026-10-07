// migrate command.
package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	"agent-runtime/internal/core"
	"agent-runtime/internal/migrate"
	"agent-runtime/internal/platform/paths"
	"agent-runtime/internal/store"
)

func newMigrateCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var dryRun, allKnown, cleanup bool
	cmd := &cobra.Command{
		Use:   "migrate [--dry-run] [--all-known] [--cleanup] [<dir>...]",
		Short: "Migrate v2 projects to the v3 central store",
		Long: `Resolve each dir to a v3 project, import .agent-runtime/logs.db + audit.log, and hand over
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
			opts := migrate.Options{DryRun: dryRun, AllKnown: allKnown, Cleanup: cleanup}
			for _, dir := range dirs {
				rep, err := migrate.Migrate(eng, p.Data, dir, opts)
				if err != nil {
					return err
				}
				fmt.Printf("%s: logs=%d audit=%d daemons=%d skipped=%v\n",
					dir, rep.LogsImported, rep.AuditImported,
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
	return cmd
}
