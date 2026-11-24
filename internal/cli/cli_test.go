package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alekzonder/onenotify/internal/modules"
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

func execute(args ...string) (code int, stdout, stderr string) {
	return executeIn("", args...)
}

func executeIn(stdin string, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = Execute(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// setup writes a config whose channels are served by the fake module.
// channels is YAML; RECORD-<name> in it becomes a file the channel records
// its requests in. It returns the directory and the config path.
type testEnv struct {
	t   *testing.T
	dir string
	cfg string
}

func setup(t *testing.T, extra, channels string) *testEnv {
	t.Helper()
	e := &testEnv{t: t, dir: t.TempDir()}
	channels = strings.ReplaceAll(channels, "RECORD-", filepath.Join(e.dir, "record-"))
	logLine := "log: {dir: log}\n"
	if strings.Contains(extra, "log:") {
		logLine = ""
	}
	content := fmt.Sprintf("host: web-01\npaths: {modules: %s, spool: spool, failed: failed}\n"+logLine+
		"sources:\n  - {type: onecheck, path: events}\n%s\nchannels:\n%s", modDir, extra, channels)
	if channels == "" {
		content = strings.TrimSuffix(content, "channels:\n")
	}
	e.cfg = filepath.Join(e.dir, "config.yaml")
	if err := os.WriteFile(e.cfg, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.dir, "events"), 0o750); err != nil {
		t.Fatal(err)
	}
	return e
}

const (
	chanA    = "  - {name: a, module: fake, timeout: 5s, params: {record: RECORD-a}}\n"
	chanB    = "  - {name: b, module: fake, timeout: 5s, params: {record: RECORD-b}}\n"
	chanDown = "  - {name: down, module: fake, timeout: 5s, params: {record: RECORD-down, mode: temporary}}\n"
)

func (e *testEnv) event(seq int) string {
	e.t.Helper()
	at := time.Now().UTC().Add(time.Duration(seq) * time.Second).Truncate(time.Second)
	id := fmt.Sprintf("%s-main-rule%d-fail", at.Format("20060102T150405Z"), seq)
	ev := map[string]any{
		"id": id, "config": "main", "rule": fmt.Sprintf("rule%d", seq), "transition": "fail",
		"at": at.Format(time.RFC3339), "expires_at": at.Add(24 * time.Hour).Format(time.RFC3339),
		"result": map[string]any{"module": "disk", "status": "fail", "consecutive": 1,
			"failures": []map[string]any{{"message": "used_percent: expected lt 90, got 97"}}},
	}
	data, _ := json.Marshal(ev)
	if err := os.WriteFile(filepath.Join(e.dir, "events", id+".json"), data, 0o640); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *testEnv) received(channel string) []module.Request {
	e.t.Helper()
	data, err := os.ReadFile(filepath.Join(e.dir, "record-"+channel))
	if err != nil {
		return nil
	}
	var out []module.Request
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var req module.Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, req)
	}
	return out
}

func (e *testEnv) queued(channel string) int {
	files, _ := filepath.Glob(filepath.Join(e.dir, "spool", channel, "*.json"))
	n := 0
	for _, f := range files {
		if !strings.HasPrefix(filepath.Base(f), ".") {
			n++
		}
	}
	return n
}

func TestRunDefaultCommand(t *testing.T) {
	e := setup(t, "", chanA+chanB)
	id := e.event(1)
	code, out, errOut := execute("-c", e.cfg)
	if code != 0 || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "SENT    a  "+id+"  (fake, ") ||
		!strings.HasPrefix(lines[1], "SENT    b  "+id) || lines[3] != "2 messages: 2 sent, 0 waiting, 0 failed" {
		t.Errorf("out = %q", out)
	}
	if got := e.received("a"); len(got) != 1 || got[0].Message.Title != "[web-01] FAIL main/rule1" {
		t.Errorf("a received %+v", got)
	}

	code, out, _ = execute("run", "-c", e.cfg)
	if code != 0 || out != "0 messages\n" {
		t.Errorf("nothing to do: code=%d out=%q", code, out)
	}
	log, err := os.ReadFile(filepath.Join(e.dir, "log", "onenotify.log"))
	if err != nil || !strings.Contains(string(log), "message sent") || !strings.Contains(string(log), "event taken") {
		t.Errorf("log = %q err=%v", log, err)
	}
}

