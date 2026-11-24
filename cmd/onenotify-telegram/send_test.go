package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/alekzonder/onenotify/internal/schema"
	"github.com/alekzonder/onenotify/internal/version"
	"github.com/alekzonder/onenotify/pkg/module"
)

const testToken = "123456:SECRET-token_abc"

// fakeAPI is a Bot API server that records what it was sent and answers
// with a fixed status and body.
type fakeAPI struct {
	*httptest.Server
	status int
	reply  string

	mu     sync.Mutex
	calls  int
	method string
	path   string
	header http.Header
	raw    []byte
}

func newFakeAPI(t *testing.T, status int, reply string) *fakeAPI {
	t.Helper()
	f := &fakeAPI{status: status, reply: reply}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls++
		f.method, f.path, f.header, f.raw = r.Method, r.URL.Path, r.Header.Clone(), data
		f.mu.Unlock()
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, f.reply)
	}))
	t.Cleanup(f.Close)
	return f
}

func okAPI(t *testing.T) *fakeAPI {
	t.Helper()
	return newFakeAPI(t, http.StatusOK, `{"ok":true,"result":{"message_id":1}}`)
}

// body is the JSON body of the only request the server got.
func (f *fakeAPI) body(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls != 1 {
		t.Fatalf("server got %d requests, want 1", f.calls)
	}
	if !utf8.Valid(f.raw) {
		t.Fatalf("request body is not valid UTF-8")
	}
	var out map[string]any
	if err := json.Unmarshal(f.raw, &out); err != nil {
		t.Fatalf("request body is not valid JSON: %v\n%s", err, f.raw)
	}
	return out
}

func (f *fakeAPI) text(t *testing.T) string {
	t.Helper()
	text, ok := f.body(t)["text"].(string)
	if !ok {
		t.Fatalf("text is not a string")
	}
	return text
}

func baseParams(apiURL string) map[string]any {
	return map[string]any{"token": testToken, "chat_id": "-1001234567890", "api_url": apiURL}
}

func message(level, title, text string) module.Message {
	return module.Message{
		ID:        "20261123T100200Z-test",
		Source:    "send",
		Host:      "web-01",
		Level:     level,
		Title:     title,
		Text:      text,
		CreatedAt: time.Date(2026, 11, 23, 10, 2, 0, 0, time.UTC),
	}
}

