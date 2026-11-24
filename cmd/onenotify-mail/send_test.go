package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alekzonder/onenotify/internal/schema"
	"github.com/alekzonder/onenotify/pkg/module"
)

const testPassword = "s3cret-Pa55word"

func testMessage() module.Message {
	return module.Message{
		ID:        "20261123T100200Z-main-api-health-fail",
		Source:    "onecheck",
		Host:      "web-01",
		Level:     module.LevelError,
		Title:     "[web-01] FAIL main/api-health",
		Text:      "module: http\nurl: https://example.com/health\n",
		CreatedAt: time.Date(2026, 11, 23, 10, 2, 0, 0, time.UTC),
	}
}

// testParams are minimal params for srv with "tls: none".
func testParams(srv *fakeSMTP) map[string]any {
	return map[string]any{
		"host": "127.0.0.1",
		"port": srv.port(),
		"tls":  "none",
		"from": "alerts@example.com",
		"to":   []string{"ops@example.org"},
		"helo": "client.example.com",
	}
}

func with(params map[string]any, kv ...any) map[string]any {
	for i := 0; i < len(kv); i += 2 {
		params[kv[i].(string)] = kv[i+1]
	}
	return params
}

// runSend delivers msg through module.Run, the way onenotify does.
func runSend(t *testing.T, params map[string]any, msg module.Message, timeoutMS int64) module.Response {
	t.Helper()
	rawParams, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := json.Marshal(module.Request{
		Protocol:  module.ProtocolVersion,
		Channel:   "ops-mail",
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
		t.Fatalf("response %q: %v", stdout.String(), err)
	}
	if resp.Delivered == (resp.Error != nil) {
		t.Fatalf("inconsistent response: %s", stdout.String())
	}
	if resp.Error != nil && strings.Contains(resp.Error.Message, testPassword) {
		t.Fatalf("the error text contains the password: %s", resp.Error.Message)
	}
	return resp
}

func mustDeliver(t *testing.T, params map[string]any, msg module.Message) {
	t.Helper()
	if resp := runSend(t, params, msg, 10000); !resp.Delivered {
		t.Fatalf("not delivered: %+v", *resp.Error)
	}
}

// mustFail checks that the delivery fails with a temporary error and returns
// its text.
func mustFail(t *testing.T, params map[string]any, msg module.Message) string {
	t.Helper()
	resp := runSend(t, params, msg, 10000)
	if resp.Delivered {
		t.Fatal("delivered, want an error")
	}
	if resp.Error.Permanent {
		t.Fatalf("permanent error, want a temporary one: %s", resp.Error.Message)
	}
	if resp.Error.RetryAfterMS != 0 {
		t.Fatalf("retry_after_ms = %d, want none", resp.Error.RetryAfterMS)
	}
	return resp.Error.Message
}

func parseData(t *testing.T, data string) *mail.Message {
	t.Helper()
	m, err := mail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatalf("recorded DATA is not a message: %v\n%s", err, data)
	}
	return m
}

func decodeHeader(t *testing.T, m *mail.Message, name string) string {
	t.Helper()
	s, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get(name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return s
}

// decodeBody returns the body decoded from quoted-printable, with the line
// endings as LF.
func decodeBody(t *testing.T, m *mail.Message) string {
	t.Helper()
	b, err := io.ReadAll(quotedprintable.NewReader(m.Body))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// checkWire checks what every message must satisfy on the wire: 7-bit, CRLF
// only, no line longer than 998 characters.
func checkWire(t *testing.T, data string) {
	t.Helper()
	for i := 0; i < len(data); i++ {
		if data[i] >= 0x80 {
			t.Fatalf("byte %d is not ASCII", i)
		}
	}
	if strings.Contains(strings.ReplaceAll(data, "\r\n", ""), "\n") || strings.Contains(strings.ReplaceAll(data, "\r\n", ""), "\r") {
		t.Fatal("the message has a bare CR or LF")
	}
	for _, line := range strings.Split(data, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("a line of %d characters", len(line))
		}
	}
}

func TestDeliveredMail(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{})
	before := time.Now().Add(-2 * time.Second)
	mustDeliver(t, testParams(srv), testMessage())
	rec := srv.record()

	if rec.helo != "client.example.com" {
		t.Errorf("EHLO %q", rec.helo)
	}
	if rec.mailFrom != "alerts@example.com" {
		t.Errorf("MAIL FROM %q", rec.mailFrom)
	}
	if !reflect.DeepEqual(rec.rcpts, []string{"ops@example.org"}) {
		t.Errorf("RCPT TO %q", rec.rcpts)
	}
	if rec.saw("AUTH") || rec.saw("STARTTLS") || rec.tls {
		t.Errorf("unexpected commands: %q", rec.commands)
	}
	if rec.dataCount != 1 {
		t.Fatalf("DATA sent %d times", rec.dataCount)
	}
	checkWire(t, rec.data)

	m := parseData(t, rec.data)
	want := map[string]string{
		"From":                      "<alerts@example.com>",
		"To":                        "<ops@example.org>",
		"Subject":                   "[web-01] FAIL main/api-health",
		"Message-Id":                "<20261123T100200Z-main-api-health-fail@example.com>",
		"Mime-Version":              "1.0",
		"Content-Type":              "text/plain; charset=utf-8",
		"Content-Transfer-Encoding": "quoted-printable",
		"X-Onenotify-Level":         "error",
		"X-Onenotify-Host":          "web-01",
	}
	for name, value := range want {
		if got := m.Header.Get(name); got != value {
			t.Errorf("%s: %q, want %q", name, got, value)
		}
	}
	date, err := m.Header.Date()
	if err != nil {
		t.Fatalf("Date %q: %v", m.Header.Get("Date"), err)
	}
	if date.Before(before) || date.After(time.Now().Add(2*time.Second)) {
		t.Errorf("Date %v is not the send time", date)
	}
	if got := decodeBody(t, m); got != testMessage().Text {
		t.Errorf("body %q", got)
	}
}

