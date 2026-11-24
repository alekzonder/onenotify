package deliver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alekzonder/onenotify/internal/config"
	"github.com/alekzonder/onenotify/internal/message"
	"github.com/alekzonder/onenotify/internal/queue"
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
	os.RemoveAll(dir)
	os.Exit(code)
}

var t0 = time.Date(2026, 11, 23, 10, 5, 0, 0, time.UTC)

// env is a onenotify setup in a temp dir: config, spool, failed, one source.
type env struct {
	t   *testing.T
	dir string
	now time.Time
	log bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, dir: t.TempDir(), now: t0}
	if err := os.MkdirAll(e.path("events"), 0o750); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) path(parts ...string) string {
	return filepath.Join(append([]string{e.dir}, parts...)...)
}

// record is the file the fake module of a channel appends its requests to.
func (e *env) record(channel string) string { return e.path("record-" + channel) }

// channel returns the YAML of a channel served by the fake module. extra is
// more params, as YAML flow pairs: `mode: temporary`.
func (e *env) channel(name string, extra ...string) string {
	params := append([]string{"record: " + e.record(name)}, extra...)
	return fmt.Sprintf("  - {name: %s, module: fake, timeout: 5s, params: {%s}}\n", name, strings.Join(params, ", "))
}

func (e *env) load(channels ...string) *config.Config {
	e.t.Helper()
	content := fmt.Sprintf("host: web-01\npaths: {modules: %s, spool: spool, failed: failed}\nlog: {dir: log}\n"+
		"sources:\n  - {type: onecheck, path: events}\nchannels:\n%s", modDir, strings.Join(channels, ""))
	if len(channels) == 0 {
		content = strings.TrimSuffix(content, "channels:\n")
	}
	path := e.path("config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		e.t.Fatal(err)
	}
	return cfg
}

func (e *env) queue(cfg *config.Config) *queue.Queue {
	e.t.Helper()
	q, err := queue.Open(cfg.Paths.Spool, cfg.Paths.Failed, cfg.Path)
	if err != nil {
		e.t.Fatal(err)
	}
	return q
}

func (e *env) runOpt(opt Options, channels ...string) (*Report, error) {
	e.t.Helper()
	cfg := e.load(channels...)
	opt.Now = func() time.Time { return e.now }
	opt.Log = slog.New(slog.NewTextHandler(&e.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return Run(context.Background(), cfg, e.queue(cfg), opt)
}

func (e *env) run(channels ...string) *Report {
	e.t.Helper()
	rep, err := e.runOpt(Options{}, channels...)
	if err != nil {
		e.t.Fatalf("Run: %v", err)
	}
	return rep
}

// event drops a onecheck event into the source. seq orders the events.
func (e *env) event(seq int, transition string) string {
	e.t.Helper()
	at := time.Date(2026, 11, 23, 10, 0, seq, 0, time.UTC)
	id := fmt.Sprintf("%s-main-rule%d-%s", at.Format("20060102T150405Z"), seq, transition)
	ev := map[string]any{
		"id": id, "config": "main", "rule": fmt.Sprintf("rule%d", seq), "transition": transition,
		"at": at.Format(time.RFC3339), "expires_at": at.Add(24 * time.Hour).Format(time.RFC3339),
		"params": map[string]any{"path": "/"}, "redacted": false,
		"result": map[string]any{"module": "disk", "tags": []string{"disk"}, "status": "fail", "consecutive": 1,
			"failures": []map[string]any{{"message": "used_percent: expected lt 90, got 97"}}},
	}
	data, _ := json.Marshal(ev)
	if err := os.WriteFile(e.path("events", id+".json"), data, 0o640); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) files(parts ...string) []string {
	e.t.Helper()
	entries, err := os.ReadDir(e.path(parts...))
	if err != nil {
		return nil
	}
	var out []string
	for _, en := range entries {
		if !strings.HasPrefix(en.Name(), ".") {
			out = append(out, en.Name())
		}
	}
	return out
}

// received is what the fake module of a channel was asked to send, in order.
func (e *env) received(channel string) []module.Request {
	e.t.Helper()
	data, err := os.ReadFile(e.record(channel))
	if err != nil {
		return nil
	}
	var out []module.Request
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var req module.Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			e.t.Fatalf("record line %q: %v", line, err)
		}
		out = append(out, req)
	}
	return out
}

