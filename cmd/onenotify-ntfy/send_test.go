package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/alekzonder/onenotify/internal/schema"
	"github.com/alekzonder/onenotify/internal/version"
	"github.com/alekzonder/onenotify/pkg/module"
)

const (
	testToken    = "tk_s3cr3tT0kenValue"
	testPassword = "p4ssw0rd-Secret"
)

// received is one request seen by the fake ntfy server.
type received struct {
	method string
	path   string
	header http.Header
	raw    []byte
}

// body decodes the request body the way ntfy would.
func (r received) body(t *testing.T) publication {
	t.Helper()
	if !utf8.Valid(r.raw) {
		t.Fatalf("request body is not valid UTF-8: %q", r.raw)
	}
	var p publication
	if err := json.Unmarshal(r.raw, &p); err != nil {
		t.Fatalf("request body is not valid JSON: %v\n%s", err, r.raw)
	}
	return p
}

// fakeNtfy is a server that records requests and answers with a fixed reply.
type fakeNtfy struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []received
}

func newFakeNtfy(t *testing.T, status int, header map[string]string, answer string) *fakeNtfy {
	t.Helper()
	f := &fakeNtfy{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, received{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), raw: raw})
		f.mu.Unlock()
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(f.Close)
	return f
}

func newOKNtfy(t *testing.T) *fakeNtfy {
	t.Helper()
	return newFakeNtfy(t, http.StatusOK, nil, `{"id":"abc","event":"message"}`)
}

// only returns the single request the server must have received.
func (f *fakeNtfy) only(t *testing.T) received {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) != 1 {
		t.Fatalf("server got %d requests, want 1", len(f.reqs))
	}
	return f.reqs[0]
}

func testMessage(level string) module.Message {
	return module.Message{
		ID:        "20261123T100200Z-main-api-health-fail",
		Source:    "onecheck",
		Host:      "web-01",
		Level:     level,
		Title:     "[web-01] FAIL main/api-health",
		Text:      "module: http\nurl: https://example.com/health",
		CreatedAt: time.Date(2026, 11, 23, 10, 2, 0, 0, time.UTC),
	}
}