func TestSubjectAndBodyRoundTrip(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{})
	msg := testMessage()
	msg.Title = "[веб-01] СБОЙ main/api-health: диск переполнен, свободно меньше одного процента от объёма раздела"
	msg.Host = "веб-01"
	msg.Text = strings.Join([]string{
		"Привет, мир",
		".",
		".starts with a dot",
		"..two dots",
		"crlf line\r",
		"equals = sign and <html> & stuff",
		strings.Repeat("x", 1500),
		strings.Repeat("ы", 700),
		strings.Repeat(".", 200),
		"emoji 🚀🔥 and 👨‍👩‍👧",
		"trailing space ",
		"\ttab and control \x01 character",
		"",
		"From here on",
		"last line without a line break",
	}, "\n")
	mustDeliver(t, testParams(srv), msg)
	rec := srv.record()
	checkWire(t, rec.data)

	m := parseData(t, rec.data)
	if got := decodeHeader(t, m, "Subject"); got != msg.Title {
		t.Errorf("Subject %q, want %q", got, msg.Title)
	}
	if raw := m.Header.Get("Subject"); !strings.HasPrefix(raw, "=?utf-8?q?") && !strings.HasPrefix(raw, "=?UTF-8?q?") {
		t.Errorf("Subject is not Q encoded: %q", raw)
	}
	if got := decodeHeader(t, m, "X-Onenotify-Host"); got != msg.Host {
		t.Errorf("X-Onenotify-Host %q", got)
	}
	want := strings.ReplaceAll(msg.Text, "\r\n", "\n")
	if got := decodeBody(t, m); got != want {
		t.Errorf("body does not round-trip:\n got %q\nwant %q", got, want)
	}
}

func TestBodyEdgeCases(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"empty", "", ""},
		{"no final line break", "one", "one"},
		{"only a dot", ".", "."},
		{"only line breaks", "\n\n", "\n\n"},
		{"lone cr", "a\rb", "a\nb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeSMTP(t, fakeOptions{})
			msg := testMessage()
			msg.Text = tc.text
			mustDeliver(t, testParams(srv), msg)
			rec := srv.record()
			checkWire(t, rec.data)
			m := parseData(t, rec.data)
			if got := decodeBody(t, m); got != tc.want {
				t.Errorf("body %q, want %q", got, tc.want)
			}
			if tc.text == "" && !strings.HasSuffix(rec.data, "\r\n\r\n") {
				t.Errorf("an empty text must give an empty body: %q", rec.data)
			}
		})
	}
}

// JSON cannot carry invalid UTF-8, so this goes to the message builder itself.
func TestInvalidUTF8IsDropped(t *testing.T) {
	msg := testMessage()
	msg.Title = "disk \xff full \xc3"
	msg.Text = "a\xffb\xc3\n\xf0\x9f"
	from := &mail.Address{Address: "alerts@example.com"}
	data := string(buildMessage(msg, from, []*mail.Address{{Address: "ops@example.org"}}, time.Now()))
	checkWire(t, data)
	m := parseData(t, data)
	if got := decodeHeader(t, m, "Subject"); got != "disk  full" {
		t.Errorf("Subject %q", got)
	}
	if got := decodeBody(t, m); got != "ab\n" {
		t.Errorf("body %q", got)
	}
}

