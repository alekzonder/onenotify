package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/alekzonder/onenotify/internal/deliver"
)

const validateLong = `Check the config without sending anything.

validate reports every problem at once: YAML syntax, unknown keys, duplicate
channel names, environment variables that are not set, modules that are not
installed or not compatible, and params a module does not accept.

It warns, without failing, about a source directory that does not exist yet
(onecheck creates its event queue on its first run) and about a config with
no enabled channel.

Whether a token or an address really works is only known by sending:
"onenotify send --title test --channel <name>".

It exits with 0 when the config is usable as is and with 2 otherwise.`

func (a *app) validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "validate",
		Short:   "Check the config without sending anything",
		Long:    validateLong,
		Example: "  onenotify validate\n  onenotify validate -c ./config.yaml",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				return err
			}
			bad := 0
			for i := range cfg.Channels {
				ch := &cfg.Channels[i]
				if err := deliver.CheckChannel(cmd.Context(), cfg, ch); err != nil {
					bad++
					fmt.Fprintf(a.stderr, "%s: channel %s: %v\n", ch.Source, ch.Name, err)
				}
			}
			for _, src := range cfg.Sources {
				if _, err := os.Stat(src.Path); err != nil {
					fmt.Fprintf(a.stderr, "onenotify: warning: source %s does not exist yet\n", src.Path)
				}
			}
			if len(cfg.EnabledChannels()) == 0 {
				fmt.Fprintln(a.stderr, "onenotify: warning: no channel is enabled; events will stay in their source")
			}
			if bad > 0 {
				return fmt.Errorf("%d channel(s) of %s are not usable", bad, cfg.Path)
			}
			fmt.Fprintf(a.stdout, "config OK: %d channels, %d sources (%s)\n", len(cfg.Channels), len(cfg.Sources), cfg.Path)
			return nil
		},
	}
}