// run sends a message the way onenotify does: through "send" with a JSON
// request on stdin, and returns the JSON response.
func run(t *testing.T, params map[string]any, msg module.Message, timeoutMS int64) module.Response {
	t.Helper()
	rawParams, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := json.Marshal(module.Request{
		Protocol:  module.ProtocolVersion,
		Channel:   "ops-telegram",
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
		t.Fatalf("invalid response %q: %v", stdout.String(), err)
	}
	if resp.Delivered != (resp.Error == nil) {
		t.Fatalf("inconsistent response: %s", stdout.String())
	}
	return resp
}

func deliver(t *testing.T, params map[string]any, msg module.Message) {
	t.Helper()
	if resp := run(t, params, msg, 10000); !resp.Delivered {
		t.Fatalf("not delivered: %+v", *resp.Error)
	}
}

// wantTemporary checks that the message was not delivered and that the
// error is a plain temporary one without the token in its text.
func wantTemporary(t *testing.T, resp module.Response) *module.Error {
	t.Helper()
	if resp.Error == nil {
		t.Fatalf("delivered, want an error")
	}
	if resp.Error.Permanent {
		t.Errorf("error is permanent, want temporary: %s", resp.Error.Message)
	}
	if resp.Error.RetryAfterMS != 0 {
		t.Errorf("retry_after_ms = %d, want none", resp.Error.RetryAfterMS)
	}
	if strings.Contains(resp.Error.Message, testToken) {
		t.Errorf("error text contains the token: %s", resp.Error.Message)
	}
	return resp.Error
}

// deliverRaw calls Send directly, so that the message reaches the module
// byte for byte: encoding a request as JSON would already replace invalid
// UTF-8.
func deliverRaw(t *testing.T, params map[string]any, msg module.Message) {
	t.Helper()
	rawParams, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	req := module.Request{Protocol: module.ProtocolVersion, Channel: "ops-telegram", Params: rawParams, Message: msg}
	if err := mod.Send(context.Background(), req); err != nil {
		t.Fatalf("not delivered: %v", err)
	}
}

func utf16Units(s string) int { return len(utf16.Encode([]rune(s))) }

func checkWithinLimit(t *testing.T, text string) {
	t.Helper()
	if !utf8.ValidString(text) {
		t.Errorf("text is not valid UTF-8")
	}
	if strings.ContainsRune(text, utf8.RuneError) {
		t.Errorf("text contains U+FFFD: a character was split")
	}
	if n := utf16Units(text); n > textLimit {
		t.Errorf("text is %d UTF-16 code units, limit is %d", n, textLimit)
	}
}

func TestBodyForEachLevel(t *testing.T) {
	levels := map[string]string{
		module.LevelError:   "🔴",
		module.LevelOK:      "🟢",
		module.LevelWarning: "🟡",
		module.LevelInfo:    "ℹ️",
		"something-new":     "ℹ️",
		"":                  "ℹ️",
	}
	for level, icon := range levels {
		t.Run("level="+level, func(t *testing.T) {
			api := okAPI(t)
			deliver(t, baseParams(api.URL), message(level, "[web-01] FAIL <main> & 🚀 a>b", "url: https://x/?a=1&b=2\n<b>not bold</b> 🔥\n\nlast > line"))
			want := icon + " <b>[web-01] FAIL &lt;main&gt; &amp; 🚀 a&gt;b</b>\n\n" +
				"url: https://x/?a=1&amp;b=2\n&lt;b&gt;not bold&lt;/b&gt; 🔥\n\nlast &gt; line"
			if got := api.text(t); got != want {
				t.Errorf("text:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestRequest(t *testing.T) {
	api := okAPI(t)
	deliver(t, baseParams(api.URL), message(module.LevelError, "title", "text"))
	body := api.body(t)
	if api.method != http.MethodPost {
		t.Errorf("method = %s, want POST", api.method)
	}
	if want := "/bot" + testToken + "/sendMessage"; api.path != want {
		t.Errorf("path = %s, want %s", api.path, want)
	}
	if got := api.header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got, want := api.header.Get("User-Agent"), "onenotify-telegram/"+version.Version; got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
	if body["parse_mode"] != "HTML" {
		t.Errorf("parse_mode = %v, want HTML", body["parse_mode"])
	}
	if body["chat_id"] != "-1001234567890" {
		t.Errorf("chat_id = %v", body["chat_id"])
	}
	preview, _ := body["link_preview_options"].(map[string]any)
	if preview["is_disabled"] != true {
		t.Errorf("link_preview_options = %v, want is_disabled: true", body["link_preview_options"])
	}
	for _, key := range []string{"message_thread_id", "disable_notification"} {
		if _, ok := body[key]; ok {
			t.Errorf("%s is set, want it absent", key)
		}
	}
}

func TestAPIURLTrailingSlash(t *testing.T) {
	api := okAPI(t)
	deliver(t, baseParams(api.URL+"/"), message(module.LevelInfo, "title", ""))
	api.body(t)
	if want := "/bot" + testToken + "/sendMessage"; api.path != want {
		t.Errorf("path = %s, want %s", api.path, want)
	}
}

func TestEmptyText(t *testing.T) {
	for _, text := range []string{"", "\n", "  \n\t\n"} {
		api := okAPI(t)
		deliver(t, baseParams(api.URL), message(module.LevelOK, "[web-01] OK main/api", text))
		if got, want := api.text(t), "🟢 <b>[web-01] OK main/api</b>"; got != want {
			t.Errorf("text %q: got %q, want %q", text, got, want)
		}
	}
}

func TestEmptyTitle(t *testing.T) {
	api := okAPI(t)
	deliver(t, baseParams(api.URL), message(module.LevelError, "", "only text"))
	if got, want := api.text(t), "🔴\n\nonly text"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSilentOK(t *testing.T) {
	cases := []struct {
		name   string
		silent any // nil: the param is not given
		level  string
		want   bool
	}{
		{"silent ok", true, module.LevelOK, true},
		{"silent error", true, module.LevelError, false},
		{"silent warning", true, module.LevelWarning, false},
		{"loud ok", false, module.LevelOK, false},
		{"loud error", false, module.LevelError, false},
		{"default ok", nil, module.LevelOK, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := okAPI(t)
			params := baseParams(api.URL)
			if tc.silent != nil {
				params["silent_ok"] = tc.silent
			}
			deliver(t, params, message(tc.level, "title", "text"))
			value, present := api.body(t)["disable_notification"]
			if tc.want && value != true {
				t.Errorf("disable_notification = %v, want true", value)
			}
			if !tc.want && present {
				t.Errorf("disable_notification = %v, want it absent", value)
			}
		})
	}
}

func TestMessageThreadID(t *testing.T) {
	api := okAPI(t)
	params := baseParams(api.URL)
	params["message_thread_id"] = 42
	deliver(t, params, message(module.LevelInfo, "title", "text"))
	if got := api.body(t)["message_thread_id"]; got != float64(42) {
		t.Errorf("message_thread_id = %v, want 42", got)
	}
}

func TestChatID(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string // as it must appear in the JSON body
	}{
		{"string", "-1001234567890", `"-1001234567890"`},
		{"username", "@ops_channel", `"@ops_channel"`},
		{"number", int64(-1001234567890), `-1001234567890`},
		{"positive number", 12345, `12345`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := okAPI(t)
			params := baseParams(api.URL)
			params["chat_id"] = tc.in
			deliver(t, params, message(module.LevelInfo, "title", "text"))
			api.body(t)
			var raw struct {
				ChatID json.RawMessage `json:"chat_id"`
			}
			if err := json.Unmarshal(api.raw, &raw); err != nil {
				t.Fatal(err)
			}
			if string(raw.ChatID) != tc.want {
				t.Errorf("chat_id = %s, want %s", raw.ChatID, tc.want)
			}
		})
	}
}

func TestLongTextIsCut(t *testing.T) {
	api := okAPI(t)
	deliver(t, baseParams(api.URL), message(module.LevelError, "title", strings.Repeat("abcdefghi\n", 1000)))
	text := api.text(t)
	checkWithinLimit(t, text)
	if !strings.HasPrefix(text, "🔴 <b>title</b>\n\nabcdefghi\n") {
		t.Errorf("unexpected start: %q", text[:40])
	}
	if !strings.HasSuffix(text, ellipsis) {
		t.Errorf("a cut text must end with %q, got %q", ellipsis, text[len(text)-20:])
	}
	if n := utf16Units(text); n < textLimit-10 {
		t.Errorf("text is %d UTF-16 code units: cut far below the limit of %d", n, textLimit)
	}
}

func TestTextAtTheLimitIsNotCut(t *testing.T) {
	head := "🔴 <b>title</b>\n\n"
	room := textLimit - utf16Units(head)
	for _, extra := range []int{-1, 0, 1} {
		api := okAPI(t)
		in := strings.Repeat("x", room+extra)
		deliver(t, baseParams(api.URL), message(module.LevelError, "title", in))
		text := api.text(t)
		checkWithinLimit(t, text)
		cut := strings.HasSuffix(text, ellipsis)
		if want := extra > 0; cut != want {
			t.Errorf("text of limit%+d: cut = %v, want %v", extra, cut, want)
		}
		if !cut && text != head+in {
			t.Errorf("text of limit%+d was changed", extra)
		}
		if cut && utf16Units(text) != textLimit {
			t.Errorf("text of limit%+d: %d units, want exactly %d", extra, utf16Units(text), textLimit)
		}
	}
}

func TestAmpersandsAreCutBetweenEntities(t *testing.T) {
	// Every offset of the text start relative to the limit, so that the cut
	// would fall on each byte of "&amp;" in turn.
	for pad := 0; pad < 6; pad++ {
		api := okAPI(t)
		title := "t" + strings.Repeat("x", pad)
		deliver(t, baseParams(api.URL), message(module.LevelWarning, title, strings.Repeat("&", 5000)))
		text := api.text(t)
		checkWithinLimit(t, text)
		head := "🟡 <b>" + title + "</b>\n\n"
		rest, ok := strings.CutPrefix(text, head)
		if !ok {
			t.Fatalf("unexpected start: %q", text[:40])
		}
		if !regexp.MustCompile(`^(&amp;)+…$`).MatchString(rest) {
			t.Errorf("pad %d: broken entity at the cut: %q", pad, rest[len(rest)-20:])
		}
		if n := utf16Units(text); n < textLimit-5 {
			t.Errorf("pad %d: %d UTF-16 code units, cut too early", pad, n)
		}
	}
}

func TestEmojiAreCutBetweenCharacters(t *testing.T) {
	for pad := 0; pad < 2; pad++ {
		api := okAPI(t)
		title := "t" + strings.Repeat("x", pad)
		deliver(t, baseParams(api.URL), message(module.LevelOK, title, strings.Repeat("😀", 5000)))
		text := api.text(t)
		checkWithinLimit(t, text)
		rest, ok := strings.CutPrefix(text, "🟢 <b>"+title+"</b>\n\n")
		if !ok {
			t.Fatalf("unexpected start: %q", text[:40])
		}
		if !regexp.MustCompile(`^(😀)+…$`).MatchString(rest) {
			t.Errorf("pad %d: broken character at the cut", pad)
		}
		if n := utf16Units(text); n < textLimit-2 {
			t.Errorf("pad %d: %d UTF-16 code units, cut too early", pad, n)
		}
	}
}

func TestLongTitleIsCut(t *testing.T) {
	for _, text := range []string{"", "some text"} {
		api := okAPI(t)
		deliver(t, baseParams(api.URL), message(module.LevelError, strings.Repeat("T<&", 2000), text))
		got := api.text(t)
		checkWithinLimit(t, got)
		if !regexp.MustCompile(`^🔴 <b>(T&lt;&amp;)+(T(&lt;)?)?…</b>$`).MatchString(got) {
			t.Errorf("unexpected text: %q ... %q", got[:40], got[len(got)-40:])
		}
	}
}

func TestLongTitleLeavesRoomForText(t *testing.T) {
	api := okAPI(t)
	deliver(t, baseParams(api.URL), message(module.LevelError, strings.Repeat("T", 4000), strings.Repeat("text ", 100)))
	got := api.text(t)
	checkWithinLimit(t, got)
	rest, ok := strings.CutPrefix(got, "🔴 <b>"+strings.Repeat("T", 4000)+"</b>\n\n")
	if !ok {
		t.Fatalf("the title was changed: %q ... %q", got[:20], got[len(got)-100:])
	}
	if !regexp.MustCompile(`^(text )+t?e?x?t?…$`).MatchString(rest) {
		t.Errorf("unexpected text: %q", rest)
	}
}

func TestControlCharactersAndInvalidUTF8(t *testing.T) {
	api := okAPI(t)
	title := "ti\x00tle\x1b[31m \xff\xfe bad\nsecond\tline"
	text := "one\r\ntwo\rthree\x00\x07\x7f\u0085 tab\there \xc3\x28 \xf0\x9f\x98 end"
	deliverRaw(t, baseParams(api.URL), message(module.LevelError, title, text))
	got := api.text(t)
	if !utf8.ValidString(got) {
		t.Fatalf("text is not valid UTF-8")
	}
	want := "🔴 <b>title[31m � bad second line</b>\n\none\ntwo\nthree tab\there �( � end"
	if got != want {
		t.Errorf("text:\n got %q\nwant %q", got, want)
	}
	for _, r := range got {
		if r != '\n' && r != '\t' && (r < 0x20 || (r >= 0x7f && r <= 0x9f)) {
			t.Errorf("control character %U in the text", r)
		}
	}
}

func TestInvalidUTF8InLongText(t *testing.T) {
	api := okAPI(t)
	deliverRaw(t, baseParams(api.URL), message(module.LevelError, "title", strings.Repeat("\xf0\x9f\x98😀\x00<", 3000)))
	got := api.text(t)
	if !utf8.ValidString(got) {
		t.Fatalf("text is not valid UTF-8")
	}
	if n := utf16Units(got); n > textLimit {
		t.Errorf("%d UTF-16 code units, limit is %d", n, textLimit)
	}
	rest, _ := strings.CutPrefix(got, "🔴 <b>title</b>\n\n")
	if !regexp.MustCompile(`^(\x{FFFD}😀&lt;)+(\x{FFFD}😀?)?…$`).MatchString(rest) {
		t.Errorf("broken cut: %q", rest[len(rest)-30:])
	}
}

func TestTooManyRequests(t *testing.T) {
	api := newFakeAPI(t, http.StatusTooManyRequests,
		`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`)
	resp := run(t, baseParams(api.URL), message(module.LevelError, "title", "text"), 10000)
	if resp.Delivered || resp.Error == nil {
		t.Fatalf("delivered, want an error")
	}
	if resp.Error.RetryAfterMS != 7000 {
		t.Errorf("retry_after_ms = %d, want 7000", resp.Error.RetryAfterMS)
	}
	if resp.Error.Permanent {
		t.Errorf("429 must not be permanent")
	}
	if want := "telegram: 429 Too Many Requests: retry after 7"; resp.Error.Message != want {
		t.Errorf("message = %q, want %q", resp.Error.Message, want)
	}
}

func TestTooManyRequestsWithoutParameters(t *testing.T) {
	api := newFakeAPI(t, http.StatusTooManyRequests, `rate limited`)
	resp := run(t, baseParams(api.URL), message(module.LevelError, "title", "text"), 10000)
	if e := wantTemporary(t, resp); e.Message != "telegram: 429 Too Many Requests" {
		t.Errorf("message = %q", e.Message)
	}
}

func TestRetryAfter(t *testing.T) {
	cases := []struct {
		seconds int64
		header  string
		want    time.Duration
	}{
		{7, "", 7 * time.Second},
		{7, "30", 7 * time.Second},
		{0, "30", 30 * time.Second},
		{0, "", 0},
		{-5, "soon", 0},
		{1 << 62, "", maxRetryAfter},
	}
	for _, tc := range cases {
		if got := retryAfter(tc.seconds, tc.header); got != tc.want {
			t.Errorf("retryAfter(%d, %q) = %v, want %v", tc.seconds, tc.header, got, tc.want)
		}
	}
}

func TestPermanentErrors(t *testing.T) {
	descriptions := []string{
		"Bad Request: message is too long",
		"Bad Request: MESSAGE IS TOO LONG",
		`Bad Request: can't parse entities: Unsupported start tag "x" at byte offset 5`,
		"Bad Request: message text is empty",
	}
	for _, desc := range descriptions {
		t.Run(desc, func(t *testing.T) {
			reply, _ := json.Marshal(map[string]any{"ok": false, "error_code": 400, "description": desc})
			api := newFakeAPI(t, http.StatusBadRequest, string(reply))
			resp := run(t, baseParams(api.URL), message(module.LevelError, "title", "text"), 10000)
			if resp.Error == nil {
				t.Fatalf("delivered, want an error")
			}
			if !resp.Error.Permanent {
				t.Errorf("error is temporary, want permanent: %s", resp.Error.Message)
			}
			if want := "telegram: 400 " + desc; resp.Error.Message != want {
				t.Errorf("message = %q, want %q", resp.Error.Message, want)
			}
		})
	}
}

func TestTemporaryErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reply  string
		want   string
	}{
		{"chat not found", 400, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`,
			"telegram: 400 Bad Request: chat not found"},
		{"thread not found", 400, `{"ok":false,"error_code":400,"description":"Bad Request: message thread not found"}`,
			"telegram: 400 Bad Request: message thread not found"},
		{"group migrated", 400, `{"ok":false,"error_code":400,"description":"Bad Request: group chat was upgraded to a supergroup chat","parameters":{"migrate_to_chat_id":-1001}}`,
			"telegram: 400 Bad Request: group chat was upgraded to a supergroup chat"},
		{"400 without JSON", 400, `message is too long`, "telegram: 400 Bad Request"},
		{"unauthorized", 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}`, "telegram: 401 Unauthorized"},
		{"forbidden", 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`,
			"telegram: 403 Forbidden: bot was blocked by the user"},
		{"too long but not 400", 403, `{"ok":false,"error_code":403,"description":"Forbidden: message is too long"}`,
			"telegram: 403 Forbidden: message is too long"},
		{"not found", 404, `{"ok":false,"error_code":404,"description":"Not Found"}`, "telegram: 404 Not Found"},
		{"internal error", 500, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`,
			"telegram: 500 Internal Server Error"},
		{"bad gateway, not JSON", 502, "<html><body>502 Bad Gateway</body></html>", "telegram: 502 Bad Gateway"},
		{"200, not JSON", 200, "<html>hello</html>", "telegram: unexpected reply: 200 OK, but not a Bot API success"},
		{"200, ok false", 200, `{"ok":false}`, "telegram: unexpected reply: 200 OK, but not a Bot API success"},
		{"200, empty", 200, ``, "telegram: unexpected reply: 200 OK, but not a Bot API success"},
		{"description repeats the token", 401, `{"ok":false,"description":"Unauthorized: ` + testToken + `"}`,
			"telegram: 401 Unauthorized: <token>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newFakeAPI(t, tc.status, tc.reply)
			resp := run(t, baseParams(api.URL), message(module.LevelError, "title", "text"), 10000)
			if e := wantTemporary(t, resp); e.Message != tc.want {
				t.Errorf("message = %q, want %q", e.Message, tc.want)
			}
		})
	}
}

func TestConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	apiURL := srv.URL
	srv.Close()
	resp := run(t, baseParams(apiURL), message(module.LevelError, "title", "text"), 10000)
	e := wantTemporary(t, resp)
	if !strings.HasPrefix(e.Message, "telegram: request failed: ") || !strings.Contains(e.Message, "connection refused") {
		t.Errorf("message = %q", e.Message)
	}
	if strings.Contains(e.Message, "/bot") || strings.Contains(e.Message, "sendMessage") {
		t.Errorf("error text contains the request URL: %s", e.Message)
	}
}