func TestSeveralRecipientsAndNamedSender(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{})
	params := with(testParams(srv),
		"from", "Alerts <alerts@example.com>",
		"to", []string{"ops@example.org", "Дежурный <duty@example.net>", "third@example.org"})
	mustDeliver(t, params, testMessage())
	rec := srv.record()

	if rec.mailFrom != "alerts@example.com" {
		t.Errorf("MAIL FROM %q", rec.mailFrom)
	}
	wantRcpts := []string{"ops@example.org", "duty@example.net", "third@example.org"}
	if !reflect.DeepEqual(rec.rcpts, wantRcpts) {
		t.Errorf("RCPT TO %q, want %q", rec.rcpts, wantRcpts)
	}
	checkWire(t, rec.data)
	m := parseData(t, rec.data)
	from, err := m.Header.AddressList("From")
	if err != nil || len(from) != 1 || from[0].Name != "Alerts" || from[0].Address != "alerts@example.com" {
		t.Errorf("From %q: %v %v", m.Header.Get("From"), from, err)
	}
	to, err := m.Header.AddressList("To")
	if err != nil || len(to) != 3 {
		t.Fatalf("To %q: %v %v", m.Header.Get("To"), to, err)
	}
	for i, a := range to {
		if a.Address != wantRcpts[i] {
			t.Errorf("To[%d] = %q", i, a.Address)
		}
	}
	if to[1].Name != "Дежурный" {
		t.Errorf("To[1] name %q", to[1].Name)
	}
	if got := m.Header.Get("Message-Id"); !strings.HasSuffix(got, "@example.com>") {
		t.Errorf("Message-ID %q", got)
	}
}

func TestHeaderInjection(t *testing.T) {
	titles := []string{
		"disk full\r\nBcc: evil@example.com",
		"disk full\nBcc: evil@example.com",
		"disk full\rBcc: evil@example.com",
		"диск\r\nBcc: evil@example.com",
		"disk full\r\n\r\nBcc: evil@example.com",
	}
	for _, title := range titles {
		t.Run(title, func(t *testing.T) {
			srv := newFakeSMTP(t, fakeOptions{})
			msg := testMessage()
			msg.Title = title
			msg.Host = "web-01\r\nBcc: evil@example.com"
			msg.Level = "error\r\nBcc: evil@example.com"
			msg.ID = "id\r\nBcc: evil@example.com"
			mustDeliver(t, testParams(srv), msg)
			rec := srv.record()
			checkWire(t, rec.data)

			if !reflect.DeepEqual(rec.rcpts, []string{"ops@example.org"}) {
				t.Errorf("RCPT TO %q", rec.rcpts)
			}
			m := parseData(t, rec.data)
			if got := m.Header.Get("Bcc"); got != "" {
				t.Errorf("injected Bcc header: %q", got)
			}
			if len(m.Header) != 10 {
				t.Errorf("%d headers, want 10: %v", len(m.Header), m.Header)
			}
			head, _, _ := strings.Cut(rec.data, "\r\n\r\n")
			for _, line := range strings.Split(head, "\r\n") {
				if strings.HasPrefix(strings.ToLower(line), "bcc") {
					t.Errorf("header line %q", line)
				}
			}
			subject := decodeHeader(t, m, "Subject")
			if strings.ContainsAny(subject, "\r\n") || !strings.Contains(subject, "Bcc: evil@example.com") {
				t.Errorf("Subject %q", subject)
			}
			if got := decodeBody(t, m); got != msg.Text {
				t.Errorf("body %q", got)
			}
		})
	}
}