func TestRunWithAFailingChannel(t *testing.T) {
	e := setup(t, "", chanA+chanDown)
	e.event(1)
	code, out, errOut := execute("-c", e.cfg)
	if code != 1 || !strings.Contains(out, "SENT    a   ") || !strings.Contains(out, "WAIT    down") ||
		!strings.Contains(out, "\n        server is down\n") || !strings.Contains(out, "2 messages: 1 sent, 1 waiting, 0 failed") {
		t.Errorf("code=%d out=%q", code, out)
	}
	// The first failure of a channel is a warning on stderr, for cron to mail.
	if !strings.Contains(errOut, "channel cannot deliver") || !strings.Contains(errOut, "channel=down") {
		t.Errorf("stderr = %q", errOut)
	}
	// The second run stays quiet on stderr.
	code, _, errOut = execute("-c", e.cfg)
	if code != 1 || errOut != "" {
		t.Errorf("second run: code=%d stderr=%q", code, errOut)
	}
}

func TestRunJSON(t *testing.T) {
	e := setup(t, "", chanA+chanDown)
	id := e.event(1)
	code, out, _ := execute("-c", e.cfg, "-o", "json")
	var got struct {
		Ingested int `json:"ingested"`
		Sent     int `json:"sent"`
		Waiting  int `json:"waiting"`
		Failed   int `json:"failed"`
		Records  []struct {
			Channel, Module, ID, Outcome, Error string
			DurationMS                          int64 `json:"duration_ms"`
		} `json:"records"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	if code != 1 || got.Ingested != 1 || got.Sent != 1 || got.Waiting != 1 || got.Failed != 0 || len(got.Records) != 2 {
		t.Fatalf("code=%d got=%+v", code, got)
	}
	if r := got.Records[1]; r.Channel != "down" || r.Module != "fake" || r.ID != id || r.Outcome != "waiting" || r.Error != "server is down" {
		t.Errorf("record = %+v", r)
	}
	if code, _, errOut := execute("-c", e.cfg, "-o", "xml"); code != 2 || !strings.Contains(errOut, "xml") {
		t.Errorf("unknown format: code=%d err=%q", code, errOut)
	}
}

func TestRunChannelFlag(t *testing.T) {
	e := setup(t, "", chanA+chanB)
	e.event(1)
	code, out, _ := execute("-c", e.cfg, "--channel", "b")
	if code != 0 || !strings.Contains(out, "1 message: 1 sent") || e.received("a") != nil || e.queued("a") != 1 {
		t.Errorf("code=%d out=%q", code, out)
	}
	if code, _, errOut := execute("-c", e.cfg, "--channel", "nope"); code != 2 || !strings.Contains(errOut, `no channel "nope"`) {
		t.Errorf("unknown channel: code=%d err=%q", code, errOut)
	}
}

func TestRunWhileLocked(t *testing.T) {
	e := setup(t, "", chanA)
	e.event(1)
	q, err := queue.Open(filepath.Join(e.dir, "spool"), filepath.Join(e.dir, "failed"), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := q.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	code, out, errOut := execute("-c", e.cfg)
	if code != 2 || out != "" || !strings.Contains(errOut, "another onenotify run is still in progress") {
		t.Errorf("code=%d out=%q err=%q", code, out, errOut)
	}
	if e.received("a") != nil {
		t.Error("a locked-out run must not deliver")
	}
}

func TestRunBadConfig(t *testing.T) {
	e := setup(t, "nonsense: true", chanA)
	if code, _, errOut := execute("-c", e.cfg); code != 2 || !strings.Contains(errOut, "nonsense") {
		t.Errorf("code=%d err=%q", code, errOut)
	}
	if code, _, _ := execute("-c", filepath.Join(e.dir, "absent.yaml")); code != 2 {
		t.Errorf("missing config: code=%d", code)
	}
	if code, _, errOut := execute("--no-such-flag"); code != 2 || !strings.Contains(errOut, "--help") {
		t.Errorf("unknown flag: code=%d err=%q", code, errOut)
	}
}

func TestSend(t *testing.T) {
	e := setup(t, "", chanA+chanB)
	code, out, errOut := execute("send", "-c", e.cfg, "--title", "backup finished", "--text", "42 GB in 13m")
	if code != 0 || !strings.Contains(out, "2 messages: 2 sent") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	for _, ch := range []string{"a", "b"} {
		got := e.received(ch)
		if len(got) != 1 {
			t.Fatalf("%s received %d", ch, len(got))
		}
		m := got[0].Message
		if m.Title != "[web-01] backup finished" || m.Text != "42 GB in 13m" || m.Level != "info" || m.Source != "send" ||
			m.Host != "web-01" || len(m.Tags) != 0 || !strings.Contains(m.ID, "-send-") {
			t.Errorf("%s got %+v", ch, m)
		}
	}

	code, _, _ = execute("send", "-c", e.cfg, "--title", "deploy", "--level", "error", "--tag", "x", "--tag", "y", "--channel", "b")
	got := e.received("b")
	if code != 0 || len(got) != 2 || got[1].Message.Level != "error" || strings.Join(got[1].Message.Tags, ",") != "x,y" ||
		got[1].Message.Text != "" || len(e.received("a")) != 1 {
		t.Errorf("code=%d b=%+v a=%d", code, got, len(e.received("a")))
	}
}

func TestSendTextSources(t *testing.T) {
	e := setup(t, "limits: {text: 20B}", chanA)
	file := filepath.Join(e.dir, "text.txt")
	if err := os.WriteFile(file, []byte("from a file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	last := func() string {
		got := e.received("a")
		return got[len(got)-1].Message.Text
	}
	if code, _, errOut := execute("send", "-c", e.cfg, "--title", "t", "--text-file", file); code != 0 || last() != "from a file" {
		t.Errorf("file: code=%d text=%q err=%q", code, last(), errOut)
	}
	if code, _, _ := executeIn("from stdin\nline 2\n", "send", "-c", e.cfg, "--title", "t", "--text-file", "-"); code != 0 || last() != "from stdin\nline 2" {
		t.Errorf("stdin: code=%d text=%q", code, last())
	}
	if code, _, _ := executeIn("", "send", "-c", e.cfg, "--title", "t", "--text-file", "-"); code != 0 || last() != "" {
		t.Errorf("empty stdin: code=%d text=%q", code, last())
	}
	// 20 bytes fit ten 2-byte letters; an 11th is cut off, never in half.
	if code, _, _ := executeIn(strings.Repeat("я", 30), "send", "-c", e.cfg, "--title", "t", "--text-file", "-"); code != 0 ||
		last() != strings.Repeat("я", 10)+"\n… (truncated)" {
		t.Errorf("long stdin: code=%d text=%q", code, last())
	}
	if code, _, _ := executeIn("", "send", "-c", e.cfg, "--title", "t", "--text", "x"+strings.Repeat("я", 30)); code != 0 ||
		last() != "x"+strings.Repeat("я", 9)+"\n… (truncated)" {
		t.Errorf("long --text: code=%d text=%q", code, last())
	}
	if code, _, _ := executeIn(strings.Repeat("x", 20), "send", "-c", e.cfg, "--title", "t", "--text-file", "-"); code != 0 ||
		last() != strings.Repeat("x", 20) {
		t.Errorf("text of exactly the limit: code=%d text=%q", code, last())
	}
	if code, _, _ := executeIn("a\x00b\xff", "send", "-c", e.cfg, "--title", "t", "--text-file", "-"); code != 0 || last() != "ab" {
		t.Errorf("binary junk: code=%d text=%q", code, last())
	}
}

func TestSendErrors(t *testing.T) {
	e := setup(t, "", chanA+"  - {name: off, module: fake, enabled: false}\n")
	cases := map[string][]string{
		"--title is required":       {"send", "-c", e.cfg, "--text", "x"},
		"must not be empty":         {"send", "-c", e.cfg, "--title", "  "},
		`unknown level "loud"`:      {"send", "-c", e.cfg, "--title", "t", "--level", "loud"},
		"cannot be used together":   {"send", "-c", e.cfg, "--title", "t", "--text", "x", "--text-file", "-"},
		`no channel "nope"`:         {"send", "-c", e.cfg, "--title", "t", "--channel", "nope"},
		`channel "off" is disabled`: {"send", "-c", e.cfg, "--title", "t", "--channel", "off"},
		"cannot read the text":      {"send", "-c", e.cfg, "--title", "t", "--text-file", e.dir},
		"no such file":              {"send", "-c", e.cfg, "--title", "t", "--text-file", filepath.Join(e.dir, "absent.txt")},
		`unknown output format "x"`: {"send", "-c", e.cfg, "--title", "t", "-o", "x"},
		"unknown command":           {"send", "-c", e.cfg, "--title", "t", "stray"},
	}
	for want, args := range cases {
		code, out, errOut := execute(args...)
		if code != 2 || !strings.Contains(errOut, want) {
			t.Errorf("%v: code=%d out=%q err=%q, want %q", args, code, out, errOut, want)
		}
	}
	if e.received("a") != nil || e.queued("a") != 0 {
		t.Error("a rejected send must queue nothing")
	}

	none := setup(t, "", "")
	if code, _, errOut := execute("send", "-c", none.cfg, "--title", "t"); code != 2 || !strings.Contains(errOut, "no channel is enabled") {
		t.Errorf("no channels: code=%d err=%q", code, errOut)
	}
}

func TestSendQueueOnly(t *testing.T) {
	e := setup(t, "", chanA)
	code, out, _ := execute("send", "-c", e.cfg, "--title", "later", "--queue-only")
	if code != 0 || !strings.HasPrefix(out, "queued ") || !strings.HasSuffix(out, " for a\n") {
		t.Errorf("code=%d out=%q", code, out)
	}
	if e.received("a") != nil || e.queued("a") != 1 {
		t.Errorf("received=%v queued=%d", e.received("a"), e.queued("a"))
	}
	if code, out, _ := execute("-c", e.cfg); code != 0 || !strings.Contains(out, "1 message: 1 sent") {
		t.Errorf("next run: code=%d out=%q", code, out)
	}
}

func TestSendToAFailingChannelIsQueued(t *testing.T) {
	e := setup(t, "", chanDown)
	code, out, _ := execute("send", "-c", e.cfg, "--title", "t")
	if code != 1 || !strings.Contains(out, "WAIT") || e.queued("down") != 1 {
		t.Errorf("code=%d out=%q queued=%d", code, out, e.queued("down"))
	}
}

func TestSendTakesWaitingEventsInAndKeepsOrder(t *testing.T) {
	e := setup(t, "", chanA)
	id := e.event(-5)
	code, out, _ := execute("send", "-c", e.cfg, "--title", "mine")
	got := e.received("a")
	if code != 0 || len(got) != 2 || got[0].Message.ID != id || got[1].Message.Title != "[web-01] mine" {
		t.Errorf("code=%d out=%q received=%+v", code, out, got)
	}
}

func TestSendWhileLockedStaysQueued(t *testing.T) {
	old := lockWait
	lockWait = 300 * time.Millisecond
	defer func() { lockWait = old }()
	e := setup(t, "", chanA)
	q, err := queue.Open(filepath.Join(e.dir, "spool"), filepath.Join(e.dir, "failed"), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := q.Lock()
	if err != nil {
		t.Fatal(err)
	}
	code, out, errOut := execute("send", "-c", e.cfg, "--title", "t")
	unlock()
	if code != 1 || !strings.HasPrefix(out, "queued ") || !strings.Contains(errOut, "stays queued") || e.queued("a") != 1 || e.received("a") != nil {
		t.Errorf("code=%d out=%q err=%q queued=%d", code, out, errOut, e.queued("a"))
	}
}

func TestValidate(t *testing.T) {
	e := setup(t, "", chanA+chanB)
	code, out, errOut := execute("validate", "-c", e.cfg)
	if code != 0 || !strings.Contains(out, "config OK: 2 channels, 1 source (") || errOut != "" {
		t.Errorf("code=%d out=%q err=%q", code, out, errOut)
	}
	if e.received("a") != nil {
		t.Error("validate must not send")
	}

	bad := setup(t, "", chanA+
		"  - {name: nomod, module: absent}\n"+
		"  - {name: badparams, module: fake, params: {nope: 1, token: SECRETVALUE}}\n"+
		"  - {name: novar, module: fake, params: {token: \"${ONENOTIFY_TEST_UNSET}\"}}\n")
	os.RemoveAll(filepath.Join(bad.dir, "events"))
	code, out, errOut = execute("validate", "-c", bad.cfg)
	if code != 2 || out != "" {
		t.Errorf("code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"channel nomod: module is not installed: absent",
		"channel badparams: invalid params: /: unknown key nope",
		"channel novar: environment variable ONENOTIFY_TEST_UNSET is not set",
		"warning: source " + filepath.Join(bad.dir, "events") + " does not exist yet",
		"3 channel(s) of " + bad.cfg + " are not usable",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	if strings.Contains(errOut, "SECRETVALUE") {
		t.Errorf("a param value leaked: %s", errOut)
	}

	none := setup(t, "", "")
	if code, _, errOut := execute("validate", "-c", none.cfg); code != 0 || !strings.Contains(errOut, "no channel is enabled") {
		t.Errorf("no channels: code=%d err=%q", code, errOut)
	}
}

func TestValidateIncompatibleModule(t *testing.T) {
	e := setup(t, "", chanA)
	t.Setenv("FAKEMOD_MODE", "protocol99")
	if code, _, errOut := execute("validate", "-c", e.cfg); code != 2 || !strings.Contains(errOut, "protocol 99") {
		t.Errorf("code=%d err=%q", code, errOut)
	}
}

const statusChannels = chanA + chanDown + "  - {name: off, module: fake, enabled: false}\n"

func TestStatusOnAFreshSetupCreatesNothing(t *testing.T) {
	e := setup(t, "", statusChannels)
	code, out, _ := execute("status", "-c", e.cfg)
	if code != 0 || !strings.Contains(out, "failed: 0 messages in "+filepath.Join(e.dir, "failed")) {
		t.Errorf("fresh: code=%d out=%q", code, out)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "spool")); err == nil {
		t.Error("status must not create the spool")
	}
}

// statusEnv has one delivered channel, one failing with a queued message,
// one disabled, one queue left by a removed channel, and one waiting event.
func statusEnv(t *testing.T) *testEnv {
	t.Helper()
	e := setup(t, "", statusChannels)
	e.event(1)
	execute("-c", e.cfg)
	e.event(2)
	// A queue left by a channel that is no longer in the config.
	if err := os.MkdirAll(filepath.Join(e.dir, "spool", "old"), 0o750); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestStatusTable(t *testing.T) {
	e := statusEnv(t)
	code, out, _ := execute("status", "-c", e.cfg)
	if code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	rows := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			rows[f[0]] = f
		}
	}
	if r := rows["a"]; len(r) < 5 || r[1] != "fake" || r[2] != "0" || r[3] != "-" || r[4] != "ok" {
		t.Errorf("a: %v", r)
	}
	if r := rows["down"]; len(r) < 8 || r[2] != "1" || r[6] != "failing" || !strings.HasSuffix(strings.Join(r, " "), ": server is down") {
		t.Errorf("down: %v", r)
	}
	if r := rows["off"]; len(r) < 5 || r[4] != "disabled" {
		t.Errorf("off: %v", r)
	}
	if r := rows["old"]; len(r) < 7 || r[1] != "-" || strings.Join(r[4:], " ") != "not in config" {
		t.Errorf("old: %v", r)
	}
	if !strings.Contains(out, "  onecheck  "+filepath.Join(e.dir, "events")+"  1 waiting\n") {
		t.Errorf("sources: %q", out)
	}
}

func TestStatusJSON(t *testing.T) {
	e := statusEnv(t)
	code, out, _ := execute("status", "-c", e.cfg, "--json")
	var got statusReport
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 {
		t.Fatalf("json: %v %q", err, out)
	}
	if len(got.Channels) != 4 || got.Channels[1].Name != "down" || got.Channels[1].State != "failing" ||
		got.Channels[1].Queued != 1 || got.Channels[1].LastError != "server is down" || got.Channels[1].Oldest == nil ||
		got.Channels[2].State != "disabled" || got.Channels[3].State != "not_in_config" ||
		len(got.Sources) != 1 || got.Sources[0].Waiting != 1 || !got.Sources[0].Exists {
		t.Errorf("json = %+v", got)
	}
}

func TestPaths(t *testing.T) {
	e := setup(t, "include: [conf.d/*.yaml]", chanA)
	if err := os.MkdirAll(filepath.Join(e.dir, "conf.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	inc := filepath.Join(e.dir, "conf.d", "more.yaml")
	if err := os.WriteFile(inc, []byte("channels:\n  - {name: more, module: fake}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := execute("paths", "-c", e.cfg)
	if code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"config    " + e.cfg + "  (from --config)\n",
		"host      web-01\n",
		"include   " + inc + "\n",
		"log       " + filepath.Join(e.dir, "log", "onenotify.log") + "  (missing)\n",
		"spool     " + filepath.Join(e.dir, "spool") + "  (missing)\n",
		"failed    " + filepath.Join(e.dir, "failed") + "  (missing)\n",
		"source    " + filepath.Join(e.dir, "events") + "\n",
		"modules   " + modDir + "\n",
		"binary    ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("paths lacks %q:\n%s", want, out)
		}
	}
	code, out, _ = execute("paths", "-c", e.cfg, "--json")
	var got pathsReport
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 {
		t.Fatalf("json: %v %q", err, out)
	}
	if got.Host != "web-01" || got.Config.Path != e.cfg || got.Config.Source != "--config" || len(got.Includes) != 1 ||
		got.Spool.Exists || len(got.Sources) != 1 || !got.Sources[0].Exists || !got.Modules.Exists {
		t.Errorf("json = %+v", got)
	}

	broken := setup(t, "nonsense: 1", chanA)
	code, out, errOut := execute("paths", "-c", broken.cfg)
	if code != 2 || !strings.Contains(out, "config    "+broken.cfg) || !strings.Contains(errOut, "nonsense") {
		t.Errorf("broken config: code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestLogrotate(t *testing.T) {
	e := setup(t, "queue: {failed_ttl: 1h}\nlog: {dir: log, rotate: {max_size: 1B, keep: 2}}", chanA)
	e.event(1)
	execute("-c", e.cfg)
	failed := filepath.Join(e.dir, "failed", "a")
	if err := os.MkdirAll(failed, 0o750); err != nil {
		t.Fatal(err)
	}
	old := `{"id":"x","failed_at":"` + time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339) + `"}`
	fresh := `{"id":"y","failed_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	for name, content := range map[string]string{"old.json": old, "fresh.json": fresh} {
		if err := os.WriteFile(filepath.Join(failed, name), []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	code, out, errOut := execute("logrotate", "-c", e.cfg)
	if code != 0 || !strings.Contains(out, "logs: rotated") || !strings.Contains(out, "failed: removed 1 old") {
		t.Errorf("code=%d out=%q err=%q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(failed, "old.json")); err == nil {
		t.Error("the old failed message is still there")
	}
	if _, err := os.Stat(filepath.Join(failed, "fresh.json")); err != nil {
		t.Error("the fresh failed message is gone")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "log", "onenotify.log.1.gz")); err != nil {
		t.Errorf("no rotated log: %v", err)
	}
	if code, out, _ := execute("logrotate", "-c", e.cfg, "--failed"); code != 0 || strings.Contains(out, "logs:") {
		t.Errorf("--failed: code=%d out=%q", code, out)
	}

	q, _ := queue.Open(filepath.Join(e.dir, "spool"), filepath.Join(e.dir, "failed"), e.cfg)
	unlock, err := q.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if code, out, _ := execute("logrotate", "-c", e.cfg, "--logs"); code != 0 || !strings.Contains(out, "skipped, a run is in progress") {
		t.Errorf("locked: code=%d out=%q", code, out)
	}
}

func TestVersionAndHelp(t *testing.T) {
	code, out, _ := execute("version")
	want := fmt.Sprintf("onenotify dev (protocol %d)\n", modules.MaxProtocol)
	if code != 0 || out != want {
		t.Errorf("version: code=%d out=%q", code, out)
	}
	for _, cmd := range []string{"run", "send", "validate", "status", "paths", "module", "self-update", "logrotate"} {
		code, out, _ := execute(cmd, "--help")
		if code != 0 || !strings.Contains(out, "Usage:") || !strings.Contains(out, "onenotify "+cmd) {
			t.Errorf("%s --help: code=%d out=%q", cmd, code, out)
		}
	}
	if code, out, _ := execute("--help"); code != 0 || !strings.Contains(out, "send") || !strings.Contains(out, "Exit codes") {
		t.Errorf("--help: code=%d", code)
	}
}

func TestDebugLogNeverShowsParams(t *testing.T) {
	e := setup(t, "", "  - {name: a, module: fake, params: {record: RECORD-a, token: s3cr3t-token}}\n")
	e.event(1)
	code, out, errOut := execute("-c", e.cfg, "--log-level", "debug")
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	log, _ := os.ReadFile(filepath.Join(e.dir, "log", "onenotify.log"))
	all := out + errOut + string(log)
	if strings.Contains(all, "s3cr3t-token") {
		t.Errorf("a channel secret was printed or logged:\n%s", all)
	}
	if !strings.Contains(errOut, "sending") || !strings.Contains(errOut, "params=\"[record token]\"") || !strings.Contains(errOut, "fakemod: sending") {
		t.Errorf("debug output lacks the request summary or the module's stderr:\n%s", errOut)
	}
	spool, _ := filepath.Glob(filepath.Join(e.dir, "spool", "*", "*"))
	for _, f := range spool {
		if data, _ := os.ReadFile(f); strings.Contains(string(data), "s3cr3t-token") {
			t.Errorf("%s holds a secret", f)
		}
	}
}

func TestSendRefusesASpoolOfAnotherConfig(t *testing.T) {
	e := setup(t, "", chanA)
	execute("-c", e.cfg) // claims the spool
	other := filepath.Join(e.dir, "other.yaml")
	data, _ := os.ReadFile(e.cfg)
	if err := os.WriteFile(other, data, 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := execute("send", "-c", other, "--title", "t", "--queue-only")
	if code != 2 || !strings.Contains(errOut, "already used by config") || e.queued("a") != 0 {
		t.Errorf("code=%d err=%q queued=%d", code, errOut, e.queued("a"))
	}
}
