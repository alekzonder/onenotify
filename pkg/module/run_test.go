package module

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func run(t *testing.T, send SendFunc, arg, stdin string) (code int, stdout, stderr string) {
	t.Helper()
	m := Module{Description: Description{Name: "fake", Version: "1.2.3", Description: "d"}, Send: send}
	var out, errOut bytes.Buffer
	var args []string
	if arg != "" {
		args = []string{arg}
	}
	code = Run(m, args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

const request = `{"protocol":1,"channel":"c","params":{"a":1},"message":{"id":"i","title":"t","level":"error"},"timeout_ms":5000}`

func TestDescribe(t *testing.T) {
	code, out, _ := run(t, nil, "describe", "")
	var d Description
	if err := json.Unmarshal([]byte(out), &d); err != nil || code != 0 {
		t.Fatalf("code=%d out=%q err=%v", code, out, err)
	}
	if d.Protocol != ProtocolVersion || d.Name != "fake" || d.Version != "1.2.3" {
		t.Errorf("description = %+v", d)
	}
}

func TestSendOutcomes(t *testing.T) {
	base := errors.New("boom")
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"delivered", nil, `{"delivered":true,"error":null}`},
		{"temporary", base, `{"delivered":false,"error":{"message":"boom"}}`},
		{"permanent", Permanent(base), `{"delivered":false,"error":{"message":"boom","permanent":true}}`},
		{"retry after", RetryAfter(base, 30*time.Second), `{"delivered":false,"error":{"message":"boom","retry_after_ms":30000}}`},
		{"wrapped permanent", fmt.Errorf("ctx: %w", Permanent(base)), `{"delivered":false,"error":{"message":"ctx: boom","permanent":true}}`},
		{"wrapped retry", fmt.Errorf("ctx: %w", RetryAfter(base, time.Second)), `{"delivered":false,"error":{"message":"ctx: boom","retry_after_ms":1000}}`},
	}
	for _, c := range cases {
		code, out, _ := run(t, func(context.Context, Request) error { return c.err }, "send", request)
		if code != 0 || strings.TrimSpace(out) != c.want {
			t.Errorf("%s: code=%d out=%s, want %s", c.name, code, out, c.want)
		}
	}
	if Permanent(nil) != nil || RetryAfter(nil, time.Second) != nil {
		t.Error("marking a nil error must stay nil")
	}
}

func TestSendGetsTheRequestAndADeadline(t *testing.T) {
	var got Request
	var hasDeadline bool
	run(t, func(ctx context.Context, req Request) error {
		got = req
		_, hasDeadline = ctx.Deadline()
		return nil
	}, "send", request)
	if got.Channel != "c" || got.Message.Title != "t" || got.Message.Level != LevelError || string(got.Params) != `{"a":1}` {
		t.Errorf("request = %+v", got)
	}
	if !hasDeadline {
		t.Error("timeout_ms must become a context deadline")
	}
}

func TestBadInvocations(t *testing.T) {
	never := func(context.Context, Request) error { panic("must not be called") }
	if code, out, errOut := run(t, never, "send", "{not json"); code != 2 || out != "" || errOut == "" {
		t.Errorf("invalid request: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, out, _ := run(t, never, "send", `{"protocol":2}`); code != 2 || out != "" {
		t.Errorf("unknown protocol: code=%d out=%q", code, out)
	}
	if code, _, errOut := run(t, never, "frobnicate", ""); code != 2 || !strings.Contains(errOut, "usage") {
		t.Errorf("unknown subcommand: code=%d err=%q", code, errOut)
	}
	if code, _, _ := run(t, never, "", ""); code != 2 {
		t.Errorf("no subcommand: code=%d", code)
	}
}

func TestDecodeParams(t *testing.T) {
	var p struct {
		A int `json:"a"`
	}
	if err := DecodeParams(json.RawMessage(`{"a":2}`), &p); err != nil || p.A != 2 {
		t.Errorf("p=%+v err=%v", p, err)
	}
	if err := DecodeParams(json.RawMessage(`{"b":2}`), &p); err == nil {
		t.Error("unknown key must be an error")
	}
	if err := DecodeParams(nil, &p); err != nil {
		t.Errorf("no params: %v", err)
	}
	if err := DecodeParams(json.RawMessage(`null`), &p); err != nil {
		t.Errorf("null params: %v", err)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 8, "hello…"},
		{"héllo", 3, "…"},
		{"привет мир", 9, "при…"}, // 2-byte letters, 3-byte ellipsis
		{"ab", 1, "a"},
		{"abc", 0, ""},
		{"a\xffb", 10, "ab"},
	}
	for _, c := range cases {
		got := Truncate(c.in, c.max)
		if got != c.want || len(got) > c.max && c.max >= 0 || !utf8.ValidString(got) {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
	for max := 0; max < 40; max++ {
		got := Truncate("日本語のテキスト🔴 and more", max)
		if len(got) > max || !utf8.ValidString(got) {
			t.Errorf("max=%d: %q (%d bytes)", max, got, len(got))
		}
	}
}
