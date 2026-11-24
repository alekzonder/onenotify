package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/alekzonder/onenotify/internal/config"
	"github.com/alekzonder/onenotify/internal/queue"
	"github.com/alekzonder/onenotify/internal/rotate"
)

const logrotateLong = `Rotate the log and clean the failed store.

Logs: when <log.dir>/onenotify.log is larger than log.rotate.max_size it
becomes onenotify.log.1 (.gz with compress) and older files move up. Rotated
files beyond log.rotate.keep or older than log.rotate.max_age are removed.

Failed: messages in the failed store (paths.failed) are kept for
queue.failed_ttl after they failed, so that you can look at them, and are
then removed. Temporary files left by an interrupted write are removed too.
Messages waiting in the spool are never touched: a run moves them to the
failed store when their deadline passes.

Without flags both are done. Run it from cron, e.g. once a day.`

const logrotateExample = `  onenotify logrotate
  onenotify logrotate --failed
  onenotify logrotate -c /etc/onenotify/config.yaml --logs

  # crontab
  23 3 * * *  root  onenotify logrotate >/dev/null`

func (a *app) logrotateCmd() *cobra.Command {
	var logs, failed bool
	cmd := &cobra.Command{
		Use:     "logrotate",
		Short:   "Rotate logs and clean the failed store",
		Long:    logrotateLong,
		Example: logrotateExample,
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				return err
			}
			if !logs && !failed {
				logs, failed = true, true
			}
			if logs {
				if err := a.rotateLogs(cfg); err != nil {
					return err
				}
			}
			if failed {
				removed, err := rotate.Failed(cfg.Paths.Failed, cfg.Paths.Spool,
					time.Duration(cfg.Queue.FailedTTL), time.Duration(cfg.Queue.TTL), time.Now())
				if err != nil {
					return fmt.Errorf("cannot clean the failed store: %w", err)
				}
				fmt.Fprintf(a.stdout, "failed: removed %d old from %s\n", removed, cfg.Paths.Failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&logs, "logs", false, "only rotate logs")
	cmd.Flags().BoolVar(&failed, "failed", false, "only clean the failed store")
	return cmd
}

// rotateLogs rotates under the run lock, so that a log is never compressed
// while a run is still writing to it.
func (a *app) rotateLogs(cfg *config.Config) error {
	q, err := queue.Open(cfg.Paths.Spool, cfg.Paths.Failed, cfg.Path)
	if err != nil {
		return err
	}
	unlock, err := q.Lock()
	if errors.Is(err, queue.ErrLocked) {
		fmt.Fprintln(a.stdout, "logs: skipped, a run is in progress")
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	rotated, err := rotate.Logs(cfg.LogFile(), cfg.Log.Rotate, time.Now())
	if err != nil {
		return err
	}
	if rotated {
		fmt.Fprintf(a.stdout, "logs: rotated %s\n", cfg.LogFile())
	} else {
		fmt.Fprintf(a.stdout, "logs: nothing to rotate (%s)\n", cfg.LogFile())
	}
	return nil
}