func TestUnstructuredHeader(t *testing.T) {
	long := strings.Repeat("word ", 300) + "end"
	huge := strings.Repeat("x", 3000)
	cases := []struct{ name, value, want string }{
		{"plain", "[web-01] FAIL main/api-health", "[web-01] FAIL main/api-health"},
		{"empty", "", ""},
		{"spaces kept", "a   b", "a   b"},
		{"trimmed", "  a b\t", "a b"},
		{"long ascii", long, long},
		{"one huge word", huge, huge},
		{"looks encoded", "=?utf-8?q?fake?= title", "=?utf-8?q?fake?= title"},
		{"cyrillic", strings.Repeat("Сбой проверки ", 60), strings.TrimSpace(strings.Repeat("Сбой проверки ", 60))},
		{"emoji", "🔥 fire", "🔥 fire"},
		{"invalid utf-8", "a\xffb", "ab"},
		{"controls", "a\x00b\x7fc", "a b c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := "Subject: " + unstructured("Subject", tc.value) + "\r\n\r\n"
			checkWire(t, raw)
			for _, line := range strings.Split(strings.TrimSuffix(raw, "\r\n\r\n"), "\r\n")[1:] {
				if !strings.HasPrefix(line, " ") || strings.TrimSpace(line) == "" {
					t.Fatalf("bad continuation line %q", line)
				}
			}
			m := parseData(t, raw)
			if got := decodeHeader(t, m, "Subject"); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMessageID(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	cases := []struct{ id, from, want string }{
		{"20261123T100200Z-main-api-health-fail", "alerts@example.com", "<20261123T100200Z-main-api-health-fail@example.com>"},
		{"a b<c>@d/e", "alerts@mail.example.com", "<a_b_c__d_e@mail.example.com>"},
		{"..a..b..", "a@example.com", "<_a._b._@example.com>"},
		{"ид-1", "a@example.com", "<____-1@example.com>"},
		{"id", "a@[127.0.0.1]", "<id@_127.0.0.1_>"},
	}
	for _, tc := range cases {
		if got := messageID(tc.id, tc.from, now); got != tc.want {
			t.Errorf("messageID(%q, %q) = %q, want %q", tc.id, tc.from, got, tc.want)
		}
	}
	generated := messageID("", "a@example.com", now)
	if !strings.HasPrefix(generated, "<20261124T120000Z-") || !strings.HasSuffix(generated, "@example.com>") {
		t.Errorf("generated id %q", generated)
	}
	if long := messageID(strings.Repeat("x", 5000), "a@example.com", now); len(long) > 300 {
		t.Errorf("Message-ID of %d characters", len(long))
	}
}

func TestAuthOnlyWithUsername(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{})
	mustDeliver(t, testParams(srv), testMessage())
	if rec := srv.record(); rec.saw("AUTH") {
		t.Errorf("AUTH without username: %q", rec.commands)
	}
}

func TestAuthPlainWithoutTLS(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{})
	mustDeliver(t, with(testParams(srv), "username", "robot", "password", testPassword), testMessage())
	rec := srv.record()
	if want := []string{"PLAIN", "\x00robot\x00" + testPassword}; !reflect.DeepEqual(rec.auth, want) {
		t.Errorf("AUTH %q, want %q", rec.auth, want)
	}
	if rec.authTLS || rec.dataCount != 1 {
		t.Errorf("authTLS %v, DATA sent %d times", rec.authTLS, rec.dataCount)
	}
}

func TestAuthLoginFallback(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{extensions: []string{"AUTH LOGIN CRAM-MD5"}})
	mustDeliver(t, with(testParams(srv), "username", "robot", "password", testPassword), testMessage())
	rec := srv.record()
	if want := []string{"LOGIN", "robot", testPassword}; !reflect.DeepEqual(rec.auth, want) {
		t.Errorf("AUTH %q, want %q", rec.auth, want)
	}
	if rec.dataCount != 1 {
		t.Errorf("DATA sent %d times", rec.dataCount)
	}
}

func TestAuthPrefersPlain(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{extensions: []string{"AUTH LOGIN PLAIN"}})
	mustDeliver(t, with(testParams(srv), "username", "robot", "password", testPassword), testMessage())
	if rec := srv.record(); len(rec.auth) == 0 || rec.auth[0] != "PLAIN" {
		t.Errorf("AUTH %q", rec.auth)
	}
}

func TestAuthNotOffered(t *testing.T) {
	for name, exts := range map[string][]string{
		"no AUTH":      {"8BITMIME"},
		"only CRAM":    {"AUTH CRAM-MD5"},
		"no extension": {},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newFakeSMTP(t, fakeOptions{extensions: exts})
			text := mustFail(t, with(testParams(srv), "username", "robot", "password", testPassword), testMessage())
			rec := srv.record()
			if rec.saw("AUTH") || rec.saw("MAIL") {
				t.Errorf("commands %q", rec.commands)
			}
			if !strings.HasPrefix(text, "mail: AUTH: ") {
				t.Errorf("error %q", text)
			}
		})
	}
}

func TestAuthAfterStartTLS(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{startTLS: true})
	params := with(testParams(srv), "tls", "starttls", "insecure_skip_verify", true,
		"username", "robot", "password", testPassword)
	mustDeliver(t, params, testMessage())
	rec := srv.record()
	if !rec.tls || !rec.authTLS {
		t.Errorf("tls %v, AUTH over TLS %v", rec.tls, rec.authTLS)
	}
	if want := []string{"EHLO", "STARTTLS", "EHLO", "AUTH", "MAIL", "RCPT", "DATA", ".", "QUIT"}; !reflect.DeepEqual(rec.commands, want) {
		t.Errorf("commands %q, want %q", rec.commands, want)
	}
	if want := []string{"PLAIN", "\x00robot\x00" + testPassword}; !reflect.DeepEqual(rec.auth, want) {
		t.Errorf("AUTH %q", rec.auth)
	}
}

func TestStartTLSIsTheDefault(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{startTLS: true})
	params := with(testParams(srv), "insecure_skip_verify", true)
	delete(params, "tls")
	mustDeliver(t, params, testMessage())
	if rec := srv.record(); !rec.tls || rec.dataCount != 1 {
		t.Errorf("tls %v, DATA sent %d times", rec.tls, rec.dataCount)
	}
}

