package modules

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alekzonder/onenotify/internal/testmods"
	"github.com/alekzonder/onenotify/pkg/module"
)

var modDir string

func TestMain(m *testing.M) {
	dir, err := testmods.BuildFake()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	modDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func fake(t *testing.T) *Info {
	t.Helper()
	info, err := Load(context.Background(), modDir, "fake")
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func request(params map[string]any) module.Request {
	raw, _ := json.Marshal(params)
	return module.Request{Channel: "c", Params: raw, Message: module.Message{ID: "id-1", Title: "t", Level: "info"}}
}

func send(t *testing.T, params map[string]any, timeout time.Duration) (*module.Response, error) {
	t.Helper()
	return Send(context.Background(), fake(t), request(params), timeout, &bytes.Buffer{})
}

func TestPath(t *testing.T) {
	if got := Path("/x/modules", "ntfy"); got != "/x/modules/onenotify-ntfy" {
		t.Errorf("Path = %q", got)
	}
}

func TestLoad(t *testing.T) {
	info := fake(t)
	if info.Name != "fake" || info.Version != "1.2.3" || info.Protocol != 1 || !info.Compatible() {
		t.Errorf("info = %+v", info)
	}
	if info.Path != Path(modDir, "fake") || !json.Valid(info.ParamsSchema) {
		t.Errorf("info = %+v", info)
	}
	if _, err := Load(context.Background(), modDir, "absent"); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("missing module: %v", err)
	}
}

func TestLoadRejects(t *testing.T) {
	for mode, want := range map[string]string{
		"protocol99":       "protocol 99",
		"other-name":       `calls itself "other"`,
		"describe-garbage": "invalid JSON",
		"describe-fail":    "describe",
	} {
		t.Setenv("FAKEMOD_MODE", mode)
		_, err := Load(context.Background(), modDir, "fake")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", mode, err)
		}
	}
	t.Setenv("FAKEMOD_MODE", "protocol99")
	if _, err := Load(context.Background(), modDir, "fake"); !errors.Is(err, ErrIncompatible) {
		t.Errorf("protocol99: %v", err)
	}
}

func TestListIgnoresNonModules(t *testing.T) {
	dir := t.TempDir()
	data, err := os.ReadFile(Path(modDir, "fake"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]os.FileMode{
		"onenotify-fake":     0o755,
		"onenotify-renamed":  0o755, // describes itself as "fake"
		"onenotify-noexec":   0o644,
		"onenotify-part.tmp": 0o755,
		"something-else":     0o755,
		"installed.json":     0o644,
	}
	for name, mode := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "onenotify-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	infos, errs := List(context.Background(), dir)
	if len(infos) != 1 || infos[0].Name != "fake" {
		t.Errorf("infos = %+v", infos)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "onenotify-renamed") {
		t.Errorf("errs = %v", errs)
	}
}

func TestListMissingDir(t *testing.T) {
	infos, errs := List(context.Background(), filepath.Join(t.TempDir(), "absent"))
	if len(infos) != 0 || len(errs) != 0 {
		t.Errorf("infos=%v errs=%v", infos, errs)
	}
}

func TestSendOutcomes(t *testing.T) {
	record := filepath.Join(t.TempDir(), "record")
	resp, err := send(t, map[string]any{"record": record}, 5*time.Second)
	if err != nil || !resp.Delivered || resp.Error != nil {
		t.Fatalf("ok: resp=%+v err=%v", resp, err)
	}
	var got module.Request
	data, _ := os.ReadFile(record)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Protocol != 1 || got.TimeoutMS != 5000 || got.Channel != "c" || got.Message.ID != "id-1" {
		t.Errorf("the module received %+v", got)
	}

	resp, err = send(t, map[string]any{"mode": "temporary"}, 5*time.Second)
	if err != nil || resp.Delivered || resp.Error.Message != "server is down" || resp.Error.Permanent {
		t.Errorf("temporary: resp=%+v err=%v", resp, err)
	}
	resp, err = send(t, map[string]any{"mode": "permanent"}, 5*time.Second)
	if err != nil || resp.Delivered || !resp.Error.Permanent {
		t.Errorf("permanent: resp=%+v err=%v", resp, err)
	}
	resp, err = send(t, map[string]any{"mode": "retry_after", "retry_after_ms": 1500}, 5*time.Second)
	if err != nil || resp.Delivered || resp.Error.RetryAfterMS != 1500 {
		t.Errorf("retry_after: resp=%+v err=%v", resp, err)
	}
}

func TestSendBrokenModule(t *testing.T) {
	for mode, want := range map[string]string{
		"exit3":         "exit code 3",
		"garbage":       "invalid JSON",
		"silent":        "invalid JSON",
		"ok_then_exit3": "exit code 3",
		"huge":          "too large",
		"no_reason":     "gave no reason",
	} {
		resp, err := send(t, map[string]any{"mode": mode}, 5*time.Second)
		if err == nil || resp != nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: resp=%+v err=%v", mode, resp, err)
		}
	}
}

func TestSendTimeoutKillsGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	start := time.Now()
	_, err := send(t, map[string]any{"mode": "hang", "pidfile": pidFile}, 300*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Send blocked for %v", elapsed)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(raw))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("child process %d of the module survived the timeout", pid)
}

func TestSendAnswerThenHangIsATimeout(t *testing.T) {
	start := time.Now()
	resp, err := send(t, map[string]any{"mode": "ok_then_hang"}, 300*time.Millisecond)
	if err == nil || resp != nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("resp=%+v err=%v", resp, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Send blocked for %v", elapsed)
	}
}

// alive reports whether the process exists and is not a zombie waiting to be
// reaped by init.
func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	return len(fields) == 0 || fields[0] != "Z"
}

func TestDescribeHangDoesNotBlock(t *testing.T) {
	old := describeTimeout
	describeTimeout = 300 * time.Millisecond
	defer func() { describeTimeout = old }()
	t.Setenv("FAKEMOD_MODE", "describe-hang")
	start := time.Now()
	_, err := Describe(context.Background(), Path(modDir, "fake"))
	if err == nil {
		t.Fatal("a hanging describe must be an error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Describe blocked for %v although its child holds stdout open", elapsed)
	}
}

func TestSendInterruptedIsNotATimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	_, err := Send(ctx, fake(t), request(map[string]any{"mode": "hang"}), 30*time.Second, &bytes.Buffer{})
	if !errors.Is(err, ErrInterrupted) || strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v, want an interruption, not a timeout", err)
	}
}

func TestModuleStderrIsPassedOn(t *testing.T) {
	var stderr bytes.Buffer
	if _, err := Send(context.Background(), fake(t), request(nil), 5*time.Second, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "fakemod: sending id-1 to c") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestAnswerIsUsedWhenAChildKeepsStdoutOpen(t *testing.T) {
	start := time.Now()
	resp, err := send(t, map[string]any{"mode": "ok_with_child"}, 10*time.Second)
	if err != nil || !resp.Delivered {
		t.Errorf("resp=%+v err=%v", resp, err)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("Send waited %v for the child", elapsed)
	}
}
