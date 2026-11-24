package rotate

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Failed removes what would otherwise grow forever in onenotify's own
// directories: files in the failed store older than failedTTL, and temporary
// files of interrupted writes in the spool and the failed store older than
// ttl. It returns the number of failed messages removed.
//
// The age of a failed message is counted from its "failed_at"; a file without
// one (an unreadable event kept as it was) is judged by its modification time.
func Failed(failedDir, spoolDir string, failedTTL, ttl time.Duration, now time.Time) (int, error) {
	removed := 0
	err := eachFile(failedDir, func(path string, info fs.FileInfo) error {
		if isTemp(info.Name()) {
			return removeOlder(path, info.ModTime(), ttl, now, nil)
		}
		return removeOlder(path, failedAt(path, info), failedTTL, now, &removed)
	})
	if err != nil {
		return removed, err
	}
	err = eachFile(spoolDir, func(path string, info fs.FileInfo) error {
		if !isTemp(info.Name()) {
			return nil
		}
		return removeOlder(path, info.ModTime(), ttl, now, nil)
	})
	return removed, err
}

func isTemp(name string) bool {
	return strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".tmp")
}

func removeOlder(path string, at time.Time, ttl time.Duration, now time.Time, count *int) error {
	if now.Sub(at) < ttl {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if count != nil {
		*count++
	}
	return nil
}

func failedAt(path string, info fs.FileInfo) time.Time {
	data, err := os.ReadFile(path)
	if err != nil {
		return info.ModTime()
	}
	var f struct {
		FailedAt time.Time `json:"failed_at"`
	}
	if json.Unmarshal(data, &f) != nil || f.FailedAt.IsZero() {
		return info.ModTime()
	}
	return f.FailedAt
}

// eachFile calls fn for every regular file one level below dir: dir/<sub>/<file>.
func eachFile(dir string, fn func(path string, info fs.FileInfo) error) error {
	subs, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, sub := range subs {
		if !sub.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, sub.Name()))
		if err != nil {
			return err
		}
		for _, f := range files {
			info, err := f.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if err := fn(filepath.Join(dir, sub.Name(), f.Name()), info); err != nil {
				return err
			}
		}
	}
	return nil
}
