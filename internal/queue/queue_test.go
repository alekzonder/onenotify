package queue

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alekzonder/onenotify/internal/message"
)

var now = time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

func open(t *testing.T) (*Queue, string) {
	t.Helper()
	dir := t.TempDir()
	q, err := Open(filepath.Join(dir, "spool"), filepath.Join(dir, "failed"), filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return q, dir
}

func msg(id string) message.Message {
	return message.Message{Version: 1, ID: id, Source: "send", Host: "h", Level: "info",
		Title: "[h] " + id, Text: "text", Tags: []string{}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
}

func TestEnqueueListLoadRemove(t *testing.T) {
	q, dir := open(t)
	for _, id := range []string{"20261123T100200Z-b", "20261123T100100Z-a", "20261123T100200Z-a"} {
		if err := q.Enqueue("ops", msg(id)); err != nil {
			t.Fatal(err)
		}
	}
	// The same id again replaces the file.
	if err := q.Enqueue("ops", msg("20261123T100100Z-a")); err != nil {
		t.Fatal(err)
	}
	files, err := q.List("ops")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(files, " ") != "20261123T100100Z-a.json 20261123T100200Z-a.json 20261123T100200Z-b.json" {
		t.Errorf("files = %v", files)
	}
	m, err := q.Load("ops", files[0])
	if err != nil || m.ID != "20261123T100100Z-a" || m.Title != "[h] 20261123T100100Z-a" || !m.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Errorf("m=%+v err=%v", m, err)
	}
	if err := q.Remove("ops", files[0]); err != nil {
		t.Fatal(err)
	}
	if files, _ := q.List("ops"); len(files) != 2 {
		t.Errorf("after remove: %v", files)
	}
	if files, err := q.List("never-used"); err != nil || len(files) != 0 {
		t.Errorf("unknown channel: %v %v", files, err)
	}

	info, _ := os.Stat(filepath.Join(dir, "spool", "ops"))
	file, _ := os.Stat(filepath.Join(dir, "spool", "ops", files[1]))
	if info.Mode().Perm() != 0o750 || file.Mode().Perm() != 0o640 {
		t.Errorf("modes: dir %v file %v", info.Mode().Perm(), file.Mode().Perm())
	}
}

func TestListSkipsWhatIsNotAMessageFile(t *testing.T) {
	q, dir := open(t)
	if err := q.Enqueue("ops", msg("20261123T100100Z-a")); err != nil {
		t.Fatal(err)
	}
	if err := q.SetStatus("ops", Status{FailingSince: now, Consecutive: 1}); err != nil {
		t.Fatal(err)
	}
	ops := filepath.Join(dir, "spool", "ops")
	for _, name := range []string{".20261123T100100Z-b.json.123.tmp", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(ops, name), []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(ops, "dir.json"), 0o750); err != nil {
		t.Fatal(err)
	}
	if files, _ := q.List("ops"); len(files) != 1 || files[0] != "20261123T100100Z-a.json" {
		t.Errorf("files = %v", files)
	}
}

func TestChannels(t *testing.T) {
	q, dir := open(t)
	for _, ch := range []string{"b", "a"} {
		if err := q.Enqueue(ch, msg("x")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := q.Lock(); err != nil { // creates .lock and .owner, which are not channels
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "spool", ".hidden"), 0o750); err != nil {
		t.Fatal(err)
	}
	if chans, err := q.Channels(); err != nil || strings.Join(chans, " ") != "a b" {
		t.Errorf("channels = %v %v", chans, err)
	}
}

func TestLoadRejectsWhatIsNotAMessage(t *testing.T) {
	q, dir := open(t)
	ops := filepath.Join(dir, "spool", "ops")
	if err := os.MkdirAll(ops, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"a.json": "{oops", "b.json": "{}", "c.json": "[]", "d.json": ""} {
		if err := os.WriteFile(filepath.Join(ops, name), []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
		if _, err := q.Load("ops", name); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestFail(t *testing.T) {
	q, dir := open(t)
	if err := q.Enqueue("ops", msg("20261123T100100Z-a")); err != nil {
		t.Fatal(err)
	}
	if err := q.Fail("ops", "20261123T100100Z-a.json", "expired", now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if files, _ := q.List("ops"); len(files) != 0 {
		t.Errorf("still queued: %v", files)
	}
	data, err := os.ReadFile(filepath.Join(dir, "failed", "ops", "20261123T100100Z-a.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["reason"] != "expired" || got["failed_at"] != "2026-11-23T12:00:00Z" || got["id"] != "20261123T100100Z-a" || got["title"] != "[h] 20261123T100100Z-a" {
		t.Errorf("failed file = %v", got)
	}
	if n, err := q.FailedCount(); err != nil || n != 1 {
		t.Errorf("failed count = %d %v", n, err)
	}
}

func TestFailOfABrokenFileKeepsItAsItIs(t *testing.T) {
	q, dir := open(t)
	ops := filepath.Join(dir, "spool", "ops")
	if err := os.MkdirAll(ops, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ops, "x.json"), []byte("{oops"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := q.Fail("ops", "x.json", "unreadable", now); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "failed", "ops", "x.json"))
	if string(data) != "{oops" {
		t.Errorf("failed copy = %q", data)
	}
	if _, err := os.Stat(filepath.Join(ops, "x.json")); err == nil {
		t.Error("the broken file is still queued")
	}
}

func TestFailRawStaysInside(t *testing.T) {
	q, dir := open(t)
	for _, name := range []string{"event.json", "../../escape.json", ".hidden.json", "a b\n.json"} {
		if err := q.FailRaw(Unreadable, name, []byte("x")); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "failed", Unreadable))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != "_.hidden.json a_b_.json escape.json event.json" {
		t.Errorf("names = %q", names)
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.json")); err == nil {
		t.Error("a file escaped the failed store")
	}
	if n, _ := q.FailedCount(); n != 4 {
		t.Errorf("failed count = %d", n)
	}
}

func TestFileName(t *testing.T) {
	for id, want := range map[string]string{
		"20261123T100200Z-main-api-health-fail": "20261123T100200Z-main-api-health-fail.json",
		"../../etc/passwd":                      "_.._.._etc_passwd.json",
		".hidden":                               "_.hidden.json",
		"":                                      "_.json",
		"a b/c":                                 "a_b_c.json",
	} {
		if got := FileName(id); got != want {
			t.Errorf("FileName(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestEnqueueWithAHostileIDStaysInTheChannel(t *testing.T) {
	q, dir := open(t)
	if err := q.Enqueue("ops", msg("../../../escape")); err != nil {
		t.Fatal(err)
	}
	if files, _ := q.List("ops"); len(files) != 1 {
		t.Errorf("files = %v", files)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*escape*")); len(matches) != 0 {
		t.Errorf("escaped: %v", matches)
	}
}

func TestLock(t *testing.T) {
	q, _ := open(t)
	unlock, err := q.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Lock(); !errors.Is(err, ErrLocked) {
		t.Errorf("second lock: %v", err)
	}
	start := time.Now()
	if _, err := q.LockWait(300 * time.Millisecond); !errors.Is(err, ErrLocked) || time.Since(start) < 250*time.Millisecond {
		t.Errorf("LockWait on a held lock: %v after %v", err, time.Since(start))
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		unlock()
	}()
	unlock2, err := q.LockWait(5 * time.Second)
	if err != nil {
		t.Fatalf("LockWait after release: %v", err)
	}
	unlock2()
	unlock3, err := q.Lock()
	if err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}
	unlock3()
}

func TestSpoolBelongsToOneConfig(t *testing.T) {
	q, dir := open(t)
	unlock, err := q.Lock()
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	other, err := Open(filepath.Join(dir, "spool"), filepath.Join(dir, "failed"), filepath.Join(dir, "other.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Lock(); err == nil || !strings.Contains(err.Error(), "already used by config") {
		t.Errorf("another config on the same spool: %v", err)
	}
	// The refused config must not keep the lock.
	unlock, err = q.Lock()
	if err != nil {
		t.Fatalf("owner after a refused lock: %v", err)
	}
	unlock()
}

func TestStatus(t *testing.T) {
	q, dir := open(t)
	if q.Status("ops") != nil {
		t.Error("a new channel has no status")
	}
	retry := now.Add(30 * time.Second)
	want := Status{FailingSince: now, Consecutive: 3, LastError: "connection refused", RetryAt: &retry}
	if err := q.SetStatus("ops", want); err != nil {
		t.Fatal(err)
	}
	got := q.Status("ops")
	if got == nil || !got.FailingSince.Equal(now) || got.Consecutive != 3 || got.LastError != "connection refused" || !got.RetryAt.Equal(retry) {
		t.Errorf("status = %+v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "spool", "ops", statusFile), []byte("{oops"), 0o640); err != nil {
		t.Fatal(err)
	}
	if q.Status("ops") != nil {
		t.Error("a corrupt status must read as no status")
	}
	if err := q.ClearStatus("ops"); err != nil {
		t.Fatal(err)
	}
	if err := q.ClearStatus("ops"); err != nil {
		t.Errorf("clearing twice: %v", err)
	}
	if q.Status("ops") != nil {
		t.Error("status after clear")
	}
}

func TestEnqueueFailsWhenTheChannelDirCannotBeMade(t *testing.T) {
	q, dir := open(t)
	if err := os.WriteFile(filepath.Join(dir, "spool", "ops"), []byte("in the way"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue("ops", msg("a")); err == nil {
		t.Error("want an error")
	}
}