func TestStartTLSNotOffered(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{})
	params := with(testParams(srv), "tls", "starttls", "insecure_skip_verify", true,
		"username", "robot", "password", testPassword)
	text := mustFail(t, params, testMessage())
	rec := srv.record()
	for _, verb := range []string{"STARTTLS", "AUTH", "MAIL", "RCPT", "DATA"} {
		if rec.saw(verb) {
			t.Errorf("%s was sent: %q", verb, rec.commands)
		}
	}
	if !strings.Contains(text, "STARTTLS") {
		t.Errorf("error %q", text)
	}
}

func TestStartTLSRefused(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{startTLS: true, replies: map[string]string{"STARTTLS": "454 TLS not available"}})
	text := mustFail(t, with(testParams(srv), "tls", "starttls", "insecure_skip_verify", true), testMessage())
	if rec := srv.record(); rec.saw("MAIL") {
		t.Errorf("commands %q", rec.commands)
	}
	if text != "mail: STARTTLS: 454 TLS not available" {
		t.Errorf("error %q", text)
	}
}

func TestImplicitTLS(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{implicitTLS: true})
	params := with(testParams(srv), "tls", "tls", "insecure_skip_verify", true,
		"username", "robot", "password", testPassword)
	mustDeliver(t, params, testMessage())
	rec := srv.record()
	if !rec.tls || !rec.authTLS || rec.saw("STARTTLS") || rec.dataCount != 1 {
		t.Errorf("tls %v, AUTH over TLS %v, commands %q", rec.tls, rec.authTLS, rec.commands)
	}
	if rec.mailFrom != "alerts@example.com" {
		t.Errorf("MAIL FROM %q", rec.mailFrom)
	}
}

func TestUntrustedCertificate(t *testing.T) {
	cases := []struct {
		mode string
		opts fakeOptions
	}{
		{"starttls", fakeOptions{startTLS: true}},
		{"tls", fakeOptions{implicitTLS: true}},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			srv := newFakeSMTP(t, tc.opts)
			params := with(testParams(srv), "tls", tc.mode, "username", "robot", "password", testPassword)
			text := mustFail(t, params, testMessage())
			rec := srv.record()
			if rec.saw("AUTH") || rec.saw("MAIL") || rec.dataCount != 0 {
				t.Errorf("commands %q", rec.commands)
			}
			if !strings.Contains(text, "certificate") {
				t.Errorf("error %q", text)
			}

			srv = newFakeSMTP(t, tc.opts)
			mustDeliver(t, with(params, "port", srv.port(), "insecure_skip_verify", true), testMessage())
			if rec := srv.record(); !rec.tls || rec.dataCount != 1 {
				t.Errorf("tls %v, DATA sent %d times", rec.tls, rec.dataCount)
			}
		})
	}
}

func TestPermanentError(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{replies: map[string]string{".": "552 5.3.4 message too big"}})
	resp := runSend(t, testParams(srv), testMessage(), 10000)
	if resp.Delivered || resp.Error == nil || !resp.Error.Permanent {
		t.Fatalf("response %+v, want a permanent error", resp)
	}
	if want := "mail: end of DATA: 552 5.3.4 message too big"; resp.Error.Message != want {
		t.Errorf("error %q, want %q", resp.Error.Message, want)
	}
	if resp.Error.RetryAfterMS != 0 {
		t.Errorf("retry_after_ms %d", resp.Error.RetryAfterMS)
	}
}

func TestMailboxFullIsTemporary(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{replies: map[string]string{"RCPT": "552 mailbox full"}})
	resp := runSend(t, testParams(srv), testMessage(), 10000)
	if resp.Delivered || resp.Error.Permanent {
		t.Fatalf("response %+v, want a temporary error", resp)
	}
	if rec := srv.record(); rec.dataCount != 0 {
		t.Errorf("DATA sent %d times", rec.dataCount)
	}
}

func TestMessageTooBigIsPermanent(t *testing.T) {
	for _, prefix := range []string{"MAIL", "DATA"} {
		t.Run(prefix, func(t *testing.T) {
			srv := newFakeSMTP(t, fakeOptions{replies: map[string]string{prefix: "552 too much"}})
			resp := runSend(t, testParams(srv), testMessage(), 10000)
			if resp.Delivered || !resp.Error.Permanent {
				t.Fatalf("response %+v, want a permanent error", resp)
			}
			if rec := srv.record(); rec.dataCount != 0 {
				t.Errorf("DATA sent %d times", rec.dataCount)
			}
		})
	}
}