func TestInvalidAPIURL(t *testing.T) {
	for _, apiURL := range []string{"http://exa mple.com", "http://[::1", "ftp://example.com", "example.com"} {
		resp := run(t, baseParams(apiURL), message(module.LevelError, "title", "text"), 10000)
		wantTemporary(t, resp)
	}
}

func TestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	defer srv.Close()
	defer close(release)

	start := time.Now()
	resp := run(t, baseParams(srv.URL), message(module.LevelError, "title", "text"), 200)
	elapsed := time.Since(start)
	e := wantTemporary(t, resp)
	if !strings.Contains(e.Message, "deadline exceeded") {
		t.Errorf("message = %q, want a deadline error", e.Message)
	}
	if elapsed > 10*time.Second {
		t.Errorf("returned after %v, want promptly after 200ms", elapsed)
	}
}

func TestSendInvalidParams(t *testing.T) {
	api := okAPI(t)
	cases := map[string]string{
		"unknown key":     `{"token":"` + testToken + `","chat_id":"1","api_url":"` + api.URL + `","colour":"red"}`,
		"no token":        `{"chat_id":"1","api_url":"` + api.URL + `"}`,
		"no chat_id":      `{"token":"` + testToken + `","api_url":"` + api.URL + `"}`,
		"null chat_id":    `{"token":"` + testToken + `","chat_id":null,"api_url":"` + api.URL + `"}`,
		"chat_id object":  `{"token":"` + testToken + `","chat_id":{},"api_url":"` + api.URL + `"}`,
		"chat_id boolean": `{"token":"` + testToken + `","chat_id":true,"api_url":"` + api.URL + `"}`,
		"token number":    `{"token":123,"chat_id":"1","api_url":"` + api.URL + `"}`,
		"no params":       ``,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			err := mod.Send(context.Background(), module.Request{
				Protocol: module.ProtocolVersion,
				Params:   json.RawMessage(raw),
				Message:  message(module.LevelError, "title", "text"),
			})
			if err == nil {
				t.Fatalf("no error for invalid params")
			}
			if !strings.Contains(err.Error(), "invalid params") {
				t.Errorf("error = %q, want it to mention invalid params", err)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("error text contains the token: %s", err)
			}
		})
	}
	if api.calls != 0 {
		t.Errorf("server got %d requests, want none", api.calls)
	}
}

