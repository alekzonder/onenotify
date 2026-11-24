package deliver

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alekzonder/onenotify/internal/config"
	"github.com/alekzonder/onenotify/internal/fsutil"
	"github.com/alekzonder/onenotify/internal/message"
	"github.com/alekzonder/onenotify/internal/queue"
)

// SourceFiles lists the event files waiting in a source directory, oldest
// first. Names starting with a dot are files still being written. A missing
// directory has none.
func SourceFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// intake moves every waiting event into the queue of every enabled channel.
//
// For each event the order is: write the message into all queues (durably),
// then delete the event. An error stops the intake and leaves the event in
// its source, so nothing is lost; the caller must then not deliver, or a
// channel that did get the message would get it again on every run.
func intake(cfg *config.Config, q *queue.Queue, now time.Time, log *slog.Logger) (int, error) {
	channels := cfg.EnabledChannels()
	taken := 0
	for _, src := range cfg.Sources {
		files, err := SourceFiles(src.Path)
		if err != nil {
			return taken, fmt.Errorf("cannot read source %s: %w", src.Path, err)
		}
		if len(files) == 0 {
			continue
		}
		if len(channels) == 0 {
			log.Warn("events are waiting but no channel is enabled; they stay in the source",
				"source", src.Path, "events", len(files))
			continue
		}
		for _, name := range files {
			if err := takeOne(cfg, q, channels, filepath.Join(src.Path, name), now, log); err != nil {
				return taken, err
			}
			taken++
		}
		// Make the removals durable: an event that comes back after a power
		// loss would be delivered a second time.
		if err := fsutil.SyncDir(src.Path); err != nil {
			log.Warn("cannot sync the source directory", "source", src.Path, "error", err)
		}
	}
	return taken, nil
}

func takeOne(cfg *config.Config, q *queue.Queue, channels []*config.Channel, path string, now time.Time, log *slog.Logger) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // taken away by someone else between listing and reading
	}
	if err != nil {
		return fmt.Errorf("cannot read event: %w", err)
	}
	msg, err := message.FromOnecheck(data, cfg.Host, time.Duration(cfg.Queue.TTL), now)
	if err != nil {
		if ferr := q.FailRaw(queue.Unreadable, filepath.Base(path), data); ferr != nil {
			return fmt.Errorf("cannot move an unreadable event to the failed store: %w", ferr)
		}
		log.Warn("a file in the source is not an event; moved to the failed store",
			"file", path, "error", err)
		return removeSource(path)
	}
	for _, ch := range channels {
		if err := q.Enqueue(ch.Name, msg); err != nil {
			return fmt.Errorf("cannot queue event %s for channel %s: %w", msg.ID, ch.Name, err)
		}
	}
	log.Info("event taken", "id", msg.ID, "level", msg.Level, "title", msg.Title, "channels", len(channels))
	return removeSource(path)
}

func removeSource(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot remove a taken event (it would be delivered again): %w", err)
	}
	return nil
}
