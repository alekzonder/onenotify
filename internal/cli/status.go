package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/alekzonder/onenotify/internal/config"
	"github.com/alekzonder/onenotify/internal/deliver"
	"github.com/alekzonder/onenotify/internal/queue"
)

const statusLong = `Show what is waiting for delivery and which channels are failing.

For every channel: how many messages are queued, when the oldest of them was
created, and the state of the channel:

  ok             the last run delivered, or there was nothing to deliver
  failing since  the channel could not deliver; the error of the last attempt
  disabled       enabled: false; queued messages wait until they expire
  not in config  a queue is left from a channel that was removed or renamed

Below the table: the number of messages in the failed store (paths.failed)
and the events waiting in each source.

The command only reads; it can be used while a run is in progress.`

const statusTime = "2006-01-02 15:04 UTC"

// channelStatus is one channel in the output of "onenotify status".
type channelStatus struct {
	Name   string `json:"name"`
	Module string `json:"module,omitempty"`
	Queued int    `json:"queued"`
	// Oldest is the creation time of the oldest queued message.
	Oldest *time.Time `json:"oldest,omitempty"`
	// State is ok, failing, disabled or not_in_config.
	State        string     `json:"state"`
	FailingSince *time.Time `json:"failing_since,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	RetryAt      *time.Time `json:"retry_at,omitempty"`
}

// sourceStatus is one source in the output of "onenotify status".
type sourceStatus struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Waiting int    `json:"waiting"`
}

// statusReport is the JSON output of "onenotify status".
type statusReport struct {
	Channels  []channelStatus `json:"channels"`
	Failed    int             `json:"failed"`
	FailedDir string          `json:"failed_dir"`
	Sources   []sourceStatus  `json:"sources"`
}

func (a *app) statusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "status",
		Short:   "Show the queues and the state of the channels",
		Long:    statusLong,
		Example: "  onenotify status\n  onenotify status --json | jq '.channels[] | select(.queued > 0)'",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				return err
			}
			report, err := gatherStatus(cfg)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(a.stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			a.printStatus(report)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the status as JSON")
	return cmd
}

// gatherStatus reads the queues without creating or changing anything.
func gatherStatus(cfg *config.Config) (*statusReport, error) {
	report := &statusReport{Channels: []channelStatus{}, Sources: []sourceStatus{}, FailedDir: cfg.Paths.Failed}
	names := map[string]bool{}
	for _, ch := range cfg.Channels {
		names[ch.Name] = true
	}
	if _, err := os.Stat(cfg.Paths.Spool); err == nil {
		q, err := queue.Open(cfg.Paths.Spool, cfg.Paths.Failed, cfg.Path)
		if err != nil {
			return nil, err
		}
		onDisk, err := q.Channels()
		if err != nil {
			return nil, err
		}
		for _, name := range onDisk {
			names[name] = true
		}
		if report.Failed, err = q.FailedCount(); err != nil {
			return nil, err
		}
		for name := range names {
			st, err := oneChannel(cfg, q, name)
			if err != nil {
				return nil, err
			}
			report.Channels = append(report.Channels, st)
		}
	} else {
		for name := range names {
			report.Channels = append(report.Channels, configState(cfg, name))
		}
	}
	sort.Slice(report.Channels, func(i, j int) bool { return report.Channels[i].Name < report.Channels[j].Name })

	for _, src := range cfg.Sources {
		s := sourceStatus{Type: src.Type, Path: src.Path}
		if _, err := os.Stat(src.Path); err == nil {
			s.Exists = true
			files, err := deliver.SourceFiles(src.Path)
			if err != nil {
				return nil, err
			}
			s.Waiting = len(files)
		}
		report.Sources = append(report.Sources, s)
	}
	return report, nil
}

// configState is what the config alone says about a channel.
func configState(cfg *config.Config, name string) channelStatus {
	st := channelStatus{Name: name, State: "ok"}
	switch ch := cfg.Channel(name); {
	case ch == nil:
		st.State = "not_in_config"
	case !ch.Enabled:
		st.Module, st.State = ch.Module, "disabled"
	default:
		st.Module = ch.Module
	}
	return st
}

func oneChannel(cfg *config.Config, q *queue.Queue, name string) (channelStatus, error) {
	st := configState(cfg, name)
	files, err := q.List(name)
	if err != nil {
		return st, err
	}
	st.Queued = len(files)
	if len(files) > 0 {
		if m, err := q.Load(name, files[0]); err == nil {
			st.Oldest = &m.CreatedAt
		}
	}
	if failing := q.Status(name); failing != nil && st.State == "ok" {
		st.State = "failing"
		st.FailingSince = &failing.FailingSince
		st.LastError = failing.LastError
		st.RetryAt = failing.RetryAt
	}
	return st, nil
}

func (a *app) printStatus(r *statusReport) {
	if len(r.Channels) == 0 {
		fmt.Fprintln(a.stdout, "no channels configured")
	} else {
		w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "CHANNEL\tMODULE\tQUEUED\tOLDEST\tSTATE")
		for _, c := range r.Channels {
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", c.Name, dash(c.Module), c.Queued, when(c.Oldest), stateText(c))
		}
		_ = w.Flush()
	}
	fmt.Fprintf(a.stdout, "\nfailed: %d %s in %s\n", r.Failed, plural(r.Failed, "message"), r.FailedDir)
	if len(r.Sources) == 0 {
		fmt.Fprintln(a.stdout, "sources: none")
		return
	}
	fmt.Fprintln(a.stdout, "sources:")
	for _, s := range r.Sources {
		note := fmt.Sprintf("%d waiting", s.Waiting)
		if !s.Exists {
			note = "(missing)"
		}
		fmt.Fprintf(a.stdout, "  %s  %s  %s\n", s.Type, s.Path, note)
	}
}

func stateText(c channelStatus) string {
	switch c.State {
	case "failing":
		text := "failing since " + c.FailingSince.UTC().Format(statusTime) + ": " + c.LastError
		if c.RetryAt != nil {
			text += " (next try after " + c.RetryAt.UTC().Format(statusTime) + ")"
		}
		return text
	case "not_in_config":
		return "not in config"
	}
	return c.State
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func when(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.UTC().Format(statusTime)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
