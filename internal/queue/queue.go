// Package queue keeps messages on disk between "taken from a source" and
// "delivered": one directory per channel under the spool, and the failed
// store for what could not be delivered.
//
//	<spool>/.lock                  run lock
//	<spool>/.owner                 the config file this spool belongs to
//	<spool>/<channel>/<id>.json    a message waiting for delivery
//	<spool>/<channel>/.status.json state of a failing channel
//	<failed>/<channel>/<id>.json   a message that expired or was rejected
//	<failed>/_unreadable/<file>    files that could not be read as events
package queue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/alekzonder/onenotify/internal/fsutil"
	"github.com/alekzonder/onenotify/internal/message"
)

// ErrLocked means another run on the same spool is still in progress.
var ErrLocked = errors.New("another onenotify run is still in progress")

// Unreadable is the directory under failed for files from sources that are
// not events. Channel names cannot start with "_".
const Unreadable = "_unreadable"

const (
	lockFile   = ".lock"
	ownerFile  = ".owner"
	statusFile = ".status.json"
)

// Messages can quote what was checked, so they are readable by owner and
// group only.
const (
	dirPerm  = 0o750
	filePerm = 0o640
)

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// maxName keeps a file name, with the suffix of a temporary file added,
// under the 255 bytes most filesystems allow.
const maxName = 180

// FileName maps a message id to a file name that cannot leave its directory.
// An id too long for a file name is cut and gets a hash of the whole id, so
// that different ids still give different names.
func FileName(id string) string {
	return clip(unsafeChars.ReplaceAllString(id, "_")) + ".json"
}

func clip(name string) string {
	if name == "" || name[0] == '.' {
		name = "_" + name
	}
	if len(name) > maxName {
		sum := sha256.Sum256([]byte(name))
		name = name[:maxName] + "-" + hex.EncodeToString(sum[:6])
	}
	return name
}

// mkdir creates a directory of the spool or the failed store. A directory
// that is new is made durable in its parent, so that a message written into
// it survives a power loss.
func mkdir(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return err
	}
	return fsutil.SyncDir(filepath.Dir(dir))
}

// Queue gives access to the spool and the failed store of one config.
type Queue struct {
	spool      string
	failed     string
	configPath string
}

// Open creates the spool and failed directories and returns the queue.
func Open(spool, failed, configPath string) (*Queue, error) {
	for _, dir := range []string{spool, failed} {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return nil, fmt.Errorf("cannot create directory: %w", err)
		}
	}
	return &Queue{spool: spool, failed: failed, configPath: configPath}, nil
}

// Lock takes the run lock without waiting and checks that the spool belongs
// to this config file. It returns ErrLocked when a run is in progress.
func (q *Queue) Lock() (unlock func(), err error) {
	f, err := os.OpenFile(filepath.Join(q.spool, lockFile), os.O_CREATE|os.O_RDWR, filePerm)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	unlock = func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
	if err := q.claim(); err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}