func TestTemporaryErrors(t *testing.T) {
	auth := []any{"username", "robot", "password", testPassword}
	cases := []struct {
		name    string
		replies map[string]string
		params  []any
		want    string
		noVerb  string // a command that must not have been sent
	}{
		{"535 on AUTH", map[string]string{"AUTH": "535 5.7.8 authentication failed"}, auth,
			"mail: AUTH: 535 5.7.8 authentication failed", "MAIL"},
		{"AUTH reply echoes the password", map[string]string{"AUTH": "535 bad password " + testPassword}, auth,
			"mail: AUTH: 535 bad password ***", "MAIL"},
		{"451 on MAIL", map[string]string{"MAIL": "451 4.3.0 try again later"}, nil,
			"mail: MAIL FROM <alerts@example.com>: 451 4.3.0 try again later", "RCPT"},
		{"550 on MAIL", map[string]string{"MAIL": "550 sender rejected"}, nil,
			"mail: MAIL FROM <alerts@example.com>: 550 sender rejected", "RCPT"},
		{"550 on RCPT", map[string]string{"RCPT": "550 5.1.1 no such user"}, nil,
			"mail: RCPT TO <ops@example.org>: 550 5.1.1 no such user", "DATA"},
		{"452 on RCPT", map[string]string{"RCPT": "452 4.2.2 mailbox full"}, nil,
			"mail: RCPT TO <ops@example.org>: 452 4.2.2 mailbox full", "DATA"},
		{"554 on DATA", map[string]string{"DATA": "554 5.7.1 rejected by policy"}, nil,
			"mail: DATA: 554 5.7.1 rejected by policy", "."},
		{"554 on the end of DATA", map[string]string{".": "554 5.7.1 looks like spam"}, nil,
			"mail: end of DATA: 554 5.7.1 looks like spam", ""},
		{"451 on the end of DATA", map[string]string{".": "451 4.7.1 greylisted"}, nil,
			"mail: end of DATA: 451 4.7.1 greylisted", ""},
		{"multi-line reply", map[string]string{"MAIL": "550-first line\r\n550 second line"}, nil,
			"mail: MAIL FROM <alerts@example.com>: 550 first line second line", "RCPT"},
		{"421 on EHLO", map[string]string{"EHLO": "421 too busy", "HELO": "421 too busy"}, nil,
			"mail: EHLO: 421 too busy", "MAIL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeSMTP(t, fakeOptions{replies: tc.replies})
			text := mustFail(t, with(testParams(srv), tc.params...), testMessage())
			if text != tc.want {
				t.Errorf("error %q, want %q", text, tc.want)
			}
			if rec := srv.record(); tc.noVerb != "" && rec.saw(tc.noVerb) {
				t.Errorf("%s was sent: %q", tc.noVerb, rec.commands)
			}
		})
	}
}

func TestOneRejectedRecipientFailsTheDelivery(t *testing.T) {
	for _, bad := range []string{"first@example.org", "second@example.org"} {
		t.Run(bad, func(t *testing.T) {
			srv := newFakeSMTP(t, fakeOptions{replies: map[string]string{"RCPT TO:<" + bad + ">": "550 no such user"}})
			params := with(testParams(srv), "to", []string{"first@example.org", "second@example.org"})
			text := mustFail(t, params, testMessage())
			if want := "mail: RCPT TO <" + bad + ">: 550 no such user"; text != want {
				t.Errorf("error %q, want %q", text, want)
			}
			rec := srv.record()
			if rec.saw("DATA") || rec.dataCount != 0 {
				t.Errorf("DATA was sent: %q", rec.commands)
			}
		})
	}
}

func TestConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	params := map[string]any{
		"host": "127.0.0.1", "port": port, "tls": "none",
		"from": "alerts@example.com", "to": []string{"ops@example.org"},
		"username": "robot", "password": testPassword,
	}
	text := mustFail(t, params, testMessage())
	if !strings.HasPrefix(text, "mail: connect: ") {
		t.Errorf("error %q", text)
	}
}

func TestServerNeverReplies(t *testing.T) {
	for _, mode := range []string{"none", "starttls", "tls"} {
		t.Run(mode, func(t *testing.T) {
			srv := newFakeSMTP(t, fakeOptions{silent: true})
			params := with(testParams(srv), "tls", mode, "username", "robot", "password", testPassword)
			start := time.Now()
			resp := runSend(t, params, testMessage(), 300)
			elapsed := time.Since(start)
			if resp.Delivered || resp.Error.Permanent {
				t.Fatalf("response %+v, want a temporary error", resp)
			}
			if elapsed < 250*time.Millisecond || elapsed > 5*time.Second {
				t.Errorf("returned after %v with a timeout of 300ms", elapsed)
			}
			if !strings.Contains(resp.Error.Message, "timed out") {
				t.Errorf("error %q", resp.Error.Message)
			}
		})
	}
}