func TestDescribe(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := module.Run(mod, []string{"describe"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	var d module.Description
	if err := json.Unmarshal(stdout.Bytes(), &d); err != nil {
		t.Fatalf("invalid description: %v", err)
	}
	if d.Protocol != module.ProtocolVersion || d.Name != "telegram" || d.Version != version.Version || d.Description == "" {
		t.Errorf("unexpected description: %+v", d)
	}
}

func TestDescriptionSchema(t *testing.T) {
	s, err := schema.Compile(mod.Description.ParamsSchema)
	if err != nil {
		t.Fatalf("schema does not compile: %v", err)
	}
	if s == nil {
		t.Fatalf("module has no params schema")
	}
	valid := []string{
		`{"token":"123:abc","chat_id":"-1001234567890"}`,
		`{"token":"123:abc","chat_id":-1001234567890}`,
		`{"token":"123:abc","chat_id":"@ops","message_thread_id":42,"silent_ok":true,"api_url":"http://127.0.0.1:8081"}`,
	}
	for _, params := range valid {
		if err := s.Validate(json.RawMessage(params)); err != nil {
			t.Errorf("valid params %s rejected: %v", params, err)
		}
	}
	invalid := map[string]string{
		"unknown key":              `{"token":"123:abc","chat_id":"1","colour":"red"}`,
		"missing token":            `{"chat_id":"1"}`,
		"missing chat_id":          `{"token":"123:abc"}`,
		"empty token":              `{"token":"","chat_id":"1"}`,
		"token is a number":        `{"token":123,"chat_id":"1"}`,
		"chat_id is a boolean":     `{"token":"123:abc","chat_id":true}`,
		"chat_id is a fraction":    `{"token":"123:abc","chat_id":1.5}`,
		"thread id is a string":    `{"token":"123:abc","chat_id":"1","message_thread_id":"42"}`,
		"silent_ok is a string":    `{"token":"123:abc","chat_id":"1","silent_ok":"yes"}`,
		"api_url without a scheme": `{"token":"123:abc","chat_id":"1","api_url":"api.telegram.org"}`,
		"no params":                `{}`,
	}
	for name, params := range invalid {
		if err := s.Validate(json.RawMessage(params)); err == nil {
			t.Errorf("%s: params %s accepted", name, params)
		}
	}
}
