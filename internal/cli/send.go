package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/alekzonder/onenotify/internal/config"
	"github.com/alekzonder/onenotify/internal/message"
	"github.com/alekzonder/onenotify/internal/queue"
)

type sendOptions struct {
	title     string
	text      string
	textFile  string
	level     string
	tags      []string
	channels  []string
	queueOnly bool
	output    string
}

// lockWait is how long "send" waits for a run in progress to finish.
var lockWait = 10 * time.Second

const truncatedMark = "\n… (truncated)"

const sendLong = `Send a message of your own through the channels of the config.

The message gets the host name in front of its title, like every
notification: --title "backup failed" arrives as "[web-01] backup failed".
The text is plain text of any number of lines, given with --text, read from a
file with --text-file, or read from standard input with --text-file -.
A text longer than limits.text (64KB by default) is cut.

--level tells the channels how to present the message (an icon, a priority):
info (default), ok, warning or error.

The message is first written into the queue of each channel, so it is not
lost if a channel is down, and then delivered right away: "send" does what
"run" does for the chosen channels, including taking in new events, so that
messages keep their order. If another run is in progress, "send" waits for it
for up to 10 seconds; after that the message simply stays queued and goes out
with the next run.

Without --channel the message goes to every enabled channel.

Exit codes:
  0  delivered to every chosen channel (with --queue-only: queued)
  1  queued, but not delivered to at least one channel yet
  2  nothing was queued: bad arguments, unknown channel, no enabled channels`

const sendExample = `  onenotify send --title "backup finished" --text "42 GB in 13m"
  onenotify send --title "backup failed" --text-file /var/log/backup.log --level error
  pg_dump mydb 2>&1 >/dev/null | onenotify send --title "pg_dump said" --text-file - --level warning
  onenotify send --title "deploy done" --level ok --tag deploy --channel ops-telegram
  onenotify send --title test --channel admin-mail      # try out a channel`

func (a *app) sendCmd() *cobra.Command {
	o := &sendOptions{}
	cmd := &cobra.Command{
		Use:     "send",
		Short:   "Queue a message of your own and deliver it",
		Long:    sendLong,
		Example: sendExample,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.send(cmd.Context(), o, cmd.Flags().Changed("text"))
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.title, "title", "", "title of the message, one line (required)")
	f.StringVar(&o.text, "text", "", "text of the message")
	f.StringVar(&o.textFile, "text-file", "", "read the text from this file; - reads standard input")
	f.StringVar(&o.level, "level", "info", "level: info, ok, warning or error")
	f.StringArrayVar(&o.tags, "tag", nil, "tag of the message (repeatable)")
	f.StringArrayVar(&o.channels, "channel", nil, "send only to this channel (repeatable; default: every enabled channel)")
	f.BoolVar(&o.queueOnly, "queue-only", false, "only queue the message; the next run delivers it")
	f.StringVarP(&o.output, "output", "o", "text", "output format: text or json")
	return cmd
}

func (a *app) send(ctx context.Context, o *sendOptions, textGiven bool) error {
	if err := checkFormat(o.output); err != nil {
		return err
	}
	if strings.TrimSpace(o.title) == "" {
		return errors.New("--title is required and must not be empty")
	}
	if !message.ValidLevel(o.level) {
		return fmt.Errorf("unknown level %q (expected info, ok, warning or error)", o.level)
	}
	if textGiven && o.textFile != "" {
		return errors.New("--text and --text-file cannot be used together")
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	targets, err := sendTargets(cfg, o.channels)
	if err != nil {
		return err
	}
	text, err := a.sendText(o, int64(cfg.Limits.Text))
	if err != nil {
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
	msg := message.NewSend(cfg.Host, o.title, text, o.level, o.tags, time.Now(), time.Duration(cfg.Queue.TTL))
	for _, name := range targets {
		if err := q.Enqueue(name, msg); err != nil {
			return fmt.Errorf("cannot queue the message for channel %s: %w", name, err)
		}
	}
	log.Info("message queued", "id", msg.ID, "level", msg.Level, "title", msg.Title, "channels", strings.Join(targets, ","))
	if o.queueOnly {
		fmt.Fprintf(a.stdout, "queued %s for %s\n", msg.ID, strings.Join(targets, ", "))
		return nil
	}

	unlock, err := q.LockWait(lockWait)
	if errors.Is(err, queue.ErrLocked) {
		fmt.Fprintf(a.stdout, "queued %s for %s\n", msg.ID, strings.Join(targets, ", "))
		fmt.Fprintln(a.stderr, "onenotify: another run is still in progress; the message stays queued and goes out with the next run")
		a.code = exitPending
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	return a.deliver(ctx, cfg, q, log, o.output, targets)
}

// sendTargets is the list of channels a message is queued for.
func sendTargets(cfg *config.Config, names []string) ([]string, error) {
	if len(names) == 0 {
		for _, ch := range cfg.EnabledChannels() {
			names = append(names, ch.Name)
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("no channel is enabled in %s: there is nowhere to send to", cfg.Path)
		}
		return names, nil
	}
	var out []string
	seen := map[string]bool{}
	for _, name := range names {
		ch := cfg.Channel(name)
		switch {
		case ch == nil:
			return nil, fmt.Errorf("there is no channel %q in %s", name, cfg.Path)
		case !ch.Enabled:
			return nil, fmt.Errorf("channel %q is disabled (enabled: false)", name)
		case !seen[name]:
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

// sendText returns the text of the message: --text, or the content of
// --text-file cut to limit bytes.
func (a *app) sendText(o *sendOptions, limit int64) (string, error) {
	if o.textFile == "" {
		return cutText([]byte(o.text), limit), nil
	}
	in := a.stdin
	if o.textFile != "-" {
		f, err := os.Open(o.textFile)
		if err != nil {
			return "", fmt.Errorf("cannot read the text: %w", err)
		}
		defer f.Close()
		in = f
	}
	// One byte over the limit tells a cut text from one that just fits.
	data, err := io.ReadAll(io.LimitReader(in, limit+1))
	if err != nil {
		return "", fmt.Errorf("cannot read the text: %w", err)
	}
	return cutText(data, limit), nil
}

// cutText makes text of what was read: valid UTF-8, without NUL bytes, at
// most limit bytes, with a mark at the end when something was cut off.
func cutText(data []byte, limit int64) string {
	cut := int64(len(data)) > limit
	if cut {
		data = data[:limit]
		// Do not leave half a character at the end.
		for len(data) > 0 && !utf8.Valid(data[len(data)-min(len(data), utf8.UTFMax):]) {
			if r, size := utf8.DecodeLastRune(data); r != utf8.RuneError || size > 1 {
				break
			}
			data = data[:len(data)-1]
		}
	}
	text := string(bytes.ReplaceAll(bytes.ToValidUTF8(data, nil), []byte{0}, nil))
	text = strings.TrimRight(text, "\r\n")
	if cut {
		text += truncatedMark
	}
	return text
}