func TestCancelledContext(t *testing.T) {
	srv := newFakeSMTP(t, fakeOptions{silent: true})
	rawParams, err := json.Marshal(testParams(srv))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	err = send(ctx, module.Request{Params: rawParams, Message: testMessage()})
	if err == nil {
		t.Fatal("no error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("returned after %v", elapsed)
	}
}

func TestInvalidParams(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"host": "127.0.0.1", "port": 1, "tls": "none",
			"from": "alerts@example.com", "to": []string{"ops@example.org"},
		}
	}
	cases := map[string]map[string]any{
		"unknown key":               with(base(), "hots", "x"),
		"no host":                   with(base(), "host", ""),
		"bad tls":                   with(base(), "tls", "ssl"),
		"bad port":                  with(base(), "port", 70000),
		"no from":                   with(base(), "from", ""),
		"bad from":                  with(base(), "from", "not an address"),
		"empty to":                  with(base(), "to", []string{}),
		"bad to":                    with(base(), "to", []string{"ops@example.org", "nobody"}),
		"two addresses in one to":   with(base(), "to", []string{"a@example.org, b@example.org"}),
		"password without username": with(base(), "password", testPassword),
		"line break in from":        with(base(), "from", "a@example.com\r\nRCPT TO:<evil@example.com>"),
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			text := mustFail(t, params, testMessage())
			if !strings.HasPrefix(text, "mail: ") || strings.Contains(text, "connect") {
				t.Errorf("error %q", text)
			}
		})
	}
}

func TestInvalidParamsDirect(t *testing.T) {
	err := send(context.Background(), module.Request{
		Params:  json.RawMessage(`{"host":"127.0.0.1","from":"a@example.com","to":["b@example.com"],"unknown":1}`),
		Message: testMessage(),
	})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error %v", err)
	}
}

func TestDefaultPort(t *testing.T) {
	for mode, want := range map[string]int{"starttls": 587, "tls": 465, "none": 25, "": 587} {
		if got := defaultPort(mode); got != want {
			t.Errorf("defaultPort(%q) = %d, want %d", mode, got, want)
		}
	}
}

func TestResolveDefaults(t *testing.T) {
	s, err := resolve(params{Host: "smtp.example.com", From: "Alerts <alerts@example.com>", To: []string{"ops@example.org"}})
	if err != nil {
		t.Fatal(err)
	}
	if s.TLS != "starttls" || s.Port != 587 || s.Helo == "" || s.InsecureSkipVerify {
		t.Errorf("defaults: %+v", s.params)
	}
	if s.from.Address != "alerts@example.com" || s.to[0].Address != "ops@example.org" {
		t.Errorf("addresses: %v %v", s.from, s.to)
	}
	for mode, port := range map[string]int{"tls": 465, "none": 25} {
		s, err := resolve(params{Host: "h", TLS: mode, From: "a@example.com", To: []string{"b@example.com"}})
		if err != nil || s.Port != port {
			t.Errorf("tls %q: port %d, err %v", mode, s.Port, err)
		}
	}
	s, err = resolve(params{Host: "h", Port: 2525, Helo: "me.example.com", From: "a@example.com", To: []string{"b@example.com"}})
	if err != nil || s.Port != 2525 || s.Helo != "me.example.com" {
		t.Errorf("explicit values: %+v, err %v", s, err)
	}
}