func (e *env) receivedIDs(channel string) string {
	var ids []string
	for _, req := range e.received(channel) {
		ids = append(ids, req.Message.ID)
	}
	return strings.Join(ids, " ")
}

func outcomes(rep *Report) string {
	var out []string
	for _, r := range rep.Records {
		out = append(out, fmt.Sprintf("%s:%s", r.Channel, r.Outcome))
	}
	return strings.Join(out, " ")
}

func TestIntakeFansOutAndDelivers(t *testing.T) {
	e := newEnv(t)
	id := e.event(1, "fail")
	off := "  - {name: off, module: fake, enabled: false, params: {record: " + e.record("off") + "}}\n"
	rep := e.run(e.channel("a"), e.channel("b"), off)

	if rep.Ingested != 1 || rep.Sent != 2 || rep.Waiting != 0 || rep.Failed != 0 || rep.Total() != 2 {
		t.Errorf("report = %+v", rep)
	}
	if outcomes(rep) != "a:sent b:sent" {
		t.Errorf("records = %s", outcomes(rep))
	}
	if len(e.files("events")) != 0 {
		t.Errorf("the event is still in the source: %v", e.files("events"))
	}
	for _, ch := range []string{"a", "b"} {
		got := e.received(ch)
		if len(got) != 1 {
			t.Fatalf("%s received %d messages", ch, len(got))
		}
		m := got[0].Message
		if m.ID != id || m.Title != "[web-01] FAIL main/rule1" || m.Level != "error" || m.Host != "web-01" || m.Source != "onecheck" {
			t.Errorf("%s got %+v", ch, m)
		}
		if !strings.Contains(m.Text, "module: disk\npath: /\ntags: disk\n") || !strings.Contains(m.Text, "- used_percent: expected lt 90, got 97") {
			t.Errorf("%s text = %q", ch, m.Text)
		}
		if got[0].Channel != ch || got[0].TimeoutMS != 5000 {
			t.Errorf("%s request = %+v", ch, got[0])
		}
		if len(e.files("spool", ch)) != 0 {
			t.Errorf("%s queue is not empty: %v", ch, e.files("spool", ch))
		}
	}
	if e.received("off") != nil || len(e.files("spool", "off")) != 0 {
		t.Error("a disabled channel got a message")
	}

	// Nothing left: a second run does nothing.
	if rep := e.run(e.channel("a"), e.channel("b")); rep.Total() != 0 || rep.Ingested != 0 || len(e.received("a")) != 1 {
		t.Errorf("second run: %+v", rep)
	}
}

func TestNoEnabledChannelsLeavesEventsInTheSource(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	rep := e.run()
	if rep.Ingested != 0 || len(e.files("events")) != 1 {
		t.Errorf("report = %+v, events = %v", rep, e.files("events"))
	}
	// Not a warning: cron would mail it every minute.
	if strings.Contains(e.log.String(), "level=WARN") || !strings.Contains(e.log.String(), "no channel is enabled") {
		t.Errorf("log = %s", e.log.String())
	}
}

func TestMissingSourceDirectoryIsNotAnError(t *testing.T) {
	e := newEnv(t)
	os.RemoveAll(e.path("events"))
	if rep := e.run(e.channel("a")); rep.Total() != 0 {
		t.Errorf("report = %+v", rep)
	}
}

