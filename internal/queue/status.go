package queue

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/alekzonder/onenotify/internal/fsutil"
)

// Status is what onenotify remembers about a channel that is failing. It
// exists so that a broken channel is reported once, not on every run, and so
// that a rate limit is respected between runs. Losing it is harmless.
type Status struct {
	FailingSince time.Time `json:"failing_since"`
	// Consecutive is the number of runs in a row that could not deliver.
	Consecutive int    `json:"consecutive"`
	LastError   string `json:"last_error"`
	// RetryAt, when set, is the earliest time the channel may be used again.
	RetryAt *time.Time `json:"retry_at,omitempty"`
}

// Status returns the state of a failing channel, or nil for a channel that
// is fine. A status file that cannot be read counts as "fine".
func (q *Queue) Status(channel string) *Status {
	data, err := os.ReadFile(filepath.Join(q.channelDir(channel), statusFile))
	if err != nil {
		return nil
	}
	var s Status
	if json.Unmarshal(data, &s) != nil || s.FailingSince.IsZero() {
		return nil
	}
	return &s
}

// SetStatus records that a channel is failing.
func (q *Queue) SetStatus(channel string, s Status) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := q.channelDir(channel)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, statusFile), append(data, '\n'), filePerm)
}

// ClearStatus records that a channel works again.
func (q *Queue) ClearStatus(channel string) error {
	err := os.Remove(filepath.Join(q.channelDir(channel), statusFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