// The fake server is on 127.0.0.1, where even smtp.PlainAuth would agree to
// work without TLS. These tests show the module's own rule: without TLS,
// credentials go out only when plain text was asked for, whatever the host.
func TestAuthTypesGuardPlainText(t *testing.T) {
	remote := &smtp.ServerInfo{Name: "mail.example.com", TLS: false, Auth: []string{"PLAIN", "LOGIN"}}
	remoteTLS := &smtp.ServerInfo{Name: "mail.example.com", TLS: true, Auth: []string{"PLAIN", "LOGIN"}}
	local := &smtp.ServerInfo{Name: "localhost", TLS: false, Auth: []string{"PLAIN", "LOGIN"}}

	for _, offered := range []string{"PLAIN", "LOGIN"} {
		t.Run(offered, func(t *testing.T) {
			strict, err := chooseAuth(offered, "robot", testPassword, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, server := range []*smtp.ServerInfo{remote, local} {
				if _, resp, err := strict.Start(server); !errors.Is(err, errUnencryptedAuth) || resp != nil {
					t.Errorf("%s without TLS: resp %q, err %v", server.Name, resp, err)
				}
			}
			if mech, _, err := strict.Start(remoteTLS); err != nil || mech != offered {
				t.Errorf("with TLS: %q, %v", mech, err)
			}

			allowed, err := chooseAuth(offered, "robot", testPassword, true)
			if err != nil {
				t.Fatal(err)
			}
			if mech, _, err := allowed.Start(remote); err != nil || mech != offered {
				t.Errorf("plain text allowed: %q, %v", mech, err)
			}
		})
	}
}

func TestPlainAuthExchange(t *testing.T) {
	a := &plainAuth{username: "robot", password: testPassword, plainTextAllowed: true}
	mech, resp, err := a.Start(&smtp.ServerInfo{Name: "mail.example.com"})
	if err != nil || mech != "PLAIN" || string(resp) != "\x00robot\x00"+testPassword {
		t.Errorf("Start: %q %q %v", mech, resp, err)
	}
	if resp, err := a.Next(nil, false); resp != nil || err != nil {
		t.Errorf("Next: %q %v", resp, err)
	}
	if _, err := a.Next([]byte("more?"), true); err == nil {
		t.Error("an unexpected challenge must be an error")
	}
}

func TestLoginAuthExchange(t *testing.T) {
	a := &loginAuth{username: "robot", password: testPassword, plainTextAllowed: true}
	mech, resp, err := a.Start(&smtp.ServerInfo{Name: "mail.example.com"})
	if err != nil || mech != "LOGIN" || resp != nil {
		t.Errorf("Start: %q %q %v", mech, resp, err)
	}
	for _, want := range []string{"robot", testPassword} {
		if resp, err := a.Next([]byte("prompt"), true); err != nil || string(resp) != want {
			t.Errorf("Next: %q %v, want %q", resp, err, want)
		}
	}
	if _, err := a.Next([]byte("prompt"), true); err == nil {
		t.Error("a third challenge must be an error")
	}
	if resp, err := a.Next(nil, false); resp != nil || err != nil {
		t.Errorf("final Next: %q %v", resp, err)
	}
}

func TestChooseAuthUnsupported(t *testing.T) {
	if _, err := chooseAuth("CRAM-MD5 GSSAPI", "robot", testPassword, false); err == nil {
		t.Error("no error")
	}
}

func TestScrub(t *testing.T) {
	plain := "AHJvYm90AHMzY3JldC1QYTU1d29yZA==" // base64 of \0robot\0s3cret-Pa55word
	login := "czNjcmV0LVBhNTV3b3Jk"             // base64 of s3cret-Pa55word
	got := scrub("535 bad "+testPassword+" / "+plain+" /\n "+login, "robot", testPassword)
	if got != "535 bad *** / *** / ***" {
		t.Errorf("scrub: %q", got)
	}
	if got := scrub("nothing to hide", "robot", ""); got != "nothing to hide" {
		t.Errorf("scrub with an empty password: %q", got)
	}
}

func TestDescriptionSchema(t *testing.T) {
	if mod.Description.Name != "mail" || mod.Description.Description == "" {
		t.Errorf("description: %+v", mod.Description)
	}
	s, err := schema.Compile(mod.Description.ParamsSchema)
	if err != nil {
		t.Fatal(err)
	}
	valid := []string{
		`{"host":"smtp.example.com","from":"alerts@example.com","to":["ops@example.org"]}`,
		`{"host":"smtp.example.com","port":2525,"tls":"none","username":"u","password":"p",
		  "from":"Alerts <alerts@example.com>","to":["a@example.org","b@example.org"],
		  "helo":"web-01.example.com","insecure_skip_verify":true}`,
		`{"host":"h","tls":"tls","username":"u","from":"a@b.c","to":["d@e.f"]}`,
		`{"host":"h","tls":"starttls","from":"a@b.c","to":["d@e.f"]}`,
	}
	for _, doc := range valid {
		if err := s.Validate(json.RawMessage(doc)); err != nil {
			t.Errorf("%s: %v", doc, err)
		}
	}
	invalid := map[string]string{
		"unknown key":               `{"host":"h","from":"a@b.c","to":["d@e.f"],"hots":"x"}`,
		"missing host":              `{"from":"a@b.c","to":["d@e.f"]}`,
		"empty host":                `{"host":"","from":"a@b.c","to":["d@e.f"]}`,
		"missing from":              `{"host":"h","to":["d@e.f"]}`,
		"missing to":                `{"host":"h","from":"a@b.c"}`,
		"empty to":                  `{"host":"h","from":"a@b.c","to":[]}`,
		"to is a string":            `{"host":"h","from":"a@b.c","to":"d@e.f"}`,
		"tls ssl":                   `{"host":"h","from":"a@b.c","to":["d@e.f"],"tls":"ssl"}`,
		"password without username": `{"host":"h","from":"a@b.c","to":["d@e.f"],"password":"p"}`,
		"port as a string":          `{"host":"h","port":"587","from":"a@b.c","to":["d@e.f"]}`,
		"port out of range":         `{"host":"h","port":0,"from":"a@b.c","to":["d@e.f"]}`,
		"insecure as a string":      `{"host":"h","from":"a@b.c","to":["d@e.f"],"insecure_skip_verify":"yes"}`,
		"empty params":              `{}`,
	}
	for name, doc := range invalid {
		if err := s.Validate(json.RawMessage(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDescribe(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := module.Run(mod, []string{"describe"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d: %s", code, stderr.String())
	}
	var d module.Description
	if err := json.Unmarshal(stdout.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Protocol != module.ProtocolVersion || d.Name != "mail" || d.Version == "" || len(d.ParamsSchema) == 0 {
		t.Errorf("description: %+v", d)
	}
}