func TestIntakeWriteFailureStopsTheRunBeforeDelivery(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	// Channel "b" cannot get a queue directory: a file is in the way.
	if err := os.MkdirAll(e.path("spool"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.path("spool", "b"), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	rep, err := e.runOpt(Options{}, e.channel("a"), e.channel("b"))
	if err == nil || !strings.Contains(err.Error(), "cannot queue event") {
		t.Fatalf("err = %v", err)
	}
	if len(e.files("events")) != 1 {
		t.Error("the event must stay in the source")
	}
	if e.received("a") != nil || rep.Sent != 0 {
		t.Errorf("nothing may be delivered in a run whose intake failed: a received %v", e.receivedIDs("a"))
	}

	// Once the problem is fixed, every channel gets the event exactly once.
	os.Remove(e.path("spool", "b"))
	rep = e.run(e.channel("a"), e.channel("b"))
	if rep.Sent != 2 || len(e.received("a")) != 1 || len(e.received("b")) != 1 {
		t.Errorf("after the fix: %+v a=%q b=%q", rep, e.receivedIDs("a"), e.receivedIDs("b"))
	}
}

func TestUnreadableEventGoesToFailedAndTheRunContinues(t *testing.T) {
	e := newEnv(t)
	for name, content := range map[string]string{
		"20261123T100000Z-broken.json":    "{oops",
		"20261123T100000Z-array.json":     "[1]",
		"20261123T100000Z-noresult.json":  `{"id":"x","rule":"r","transition":"fail","result":null}`,
		"20261123T100000Z-notevent.json":  `{"hello":"world"}`,
		".20261123T100000Z-partial.json":  "{",
		"20261123T100000Z-readme.txt":     "not json at all",
		"20261123T100000Z-emptyfile.json": "",
	} {
		if err := os.WriteFile(e.path("events", name), []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	good := e.event(1, "fail")
	rep := e.run(e.channel("a"))
	// "noresult" is a usable event: it has id, rule and transition.
	if rep.Ingested != 6 || e.receivedIDs("a") != "x "+good && e.receivedIDs("a") != good+" x" {
		t.Errorf("report = %+v, received %q", rep, e.receivedIDs("a"))
	}
	unreadable := strings.Join(e.files("failed", queue.Unreadable), " ")
	if unreadable != "20261123T100000Z-array.json 20261123T100000Z-broken.json 20261123T100000Z-emptyfile.json 20261123T100000Z-notevent.json" {
		t.Errorf("unreadable = %s", unreadable)
	}
	// Files being written and files that are not .json are left alone.
	left, _ := os.ReadDir(e.path("events"))
	if len(left) != 2 {
		t.Errorf("left in the source: %v", left)
	}
}

func TestOrderWithinAChannel(t *testing.T) {
	e := newEnv(t)
	third := e.event(3, "fail")
	first := e.event(1, "fail")
	second := e.event(2, "recovered")
	e.run(e.channel("a"))
	if got := e.receivedIDs("a"); got != first+" "+second+" "+third {
		t.Errorf("order = %s", got)
	}
}

// failingRun queues three events for a failing channel "a" and a working "b"
// and runs once. It returns the env, the report and the id of the first event.
func failingRun(t *testing.T) (*env, *Report, string) {
	t.Helper()
	e := newEnv(t)
	first := e.event(1, "fail")
	e.event(2, "fail")
	e.event(3, "fail")
	rep := e.run(e.channel("a", "mode: temporary", "token: s3cr3t-token"), e.channel("b"))
	return e, rep, first
}

func TestTemporaryErrorStopsOnlyThatChannel(t *testing.T) {
	e, rep, first := failingRun(t)

	if rep.Sent != 3 || rep.Waiting != 3 || rep.Failed != 0 {
		t.Errorf("report = %+v", rep)
	}
	if outcomes(rep) != "a:waiting b:sent b:sent b:sent" || rep.Records[0].Error != "server is down" || rep.Records[0].ID != first {
		t.Errorf("records = %+v", rep.Records)
	}
	if e.receivedIDs("a") != first {
		t.Errorf("a must be asked once, for the first message only; got %q", e.receivedIDs("a"))
	}
	if len(e.files("spool", "a")) != 3 || len(e.files("spool", "b")) != 0 {
		t.Errorf("queues: a=%v b=%v", e.files("spool", "a"), e.files("spool", "b"))
	}
	cfg := e.load(e.channel("a"))
	st := e.queue(cfg).Status("a")
	if st == nil || st.Consecutive != 1 || st.LastError != "server is down" || !st.FailingSince.Equal(t0) || st.RetryAt != nil {
		t.Fatalf("status = %+v", st)
	}
	if n := strings.Count(e.log.String(), "level=WARN"); n != 1 {
		t.Errorf("the first failure must be one warning, got %d:\n%s", n, e.log.String())
	}
	if strings.Contains(e.log.String(), "s3cr3t-token") {
		t.Errorf("the log holds a channel secret:\n%s", e.log.String())
	}
}

func TestRepeatedFailureIsQuietAndRecoveryClearsIt(t *testing.T) {
	e, _, _ := failingRun(t)
	cfg := e.load(e.channel("a"))

	// Second run: still failing, logged quietly, counted.
	e.log.Reset()
	e.now = t0.Add(time.Minute)
	rep := e.run(e.channel("a", "mode: temporary", "token: s3cr3t-token"), e.channel("b"))
	if rep.Sent != 0 || rep.Waiting != 3 {
		t.Errorf("second run: %+v", rep)
	}
	if st := e.queue(cfg).Status("a"); st == nil || st.Consecutive != 2 || !st.FailingSince.Equal(t0) {
		t.Errorf("status after second run = %+v", st)
	}
	if strings.Contains(e.log.String(), "level=WARN") || !strings.Contains(e.log.String(), "still cannot deliver") {
		t.Errorf("a repeated failure must not warn again:\n%s", e.log.String())
	}
	if strings.Contains(e.log.String(), "s3cr3t-token") {
		t.Errorf("the log holds a channel secret:\n%s", e.log.String())
	}

	// The channel is fixed: everything goes out in order, the status is gone.
	e.log.Reset()
	rep = e.run(e.channel("a"), e.channel("b"))
	if rep.Sent != 3 || rep.Waiting != 0 || e.queue(cfg).Status("a") != nil {
		t.Errorf("after the fix: %+v status=%+v", rep, e.queue(cfg).Status("a"))
	}
	if !strings.Contains(e.log.String(), "channel delivers again") {
		t.Errorf("log = %s", e.log.String())
	}
}

func TestPermanentErrorFailsTheMessageAndGoesOn(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	bad := e.event(2, "fail")
	e.event(3, "fail")
	rep := e.run(e.channel("a", "mode: permanent", "only: rule2"))
	if rep.Sent != 2 || rep.Failed != 1 || rep.Waiting != 0 || outcomes(rep) != "a:sent a:failed a:sent" {
		t.Errorf("report = %+v", rep)
	}
	if rep.Records[1].ID != bad || rep.Records[1].Error != "message rejected" {
		t.Errorf("record = %+v", rep.Records[1])
	}
	if len(e.received("a")) != 3 || len(e.files("spool", "a")) != 0 {
		t.Errorf("received %q, queue %v", e.receivedIDs("a"), e.files("spool", "a"))
	}
	data, err := os.ReadFile(e.path("failed", "a", bad+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var failed map[string]any
	if json.Unmarshal(data, &failed) != nil || failed["reason"] != "message rejected" || failed["failed_at"] != "2026-11-23T10:05:00Z" {
		t.Errorf("failed file = %s", data)
	}
	if strings.Contains(string(data), "record-a") {
		t.Errorf("the failed file holds channel params: %s", data)
	}
	cfg := e.load(e.channel("a"))
	if e.queue(cfg).Status("a") != nil {
		t.Error("a rejected message is not a failure of the channel")
	}
}

func TestExpiredMessagesAreFailedWithoutAsking(t *testing.T) {
	e := newEnv(t)
	old := e.event(1, "fail")
	e.run(e.channel("a", "mode: temporary"), e.channel("gone", "mode: temporary"))
	e.now = t0.Add(25 * time.Hour)
	before := len(e.received("a"))

	// "gone" is no longer in the config; its queue expires all the same.
	rep := e.run(e.channel("a"))
	if rep.Failed != 2 || rep.Sent != 0 || rep.Waiting != 0 || outcomes(rep) != "a:failed gone:failed" {
		t.Errorf("report = %+v", rep)
	}
	if rep.Records[0].Error != "expired at 2026-11-24T10:00:01Z" {
		t.Errorf("reason = %q", rep.Records[0].Error)
	}
	if len(e.received("a")) != before {
		t.Error("an expired message must not be sent")
	}
	for _, ch := range []string{"a", "gone"} {
		if _, err := os.Stat(e.path("failed", ch, old+".json")); err != nil {
			t.Errorf("%s: %v", ch, err)
		}
	}
}

func TestEventExpiredBeforeIntakeIsNotSent(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	e.now = t0.Add(48 * time.Hour)
	rep := e.run(e.channel("a"))
	if rep.Ingested != 1 || rep.Failed != 1 || e.received("a") != nil {
		t.Errorf("report = %+v, received %q", rep, e.receivedIDs("a"))
	}
}

func TestRetryAfterIsRespectedBetweenRuns(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	slow := e.channel("a", "mode: retry_after", "retry_after_ms: 120000")
	rep := e.run(slow)
	if rep.Waiting != 1 || len(e.received("a")) != 1 {
		t.Fatalf("report = %+v", rep)
	}
	cfg := e.load(slow)
	st := e.queue(cfg).Status("a")
	if st == nil || st.RetryAt == nil || !st.RetryAt.Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("status = %+v", st)
	}

	e.now = t0.Add(time.Minute)
	rep = e.run(e.channel("a"))
	if rep.Waiting != 1 || len(rep.Records) != 0 || len(e.received("a")) != 1 {
		t.Errorf("before retry_at the module must not be started: %+v, received %d", rep, len(e.received("a")))
	}

	e.now = t0.Add(2 * time.Minute)
	rep = e.run(e.channel("a"))
	if rep.Sent != 1 || len(e.received("a")) != 2 {
		t.Errorf("after retry_at: %+v, received %d", rep, len(e.received("a")))
	}
}

func TestBrokenModuleKeepsTheMessage(t *testing.T) {
	for _, mode := range []string{"exit3", "garbage", "silent", "ok_then_exit3", "no_reason"} {
		e := newEnv(t)
		e.event(1, "fail")
		rep := e.run(e.channel("a", "mode: "+mode))
		if rep.Waiting != 1 || rep.Sent != 0 || rep.Failed != 0 || len(e.files("spool", "a")) != 1 {
			t.Errorf("%s: report = %+v, queue = %v", mode, rep, e.files("spool", "a"))
		}
	}
}

func TestHangingModuleKeepsTheMessage(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	slow := "  - {name: a, module: fake, timeout: 300ms, params: {mode: hang}}\n"
	start := time.Now()
	rep := e.run(slow)
	if rep.Waiting != 1 || !strings.Contains(rep.Records[0].Error, "timed out after 300ms") {
		t.Errorf("report = %+v", rep)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("the run took %v", time.Since(start))
	}
}

func TestUnusableChannelsWait(t *testing.T) {
	cases := map[string]struct{ channel, want string }{
		"module not installed": {"  - {name: a, module: absent}\n", "module is not installed: absent"},
		"params do not match":  {"  - {name: a, module: fake, params: {nope: 1, token: SECRETVALUE}}\n", "invalid params: /: unknown key nope"},
		"variable not set":     {"  - {name: a, module: fake, params: {token: \"${ONENOTIFY_TEST_UNSET}\"}}\n", "ONENOTIFY_TEST_UNSET is not set"},
	}
	for name, c := range cases {
		e := newEnv(t)
		id := e.event(1, "fail")
		e.event(2, "fail")
		rep := e.run(c.channel)
		if rep.Waiting != 2 || len(rep.Records) != 1 || rep.Records[0].Outcome != Waiting || rep.Records[0].ID != id ||
			!strings.Contains(rep.Records[0].Error, c.want) {
			t.Errorf("%s: report = %+v", name, rep)
		}
		if len(e.files("spool", "a")) != 2 {
			t.Errorf("%s: queue = %v", name, e.files("spool", "a"))
		}
		if strings.Contains(e.log.String(), "SECRETVALUE") || strings.Contains(rep.Records[0].Error, "SECRETVALUE") {
			t.Errorf("%s: a param value leaked", name)
		}
		cfg := e.load(c.channel)
		if st := e.queue(cfg).Status("a"); st == nil || st.Consecutive != 1 {
			t.Errorf("%s: status = %+v", name, st)
		}
	}
}

func TestIncompatibleModuleWaits(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	t.Setenv("FAKEMOD_MODE", "protocol99")
	rep := e.run(e.channel("a"))
	if rep.Waiting != 1 || !strings.Contains(rep.Records[0].Error, "protocol 99") {
		t.Errorf("report = %+v", rep)
	}
}

func TestChannelFilterDeliversOnlyThoseButTakesInForAll(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	rep, err := e.runOpt(Options{Channels: []string{"a"}}, e.channel("a"), e.channel("b"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ingested != 1 || rep.Sent != 1 || rep.Waiting != 0 || outcomes(rep) != "a:sent" {
		t.Errorf("report = %+v", rep)
	}
	if e.received("b") != nil || len(e.files("spool", "b")) != 1 {
		t.Errorf("b: received %q, queue %v", e.receivedIDs("b"), e.files("spool", "b"))
	}
	if rep := e.run(e.channel("a"), e.channel("b")); rep.Sent != 1 || outcomes(rep) != "b:sent" {
		t.Errorf("next full run: %+v", rep)
	}
}

func TestDisabledChannelKeepsItsQueueUntilExpiry(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	e.run(e.channel("a", "mode: temporary"))
	off := "  - {name: a, module: fake, enabled: false}\n"
	rep := e.run(off)
	if rep.Total() != 0 || len(e.files("spool", "a")) != 1 {
		t.Errorf("a disabled channel must be left alone: %+v", rep)
	}
	e.now = t0.Add(25 * time.Hour)
	if rep := e.run(off); rep.Failed != 1 {
		t.Errorf("after expiry: %+v", rep)
	}
}

func TestUnreadableQueueFileIsFailed(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(e.path("spool", "a"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.path("spool", "a", "20261123T090000Z-broken.json"), []byte("{oops"), 0o640); err != nil {
		t.Fatal(err)
	}
	good := e.event(1, "fail")
	rep := e.run(e.channel("a"))
	if rep.Failed != 1 || rep.Sent != 1 || e.receivedIDs("a") != good {
		t.Errorf("report = %+v, received %q", rep, e.receivedIDs("a"))
	}
	if len(e.files("failed", "a")) != 1 {
		t.Errorf("failed = %v", e.files("failed", "a"))
	}
}

func TestMessagesQueuedDirectlyAreDelivered(t *testing.T) {
	e := newEnv(t)
	cfg := e.load(e.channel("a"))
	m := message.NewSend("web-01", "backup <done> & dusted 🔴", "42 GB\x00\x1b[31m in 13m", "ok", []string{"backup"}, t0, time.Hour)
	if err := e.queue(cfg).Enqueue("a", m); err != nil {
		t.Fatal(err)
	}
	rep := e.run(e.channel("a"))
	got := e.received("a")
	if rep.Sent != 1 || len(got) != 1 || got[0].Message.Title != "[web-01] backup <done> & dusted 🔴" ||
		got[0].Message.Source != "send" || got[0].Message.Level != "ok" || got[0].Message.Tags[0] != "backup" {
		t.Errorf("report = %+v, received %+v", rep, got)
	}
}

func TestInterruptedRunKeepsMessagesAndBlamesNoChannel(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	cfg := e.load("  - {name: a, module: fake, timeout: 30s, params: {mode: hang}}\n")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	q := e.queue(cfg)
	rep, err := Run(ctx, cfg, q, Options{Now: func() time.Time { return e.now }})
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("err = %v", err)
	}
	if rep.Sent != 0 || len(e.files("spool", "a")) != 1 || q.Status("a") != nil {
		t.Errorf("report = %+v, queue = %v, status = %+v", rep, e.files("spool", "a"), q.Status("a"))
	}
}

func TestManyChannelsInParallel(t *testing.T) {
	e := newEnv(t)
	for i := 1; i <= 5; i++ {
		e.event(i, "fail")
	}
	var channels []string
	for i := 0; i < 8; i++ {
		channels = append(channels, e.channel(fmt.Sprintf("ch%d", i)))
	}
	rep := e.run(channels...)
	if rep.Sent != 40 || rep.Waiting != 0 {
		t.Errorf("report = %+v", rep)
	}
	for i := 0; i < 8; i++ {
		if got := e.received(fmt.Sprintf("ch%d", i)); len(got) != 5 || got[0].Message.ID > got[4].Message.ID {
			t.Errorf("ch%d received %d", i, len(got))
		}
	}
}

func TestMessageThatCannotBePutAwayStopsItsChannel(t *testing.T) {
	e := newEnv(t)
	bad := e.event(1, "fail")
	e.event(2, "fail")
	// The failed store of channel "a" cannot be created: a file is in the way.
	if err := os.MkdirAll(e.path("failed"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.path("failed", "a"), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	rep, err := e.runOpt(Options{}, e.channel("a", "mode: permanent", "only: rule1"), e.channel("b"))
	if err == nil || !strings.Contains(err.Error(), "failed store") {
		t.Fatalf("err = %v", err)
	}
	if e.receivedIDs("a") != bad {
		t.Errorf("nothing may overtake a message that is stuck; a received %q", e.receivedIDs("a"))
	}
	if len(e.files("spool", "a")) != 2 || len(e.received("b")) != 2 || rep.Sent != 2 {
		t.Errorf("queue a = %v, b received %d, report %+v", e.files("spool", "a"), len(e.received("b")), rep)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	e.run(e.channel("a", "mode: retry_after", "retry_after_ms: 9000000000000"))
	cfg := e.load(e.channel("a"))
	st := e.queue(cfg).Status("a")
	if st == nil || st.RetryAt == nil || !st.RetryAt.Equal(t0.Add(24*time.Hour)) {
		t.Fatalf("status = %+v", st)
	}
}

func TestAStoredRetryTimeFarInTheFutureIsIgnored(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	cfg := e.load(e.channel("a"))
	far := t0.Add(300 * 24 * time.Hour) // written while the clock was wrong
	if err := e.queue(cfg).SetStatus("a", queue.Status{FailingSince: t0, Consecutive: 1, RetryAt: &far}); err != nil {
		t.Fatal(err)
	}
	if rep := e.run(e.channel("a")); rep.Sent != 1 {
		t.Errorf("report = %+v", rep)
	}
}

func TestUnusableChannelReportsTheFirstMessageThatWaits(t *testing.T) {
	e := newEnv(t)
	cfg := e.load("  - {name: a, module: absent}\n")
	q := e.queue(cfg)
	old := message.NewSend("web-01", "old", "", "info", nil, t0.Add(-2*time.Hour), time.Hour)
	fresh := message.NewSend("web-01", "fresh", "", "info", nil, t0, time.Hour)
	for _, m := range []message.Message{old, fresh} {
		if err := q.Enqueue("a", m); err != nil {
			t.Fatal(err)
		}
	}
	rep := e.run("  - {name: a, module: absent}\n")
	if outcomes(rep) != "a:failed a:waiting" || rep.Records[0].ID != old.ID || rep.Records[1].ID != fresh.ID {
		t.Errorf("records = %+v", rep.Records)
	}
}

func TestEventWithAVeryLongIDIsDelivered(t *testing.T) {
	e := newEnv(t)
	id := "20261123T100000Z-" + strings.Repeat("r", 300) + "-fail"
	data := `{"id":"` + id + `","rule":"r","transition":"fail","at":"2026-11-23T10:00:00Z","expires_at":"2026-11-24T10:00:00Z"}`
	if err := os.WriteFile(e.path("events", "long.json"), []byte(data), 0o640); err != nil {
		t.Fatal(err)
	}
	if rep := e.run(e.channel("a")); rep.Sent != 1 || e.receivedIDs("a") != id {
		t.Errorf("report = %+v", rep)
	}
}

func TestEventThatCannotBeStoredDoesNotBlockTheOthers(t *testing.T) {
	e := newEnv(t)
	data := `{"id":"20261123T095900Z-far","rule":"r","transition":"fail","at":"9999-12-31T23:59:59Z"}`
	if err := os.WriteFile(e.path("events", "20261123T095900Z-far.json"), []byte(data), 0o640); err != nil {
		t.Fatal(err)
	}
	good := e.event(1, "fail")
	rep := e.run(e.channel("a"))
	if rep.Sent != 1 || e.receivedIDs("a") != good || len(e.files("failed", queue.Unreadable)) != 1 {
		t.Errorf("report = %+v, received %q", rep, e.receivedIDs("a"))
	}
}

func TestQueueEmptiedByExpiryIsNoLongerFailing(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	down := e.channel("a", "mode: temporary")
	e.run(down)
	cfg := e.load(down)
	if e.queue(cfg).Status("a") == nil {
		t.Fatal("the channel must be failing first")
	}
	e.now = t0.Add(25 * time.Hour)
	e.run(down)
	if st := e.queue(cfg).Status("a"); st != nil {
		t.Errorf("status of an empty queue = %+v", st)
	}
}

func TestEmptyQueueOfARemovedChannelIsForgotten(t *testing.T) {
	e := newEnv(t)
	e.event(1, "fail")
	e.run(e.channel("a"), e.channel("gone", "mode: temporary"))
	e.now = t0.Add(25 * time.Hour)
	e.run(e.channel("a")) // expires what "gone" had
	e.run(e.channel("a")) // finds its queue empty
	if _, err := os.Stat(e.path("spool", "gone")); err == nil {
		t.Error("the directory of a removed channel is still there")
	}
	if _, err := os.Stat(e.path("spool", "a")); err != nil {
		t.Error("the directory of a configured channel must stay")
	}
}