// LockWait is Lock that keeps trying for up to d while a run is in progress.
func (q *Queue) LockWait(d time.Duration) (unlock func(), err error) {
	deadline := time.Now().Add(d)
	for {
		unlock, err = q.Lock()
		if !errors.Is(err, ErrLocked) || !time.Now().Before(deadline) {
			return unlock, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// claim records which config file owns the spool and refuses a different
// file using the same one.
func (q *Queue) claim() error {
	path := filepath.Join(q.spool, ownerFile)
	data, err := os.ReadFile(path)
	if err == nil {
		if owner := strings.TrimSpace(string(data)); owner != "" && owner != q.configPath {
			return fmt.Errorf("spool %s is already used by config %s; %s must use different paths.spool and paths.failed",
				q.spool, owner, q.configPath)
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return fsutil.WriteFileAtomic(path, []byte(q.configPath+"\n"), filePerm)
}

func (q *Queue) channelDir(channel string) string {
	return filepath.Join(q.spool, channel)
}

// Forget removes the empty queue directory of a channel together with its
// status. A directory that still holds anything else is left alone.
func (q *Queue) Forget(channel string) {
	if files, err := q.List(channel); err != nil || len(files) > 0 {
		return
	}
	_ = q.ClearStatus(channel)
	_ = os.Remove(q.channelDir(channel))
}

// CheckOwner refuses a spool that another config file already uses, without
// claiming a spool nobody uses yet. Lock does both; this is for writing into
// the queue without the lock.
func (q *Queue) CheckOwner() error {
	data, err := os.ReadFile(filepath.Join(q.spool, ownerFile))
	if err != nil {
		return nil
	}
	if owner := strings.TrimSpace(string(data)); owner != "" && owner != q.configPath {
		return fmt.Errorf("spool %s is already used by config %s; %s must use different paths.spool and paths.failed",
			q.spool, owner, q.configPath)
	}
	return nil
}

// Enqueue writes a message into a channel's queue. The write is durable, so
// the caller may delete the source of the message right after it. Writing
// the same id again replaces the file: enqueueing is idempotent.
func (q *Queue) Enqueue(channel string, m message.Message) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	dir := q.channelDir(channel)
	if err := mkdir(dir); err != nil {
		return err
	}
	return fsutil.WriteFileDurable(filepath.Join(dir, FileName(m.ID)), append(data, '\n'), filePerm)
}

// Channels lists the channels that have a queue directory, sorted. It
// includes channels that are no longer in the config.
func (q *Queue) Channels() ([]string, error) {
	entries, err := os.ReadDir(q.spool)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// List returns the file names of a channel's waiting messages, oldest first.
// A channel without a queue directory has none.
func (q *Queue) List(channel string) ([]string, error) {
	return listJSON(q.channelDir(channel))
}

func listJSON(dir string) ([]string, error) {
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

// Load reads one waiting message. An error means the file is not a message.
func (q *Queue) Load(channel, file string) (message.Message, error) {
	var m message.Message
	data, err := os.ReadFile(filepath.Join(q.channelDir(channel), file))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("not a message: %w", err)
	}
	if m.ID == "" || m.Title == "" {
		return m, errors.New("not a message: no id or title")
	}
	return m, nil
}

// Remove deletes a delivered message.
func (q *Queue) Remove(channel, file string) error {
	return os.Remove(filepath.Join(q.channelDir(channel), file))
}

// failedMessage is a message in the failed store.
type failedMessage struct {
	message.Message
	FailedAt time.Time `json:"failed_at"`
	Reason   string    `json:"reason"`
}

// Fail moves a waiting message to the failed store, recording when and why.
// A file that is not a message is moved as it is. The copy in failed is
// written before the original is removed, so a message is never in neither.
func (q *Queue) Fail(channel, file, reason string, now time.Time) error {
	src := filepath.Join(q.channelDir(channel), file)
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	var m message.Message
	if json.Unmarshal(data, &m) == nil && m.ID != "" {
		out, err := json.MarshalIndent(failedMessage{Message: m, FailedAt: now.UTC(), Reason: reason}, "", "  ")
		if err != nil {
			return err
		}
		data = append(out, '\n')
	}
	if err := q.writeFailed(channel, file, data); err != nil {
		return err
	}
	return os.Remove(src)
}

// FailRaw stores data in the failed store under dir, for content that never
// became a message.
func (q *Queue) FailRaw(dir, file string, data []byte) error {
	return q.writeFailed(dir, file, data)
}

func (q *Queue) writeFailed(dir, file string, data []byte) error {
	target := filepath.Join(q.failed, dir)
	if err := mkdir(target); err != nil {
		return err
	}
	return fsutil.WriteFileDurable(filepath.Join(target, safeFile(file)), data, filePerm)
}

// safeFile keeps a file name taken from a source directory inside the
// directory it is written to.
func safeFile(name string) string {
	name = unsafeChars.ReplaceAllString(filepath.Base(name), "_")
	ext := filepath.Ext(name)
	if len(ext) > 16 {
		ext = ""
	}
	return clip(strings.TrimSuffix(name, ext)) + ext
}

// FailedCount is the number of files in the failed store.
func (q *Queue) FailedCount() (int, error) {
	dirs, err := os.ReadDir(q.failed)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(q.failed, d.Name()))
		if err != nil {
			return 0, err
		}
		for _, f := range files {
			if !f.IsDir() && !strings.HasPrefix(f.Name(), ".") {
				n++
			}
		}
	}
	return n, nil
}
