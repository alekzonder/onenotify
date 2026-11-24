package rotate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFailed(t *testing.T) {
	dir := t.TempDir()
	failed, spool := filepath.Join(dir, "failed"), filepath.Join(dir, "spool")
	for _, d := range []string{filepath.Join(failed, "ops"), filepath.Join(failed, "_unreadable"), filepath.Join(spool, "ops")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	day := 24 * time.Hour
	old, fresh := now.Add(-8*day), now.Add(-6*day)
	failedAt := func(at time.Time) string { return `{"id":"x","failed_at":"` + at.Format(time.RFC3339) + `"}` }

	// failed_at decides, not the modification time.
	write(t, filepath.Join(failed, "ops", "old.json"), failedAt(old), now)
	write(t, filepath.Join(failed, "ops", "fresh.json"), failedAt(fresh), old)
	// Without failed_at the modification time decides.
	write(t, filepath.Join(failed, "_unreadable", "old.json"), "{oops", old)
	write(t, filepath.Join(failed, "_unreadable", "fresh.json"), "{oops", fresh)
	// Temporary files are judged by queue.ttl.
	write(t, filepath.Join(failed, "ops", ".a.json.1.tmp"), "x", now.Add(-25*time.Hour))
	write(t, filepath.Join(spool, "ops", ".b.json.1.tmp"), "x", now.Add(-25*time.Hour))
	write(t, filepath.Join(spool, "ops", ".c.json.1.tmp"), "x", now.Add(-time.Hour))
	// Waiting messages and channel state are never touched.
	write(t, filepath.Join(spool, "ops", "waiting.json"), "{}", old)
	write(t, filepath.Join(spool, "ops", ".status.json"), "{}", old)
	write(t, filepath.Join(spool, ".lock"), "", old)

	removed, err := Failed(failed, spool, 7*day, day, now)
	if err != nil || removed != 2 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if got := names(t, filepath.Join(failed, "ops")); got != "fresh.json" {
		t.Errorf("failed/ops: %s", got)
	}
	if got := names(t, filepath.Join(failed, "_unreadable")); got != "fresh.json" {
		t.Errorf("failed/_unreadable: %s", got)
	}
	if got := names(t, filepath.Join(spool, "ops")); got != ".c.json.1.tmp .status.json waiting.json" {
		t.Errorf("spool/ops: %s", got)
	}
}

func TestFailedMissingDirs(t *testing.T) {
	dir := t.TempDir()
	if removed, err := Failed(filepath.Join(dir, "a"), filepath.Join(dir, "b"), time.Hour, time.Hour, now); err != nil || removed != 0 {
		t.Errorf("removed=%d err=%v", removed, err)
	}
}
