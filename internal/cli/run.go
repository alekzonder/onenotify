package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/alekzonder/onenotify/internal/config"
	"github.com/alekzonder/onenotify/internal/deliver"
	"github.com/alekzonder/onenotify/internal/output"
	"github.com/alekzonder/onenotify/internal/queue"
	"github.com/alekzonder/onenotify/internal/version"
)

type runOptions struct {
	output   string
	channels []string
}

const runLong = `Take new events from the sources and deliver what is queued.

A run does two things, in this order:

  1. Intake. Every event waiting in a source (the onecheck event queue) is
     turned into a message and written into the queue of every enabled
     channel; then the event is removed from the source.
  2. Delivery. Channels are served in parallel; within a channel messages go
     out one by one, oldest first, through the channel's module.

What can happen to a message:

  SENT    the provider accepted it; it left the queue
  WAIT    it could not be delivered now (network, wrong token, rate limit);
          it stays in the queue, and so does everything behind it in that
          channel, until the next run
  FAILED  it will not be delivered: its deadline passed, or the provider
          rejected its content; it was moved to paths.failed

--channel limits delivery to the named channels. Intake is not limited: new
events are still queued for every enabled channel.

Only one run works on a spool at a time; a second one exits with 2.`

const runExample = `  onenotify run
  onenotify run -c ./config.yaml -o json
  onenotify run --channel ops-telegram --log-level debug

  # crontab: every minute, mail only what cron sees on stderr
  * * * * *  root  onenotify >/dev/null`

func addRunFlags(cmd *cobra.Command, o *runOptions) {
	cmd.Flags().StringVarP(&o.output, "output", "o", "text", "output format: text or json")
	cmd.Flags().StringArrayVar(&o.channels, "channel", nil, "deliver only to this channel (repeatable)")
}

func (a *app) runCmd() *cobra.Command {
	o := &runOptions{}
	cmd := &cobra.Command{
		Use:     "run",
		Short:   "Take new events and deliver the queues (default command)",
		Long:    runLong,
		Example: runExample,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.run(cmd.Context(), o)
		},
	}
	addRunFlags(cmd, o)
	return cmd
}

func checkFormat(format string) error {
	if format != "text" && format != "json" {
		return fmt.Errorf("unknown output format %q (expected text or json)", format)
	}
	return nil
}

// checkChannels rejects names that are not channels of the config.
func checkChannels(cfg *config.Config, names []string) error {
	for _, name := range names {
		if cfg.Channel(name) == nil {
			return fmt.Errorf("there is no channel %q in %s", name, cfg.Path)
		}
	}
	return nil
}

func (a *app) run(ctx context.Context, o *runOptions) error {
	if err := checkFormat(o.output); err != nil {
		return err
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	if err := checkChannels(cfg, o.channels); err != nil {
		return err
	}
	log, closer, err := a.logger(cfg)
	if err != nil {
		return err
	}
	defer closer.Close()

	q, err := queue.Open(cfg.Paths.Spool, cfg.Paths.Failed, cfg.Path)
	if err != nil {
		return err
	}
	unlock, err := q.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	return a.deliver(ctx, cfg, q, log, o.output, o.channels)
}

// deliver runs one pass under the lock the caller holds, prints the report
// and sets the exit code.
func (a *app) deliver(ctx context.Context, cfg *config.Config, q *queue.Queue, log *slog.Logger, format string, channels []string) error {
	log.Debug("run started", "config", cfg.Path, "host", cfg.Host, "version", version.Version,
		"log_file", cfg.LogFile(), "spool", cfg.Paths.Spool)
	report, err := deliver.Run(ctx, cfg, q, deliver.Options{Channels: channels, Log: log, Now: time.Now})
	if report != nil && (err == nil || report.Total() > 0) {
		if werr := output.Write(a.stdout, format, report); werr != nil {
			err = errors.Join(err, werr)
		}
	}
	if err != nil {
		log.Error("run failed", "error", err)
		return err
	}
	a.code = output.ExitCode(report)
	return nil
}