// run sends msg through the module the way onenotify does and returns the
// module's JSON response.
func run(t *testing.T, p map[string]any, msg module.Message, timeoutMS int64) module.Response {
	t.Helper()
	rawParams, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := json.Marshal(module.Request{
		Protocol:  module.ProtocolVersion,
		Channel:   "ops-ntfy",
		Params:    rawParams,
		Message:   msg,
		TimeoutMS: timeoutMS,
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := module.Run(mod, []string{"send"}, bytes.NewReader(stdin), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	var resp module.Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, stdout.String())
	}
	return resp
}

func mustDeliver(t *testing.T, p map[string]any, msg module.Message) {
	t.Helper()
	resp := run(t, p, msg, 5000)
	if !resp.Delivered || resp.Error != nil {
		t.Fatalf("not delivered: %+v", resp.Error)
	}
}

// mustFail returns the error of a message that must not be delivered.
func mustFail(t *testing.T, p map[string]any, msg module.Message) *module.Error {
	t.Helper()
	resp := run(t, p, msg, 5000)
	if resp.Delivered || resp.Error == nil {
		t.Fatalf("delivered=%v error=%v, want an error", resp.Delivered, resp.Error)
	}
	return resp.Error
}

func mustBeTemporary(t *testing.T, e *module.Error) {
	t.Helper()
	if e.Permanent || e.RetryAfterMS != 0 {
		t.Fatalf("error %+v, want temporary without retry_after", e)
	}
}

func mustHaveNoSecrets(t *testing.T, text string) {
	t.Helper()
	for _, secret := range []string{testToken, testPassword} {
		if strings.Contains(text, secret) {
			t.Fatalf("error text contains a secret: %q", text)
		}
	}
}

func TestSendBodyByLevel(t *testing.T) {
	cases := []struct {
		level    string
		priority int
		tag      string
	}{
		{module.LevelError, 4, "rotating_light"},
		{module.LevelWarning, 3, "warning"},
		{module.LevelOK, 3, "white_check_mark"},
		{module.LevelInfo, 2, "information_source"},
	}
	for _, tc := range cases {
		t.Run(tc.level, func(t *testing.T) {
			srv := newOKNtfy(t)
			msg := testMessage(tc.level)
			mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts"}, msg)

			req := srv.only(t)
			if req.method != http.MethodPost || req.path != "/" {
				t.Errorf("request %s %s, want POST /", req.method, req.path)
			}
			if got := req.header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type %q", got)
			}
			if got, want := req.header.Get("User-Agent"), "onenotify-ntfy/"+version.Version; got != want {
				t.Errorf("User-Agent %q, want %q", got, want)
			}
			want := publication{Topic: "alerts", Title: msg.Title, Message: msg.Text, Priority: tc.priority, Tags: []string{tc.tag}}
			if got := req.body(t); !reflect.DeepEqual(got, want) {
				t.Errorf("body\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestSendBodyKeys(t *testing.T) {
	srv := newOKNtfy(t)
	mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts"}, testMessage(module.LevelError))
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(srv.only(t).raw, &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"topic", "title", "message", "priority", "tags"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("body has no key %q", k)
		}
	}
	if len(keys) != 5 {
		t.Errorf("body has %d keys, want 5", len(keys))
	}
}

func TestSendUnknownLevel(t *testing.T) {
	srv := newOKNtfy(t)
	mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts"}, testMessage("critical"))
	got := srv.only(t).body(t)
	if got.Priority != 3 || len(got.Tags) != 0 {
		t.Errorf("priority %d tags %v, want 3 and no tags", got.Priority, got.Tags)
	}
}

func TestSendMessageTags(t *testing.T) {
	srv := newOKNtfy(t)
	msg := testMessage(module.LevelError)
	msg.Tags = []string{"api", "", "  ", "prod,eu", "api", "rotating_light"}
	mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts"}, msg)
	want := []string{"rotating_light", "api", "prod eu"}
	if got := srv.only(t).body(t).Tags; !reflect.DeepEqual(got, want) {
		t.Errorf("tags %q, want %q", got, want)
	}
}

func TestSendPriorityParam(t *testing.T) {
	for _, level := range []string{module.LevelError, module.LevelInfo} {
		srv := newOKNtfy(t)
		mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts", "priority": 5}, testMessage(level))
		if got := srv.only(t).body(t).Priority; got != 5 {
			t.Errorf("level %s: priority %d, want 5", level, got)
		}
	}
}

func TestSendAuth(t *testing.T) {
	t.Run("token", func(t *testing.T) {
		srv := newOKNtfy(t)
		mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts", "token": testToken}, testMessage(module.LevelOK))
		if got := srv.only(t).header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("Authorization %q", got)
		}
	})
	t.Run("basic", func(t *testing.T) {
		srv := newOKNtfy(t)
		mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts", "username": "monitor", "password": testPassword}, testMessage(module.LevelOK))
		r := http.Request{Header: srv.only(t).header}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "monitor" || pass != testPassword {
			t.Errorf("basic auth %q %q %v", user, pass, ok)
		}
	})
	t.Run("none", func(t *testing.T) {
		srv := newOKNtfy(t)
		mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts"}, testMessage(module.LevelOK))
		if got, ok := srv.only(t).header["Authorization"]; ok {
			t.Errorf("unexpected Authorization %q", got)
		}
	})
	t.Run("userinfo in url", func(t *testing.T) {
		srv := newOKNtfy(t)
		u := strings.Replace(srv.URL, "http://", "http://monitor:"+testPassword+"@", 1)
		mustDeliver(t, map[string]any{"url": u, "topic": "alerts"}, testMessage(module.LevelOK))
		r := http.Request{Header: srv.only(t).header}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "monitor" || pass != testPassword {
			t.Errorf("basic auth %q %q %v", user, pass, ok)
		}
	})
}

func TestSendCyrillic(t *testing.T) {
	srv := newOKNtfy(t)
	msg := testMessage(module.LevelError)
	msg.Title = "[веб-01] СБОЙ главная/здоровье"
	msg.Text = "модуль: http\nответ: 503 — сервис недоступен 🔥 <b>&</b>"
	mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts"}, msg)
	req := srv.only(t)
	got := req.body(t)
	if got.Title != msg.Title || got.Message != msg.Text {
		t.Errorf("title %q message %q", got.Title, got.Message)
	}
	if !bytes.Contains(req.raw, []byte("веб-01")) || !bytes.Contains(req.raw, []byte("<b>&</b>")) {
		t.Errorf("text is escaped in the body: %s", req.raw)
	}
}

func TestSendLongTextIsCut(t *testing.T) {
	srv := newOKNtfy(t)
	msg := testMessage(module.LevelError)
	msg.Text = strings.Repeat("я", 2500) // 5000 bytes
	mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts"}, msg)
	got := srv.only(t).body(t).Message
	if len(got) > maxMessageBytes || len(got) < maxMessageBytes-4 {
		t.Errorf("message is %d bytes, want just under %d", len(got), maxMessageBytes)
	}
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Errorf("message is not valid UTF-8 ending with …: %q", got[len(got)-10:])
	}
	if strings.Trim(strings.TrimSuffix(got, "…"), "я") != "" {
		t.Error("message has a broken character")
	}
}

func TestSendTextAtLimitIsNotCut(t *testing.T) {
	srv := newOKNtfy(t)
	msg := testMessage(module.LevelError)
	msg.Text = strings.Repeat("я", maxMessageBytes/2)
	mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts"}, msg)
	if got := srv.only(t).body(t).Message; got != msg.Text {
		t.Errorf("a text of exactly %d bytes was changed (%d bytes)", maxMessageBytes, len(got))
	}
}

func TestSendControlCharsAndInvalidUTF8(t *testing.T) {
	srv := newOKNtfy(t)
	msg := testMessage(module.LevelWarning)
	msg.Title = "[web-01] bad\x00\x1b[31m\xff title\nsecond"
	msg.Text = "bell\a esc\x1b[0m nul\x00 tab\t quote\" back\\ half\xd0 bad\xff\xfe end <&> 😀"
	msg.Tags = []string{"t\xffag\n"}
	// Called directly: a request encoded as JSON cannot carry invalid UTF-8.
	err := send(context.Background(), module.Request{
		Params:  json.RawMessage(`{"url":"` + srv.URL + `","topic":"alerts"}`),
		Message: msg,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := srv.only(t).body(t)
	if got.Title != "[web-01] bad  [31m title second" {
		t.Errorf("title %q", got.Title)
	}
	if want := "bell\a esc\x1b[0m nul\x00 tab\t quote\" back\\ half bad end <&> 😀"; got.Message != want {
		t.Errorf("message\n got %q\nwant %q", got.Message, want)
	}
	if !reflect.DeepEqual(got.Tags, []string{"warning", "tag"}) {
		t.Errorf("tags %q", got.Tags)
	}
}

// A text that grows a lot when escaped in JSON must still fit ntfy's limit
// for the request body.
func TestSendEscapedTextFitsBody(t *testing.T) {
	for _, ch := range []string{"\x01", "\"", "\n"} {
		srv := newOKNtfy(t)
		msg := testMessage(module.LevelError)
		msg.Text = strings.Repeat(ch, 5000) + "x"
		mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts"}, msg)
		req := srv.only(t)
		if len(req.raw) > 8192 || len(req.raw) < 7900 {
			t.Errorf("%q: body is %d bytes", ch, len(req.raw))
		}
		got := req.body(t).Message
		if !strings.HasSuffix(got, "…") || len(got) < 1000 {
			t.Errorf("%q: message of %d bytes, want a long one ending with …", ch, len(got))
		}
	}
}

// ntfy shows "triggered" for an empty message, so the title is repeated as
// the message and kept as the title.
func TestSendEmptyText(t *testing.T) {
	for _, text := range []string{"", " \n\t "} {
		srv := newOKNtfy(t)
		msg := testMessage(module.LevelOK)
		msg.Title = "[web-01] OK main/api-health"
		msg.Text = text
		mustDeliver(t, map[string]any{"url": srv.URL, "topic": "alerts"}, msg)
		got := srv.only(t).body(t)
		if got.Title != msg.Title || got.Message != msg.Title {
			t.Errorf("text %q: title %q message %q, want both %q", text, got.Title, got.Message, msg.Title)
		}
	}
}

func TestSendURLForms(t *testing.T) {
	for _, suffix := range []string{"", "/", "//"} {
		srv := newOKNtfy(t)
		mustDeliver(t, map[string]any{"url": srv.URL + suffix, "topic": "alerts"}, testMessage(module.LevelOK))
		if got := srv.only(t).path; got != "/" {
			t.Errorf("url with suffix %q: path %q, want /", suffix, got)
		}
	}
}

func TestSendRateLimit(t *testing.T) {
	t.Run("with Retry-After", func(t *testing.T) {
		srv := newFakeNtfy(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "30"},
			`{"code":42901,"http":429,"error":"limit reached: too many requests"}`)
		e := mustFail(t, map[string]any{"url": srv.URL, "topic": "alerts"}, testMessage(module.LevelError))
		if e.Permanent || e.RetryAfterMS != 30000 {
			t.Errorf("error %+v, want temporary with retry_after_ms 30000", e)
		}
		if want := "ntfy: 429 Too Many Requests: limit reached: too many requests"; e.Message != want {
			t.Errorf("message %q, want %q", e.Message, want)
		}
	})
	t.Run("without Retry-After", func(t *testing.T) {
		srv := newFakeNtfy(t, http.StatusTooManyRequests, nil, "")
		e := mustFail(t, map[string]any{"url": srv.URL, "topic": "alerts"}, testMessage(module.LevelError))
		mustBeTemporary(t, e)
		if e.Message != "ntfy: 429 Too Many Requests" {
			t.Errorf("message %q", e.Message)
		}
	})
	t.Run("Retry-After as a date", func(t *testing.T) {
		srv := newFakeNtfy(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "Wed, 21 Oct 2026 07:28:00 GMT"}, "")
		mustBeTemporary(t, mustFail(t, map[string]any{"url": srv.URL, "topic": "alerts"}, testMessage(module.LevelError)))
	})
}

func TestSendTooLargeIsPermanent(t *testing.T) {
	srv := newFakeNtfy(t, http.StatusRequestEntityTooLarge, nil, `{"code":41301,"http":413,"error":"attachment too large"}`)
	e := mustFail(t, map[string]any{"url": srv.URL, "topic": "alerts"}, testMessage(module.LevelError))
	if !e.Permanent || e.RetryAfterMS != 0 {
		t.Errorf("error %+v, want permanent", e)
	}
	if !strings.HasPrefix(e.Message, "ntfy: 413 ") {
		t.Errorf("message %q", e.Message)
	}
}

func TestSendTemporaryStatuses(t *testing.T) {
	cases := []struct {
		status int
		answer string
		want   string
	}{
		{400, `{"code":40009,"http":400,"error":"invalid request: topic invalid"}`, "ntfy: 400 Bad Request: invalid request: topic invalid"},
		{401, `{"code":40101,"http":401,"error":"unauthorized"}`, "ntfy: 401 Unauthorized: unauthorized"},
		{403, `{"code":40301,"http":403,"error":"forbidden"}`, "ntfy: 403 Forbidden: forbidden"},
		{404, `<html>not found</html>`, "ntfy: 404 Not Found"},
		{500, `{"code":50001,"http":500,"error":"internal server error"}`, "ntfy: 500 Internal Server Error: internal server error"},
		{502, ``, "ntfy: 502 Bad Gateway"},
		{301, ``, "ntfy: 301 Moved Permanently: unexpected redirect"},
	}
	for _, tc := range cases {
		srv := newFakeNtfy(t, tc.status, nil, tc.answer)
		e := mustFail(t, map[string]any{"url": srv.URL, "topic": "alerts", "token": testToken}, testMessage(module.LevelError))
		mustBeTemporary(t, e)
		if e.Message != tc.want {
			t.Errorf("status %d: message %q, want %q", tc.status, e.Message, tc.want)
		}
		mustHaveNoSecrets(t, e.Message)
	}
}

// A redirect is not followed: the POST would turn into a GET of the web
// page, whose 200 is not a delivery.
func TestSendRedirectIsNotFollowed(t *testing.T) {
	dst := newOKNtfy(t)
	src := newFakeNtfy(t, http.StatusFound, map[string]string{"Location": dst.URL + "/"}, "")
	e := mustFail(t, map[string]any{"url": src.URL, "topic": "alerts"}, testMessage(module.LevelError))
	mustBeTemporary(t, e)
	if !strings.Contains(e.Message, "redirect to "+dst.URL+"/") {
		t.Errorf("message %q", e.Message)
	}
	dst.mu.Lock()
	defer dst.mu.Unlock()
	if len(dst.reqs) != 0 {
		t.Errorf("redirect was followed")
	}
}

func TestSendLongServerErrorIsLimited(t *testing.T) {
	answer, _ := json.Marshal(map[string]string{"error": strings.Repeat("ошибка\n", 500)})
	srv := newFakeNtfy(t, http.StatusBadRequest, nil, string(answer))
	e := mustFail(t, map[string]any{"url": srv.URL, "topic": "alerts"}, testMessage(module.LevelError))
	if len(e.Message) > 300 || strings.ContainsAny(e.Message, "\n\r") || !utf8.ValidString(e.Message) {
		t.Errorf("message of %d bytes: %q", len(e.Message), e.Message)
	}
}

// A server that repeats the credentials in its answer must not get them
// into the error text.
func TestSendServerEchoingSecrets(t *testing.T) {
	answer := `{"error":"bad credentials ` + testToken + ` / ` + testPassword + `"}`
	for _, p := range []map[string]any{
		{"token": testToken},
		{"username": "monitor", "password": testPassword},
	} {
		srv := newFakeNtfy(t, http.StatusUnauthorized, nil, answer)
		p["url"], p["topic"] = srv.URL, "alerts"
		e := mustFail(t, p, testMessage(module.LevelError))
		mustBeTemporary(t, e)
		if _, withToken := p["token"]; withToken && strings.Contains(e.Message, testToken) {
			t.Errorf("token in %q", e.Message)
		}
		if _, withPassword := p["password"]; withPassword && strings.Contains(e.Message, testPassword) {
			t.Errorf("password in %q", e.Message)
		}
		if !strings.Contains(e.Message, redacted) {
			t.Errorf("nothing redacted in %q", e.Message)
		}
	}
}

func TestSendConnectionRefused(t *testing.T) {
	srv := newOKNtfy(t)
	addr := srv.URL
	srv.Close()
	userinfo := strings.Replace(addr, "http://", "http://monitor:"+testPassword+"@", 1)
	for _, p := range []map[string]any{
		{"url": addr, "topic": "alerts", "token": testToken},
		{"url": addr, "topic": "alerts", "username": "monitor", "password": testPassword},
		{"url": userinfo, "topic": "alerts"},
		{"url": userinfo, "topic": "alerts", "token": testToken},
	} {
		e := mustFail(t, p, testMessage(module.LevelError))
		mustBeTemporary(t, e)
		mustHaveNoSecrets(t, e.Message)
		if strings.Contains(e.Message, "monitor") {
			t.Errorf("user name from the url in %q", e.Message)
		}
		if !strings.HasPrefix(e.Message, "ntfy: ") || !strings.Contains(e.Message, strings.TrimPrefix(addr, "http://")) {
			t.Errorf("message %q does not say what failed", e.Message)
		}
	}
}

func TestSendTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	start := time.Now()
	resp := run(t, map[string]any{"url": srv.URL, "topic": "alerts", "token": testToken}, testMessage(module.LevelError), 150)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("send took %s with timeout_ms 150", elapsed)
	}
	if resp.Delivered || resp.Error == nil {
		t.Fatalf("response %+v, want an error", resp)
	}
	mustBeTemporary(t, resp.Error)
	mustHaveNoSecrets(t, resp.Error.Message)
	if !strings.Contains(resp.Error.Message, "deadline exceeded") {
		t.Errorf("message %q", resp.Error.Message)
	}
}

