// Package cli implements the onenotify command line.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// Exit codes of onenotify.
const (
	exitOK      = 0
	exitPending = 1 // something is waiting for delivery or went to the failed store
	exitError   = 2 // onenotify itself could not do its job
)

const rootLong = `onenotify delivers notifications. It takes the events onecheck writes when a
check fails or recovers, turns each into a message, and sends it to every
channel of the config: Telegram, mail, ntfy. "onenotify send" sends a message
of your own the same way. It is meant to be started by cron or a systemd
timer; nothing keeps running in between.

A channel names a module (telegram, mail, ntfy, ...), an executable that
knows how to deliver to one kind of destination. onenotify decides what is
sent and to whom, and keeps every message in a queue per channel until that
channel has delivered it: a channel that is down delays nothing else and gets
its messages, in order, once it works again.

Running "onenotify" without a command is the same as "onenotify run".

Config file, first match wins:
  1. --config <path>
  2. $ONENOTIFY_CONFIG
  3. /etc/onenotify/config.yaml
  4. $HOME/.config/onenotify/config.yaml

Exit codes:
  0  everything in the queues was delivered, or there was nothing to do
  1  at least one message is still waiting or will not be delivered
  2  onenotify could not run (bad config, a run already in progress, ...)

Documentation: https://alekzonder.github.io/onenotify`

const rootExample = `  # take new events and deliver what is queued
  onenotify

  # send a message of your own to every channel
  onenotify send --title "backup finished" --text "42 GB in 13m"

  # check a config without sending anything
  onenotify validate -c ./config.yaml

  # see what is waiting and which channels are failing
  onenotify status

  # install the bundled modules
  onenotify module install alekzonder/onenotify`

// app carries the state shared by all commands of one invocation.
type app struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	configPath string
	logLevel   string

	// code is the exit code set by a command that ran successfully.
	code int
}

// Execute runs onenotify with the given arguments and returns the exit code.
func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{stdin: stdin, stdout: stdout, stderr: stderr}
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(stderr, "onenotify: %v\n", err)
		return exitError
	}
	return a.code
}

func (a *app) rootCmd() *cobra.Command {
	run := &runOptions{}
	root := &cobra.Command{
		Use:           "onenotify",
		Short:         "Deliver notifications about onecheck events and your own messages",
		Long:          rootLong,
		Example:       rootExample,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.run(cmd.Context(), run)
		},
	}
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return fmt.Errorf("%w\nSee '%s --help'", err, cmd.CommandPath())
	})
	root.PersistentFlags().StringVarP(&a.configPath, "config", "c", "", "path to the config file (default: see above)")
	root.PersistentFlags().StringVar(&a.logLevel, "log-level", "", "log level: debug, info, warn or error; overrides the config and also prints the log on stderr")
	addRunFlags(root, run)

	root.AddCommand(a.runCmd(), a.sendCmd(), a.validateCmd(), a.statusCmd(), a.pathsCmd(),
		a.moduleCmd(), a.selfUpdateCmd(), a.logrotateCmd(), a.versionCmd())
	return root
}