func TestSendInvalidParams(t *testing.T) {
	srv := newOKNtfy(t)
	badURL := "http://monitor:" + testPassword + "@bad host/"
	cases := map[string]map[string]any{
		"unknown key":               {"url": srv.URL, "topic": "alerts", "chat_id": 1},
		"no topic":                  {"url": srv.URL},
		"priority out of range":     {"url": srv.URL, "topic": "alerts", "priority": 9},
		"token with username":       {"url": srv.URL, "topic": "alerts", "token": testToken, "username": "u", "password": testPassword},
		"username without password": {"url": srv.URL, "topic": "alerts", "username": "u"},
		"password without username": {"url": srv.URL, "topic": "alerts", "password": testPassword},
		"ftp url":                   {"url": "ftp://example.com", "topic": "alerts"},
		"unparsable url":            {"url": badURL, "topic": "alerts"},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			e := mustFail(t, p, testMessage(module.LevelError))
			mustBeTemporary(t, e)
			mustHaveNoSecrets(t, e.Message)
		})
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.reqs) != 0 {
		t.Errorf("server got %d requests for invalid params", len(srv.reqs))
	}
}

func TestDescribe(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := module.Run(mod, []string{"describe"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	var d module.Description
	if err := json.Unmarshal(stdout.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Protocol != module.ProtocolVersion || d.Name != "ntfy" || d.Version != version.Version || d.Description == "" {
		t.Errorf("description %+v", d)
	}
}

func TestDescriptionSchema(t *testing.T) {
	s, err := schema.Compile(mod.Description.ParamsSchema)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("module has no params schema")
	}
	valid := []string{
		`{"topic":"alerts"}`,
		`{"topic":"alerts","token":"tk_abc"}`,
		`{"topic":"alerts","username":"u","password":"p"}`,
		`{"topic":"alerts","url":"http://ntfy.example.com:8080/","priority":1}`,
		`{"topic":"alerts","url":"https://ntfy.example.com","priority":5,"token":"tk_abc"}`,
	}
	for _, p := range valid {
		if err := s.Validate(json.RawMessage(p)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	invalid := []string{
		`{}`,
		`{"topic":"alerts","chat_id":1}`,
		`{"url":"https://ntfy.sh"}`,
		`{"topic":""}`,
		`{"topic":"alerts","token":"tk_abc","username":"u","password":"p"}`,
		`{"topic":"alerts","token":"tk_abc","username":"u"}`,
		`{"topic":"alerts","token":"tk_abc","password":"p"}`,
		`{"topic":"alerts","username":"u"}`,
		`{"topic":"alerts","password":"p"}`,
		`{"topic":"alerts","token":""}`,
		`{"topic":"alerts","priority":9}`,
		`{"topic":"alerts","priority":0}`,
		`{"topic":"alerts","priority":2.5}`,
		`{"topic":"alerts","priority":"high"}`,
		`{"topic":"alerts","url":"ftp://x"}`,
		`{"topic":"alerts","url":"ntfy.sh"}`,
	}
	for _, p := range invalid {
		if err := s.Validate(json.RawMessage(p)); err == nil {
			t.Errorf("%s: accepted", p)
		}
	}
}
