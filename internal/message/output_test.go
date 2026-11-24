package message

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// outputText builds a message from a fail event without params and tags,
// with more top-level keys and the given saved output, and returns what
// follows the failure reason in its text.
func outputText(t *testing.T, top map[string]any, output map[string]any) string {
	t.Helper()
	keys := map[string]any{"params": nil}
	for k, v := range top {
		keys[k] = v
	}
	m, err := FromOnecheck(ev(keys, map[string]any{"tags": nil, "output": output}), "web-01", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	_, after, _ := strings.Cut(m.Text, "- status: expected equals 200, got 503")
	return after
}

func fields(names ...string) map[string]any {
	return map[string]any{"output_fields": names}
}

func TestOutputBlock(t *testing.T) {
	output := map[string]any{"stdout": "SECRET BODY", "stderr": "one\ntwo\n"}
	m, err := FromOnecheck(ev(fields("stderr"), map[string]any{"output": output}), "web-01", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	want := "module: http\nparams:\n  headers.Authorization: Bearer ${API_TOKEN}\n  url: https://example.com/health\n" +
		"tags: api, web\nat: 2026-11-23 10:02:00 UTC\n" +
		"failed 3 runs in a row:\n- status: expected equals 200, got 503\n" +
		"stderr:\none\ntwo"
	if m.Text != want {
		t.Errorf("text:\n%s\n--- want:\n%s", m.Text, want)
	}

	top := reminder(map[string]any{"output_fields": []string{"stderr"}})
	if got := outputText(t, top, output); got != "\nstderr:\none\ntwo" {
		t.Errorf("a reminder shows output like a failure: %q", got)
	}

	// A check that could not run has no conditions; its output follows the error.
	m, err = FromOnecheck(ev(fields("stderr"), map[string]any{
		"status": "error", "error": "timed out", "failures": []any{}, "output": output,
	}), "web-01", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(m.Text, "\nerror: timed out\nstderr:\none\ntwo") {
		t.Errorf("text:\n%s", m.Text)
	}
}

func TestOutputNotShown(t *testing.T) {
	output := map[string]any{"stderr": "SECRET BODY"}
	for name, top := range map[string]map[string]any{
		"recovered":          {"transition": "recovered", "output_fields": []string{"stderr"}},
		"unknown transition": {"transition": "flapping", "output_fields": []string{"stderr"}},
		"redacted":           {"redacted": true, "output_fields": []string{"stderr"}},
		"redacted reminder":  reminder(map[string]any{"redacted": true, "output_fields": []string{"stderr"}}),
		"nothing allowed":    {"output_fields": []string{}},
		"null":               {"output_fields": nil},
		"an older onecheck":  {"output_fields": drop},
	} {
		m, err := FromOnecheck(ev(top, map[string]any{"output": output}), "web-01", time.Hour, now)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if strings.Contains(m.Text, "SECRET BODY") || strings.Contains(m.Text, "stderr") {
			t.Errorf("%s: output in the text:\n%s", name, m.Text)
		}
	}
}

func TestOutputFields(t *testing.T) {
	cases := []struct {
		name   string
		fields []string
		output map[string]any
		want   string
	}{
		{
			name:   "fields come in the order the rule gave",
			fields: []string{"stderr", "stdout"},
			output: map[string]any{"stdout": "out", "stderr": "err"},
			want:   "\nstderr:\nerr\nstdout:\nout",
		},
		{
			name:   "a repeated name is shown once",
			fields: []string{"stderr", "stderr"},
			output: map[string]any{"stderr": "err"},
			want:   "\nstderr:\nerr",
		},
		{
			name:   "nothing to show leaves no header",
			fields: []string{"gone", "null", "empty", "blank", "stderr", "none"},
			output: map[string]any{"null": nil, "empty": "", "blank": "\n \n\t\r\n", "stderr": "err", "none": map[string]any{}},
			want:   "\nstderr:\nerr",
		},
		{
			name:   "an object is printed like params",
			fields: []string{"headers"},
			output: map[string]any{"headers": map[string]any{
				"x": map[string]any{"y": 1}, "content-type": "text/plain", "multi": "a\nb",
			}},
			want: "\nheaders:\ncontent-type: text/plain\nmulti: a b\nx.y: 1",
		},
		{
			name:   "other values take one line",
			fields: []string{"n", "flag", "list"},
			output: map[string]any{"n": 7, "flag": false, "list": []any{"a", 1}},
			want:   "\nn:\n7\nflag:\nfalse\nlist:\n[\"a\",1]",
		},
		{
			name:   "a field name is one line",
			fields: []string{"std\nerr", " \n"},
			output: map[string]any{"std\nerr": "err", " \n": "x"},
			want:   "\nstd err:\nerr",
		},
		{
			name:   "no saved output at all",
			fields: []string{"stderr"},
			output: nil,
			want:   "",
		},
	}
	for _, c := range cases {
		if got := outputText(t, fields(c.fields...), c.output); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestOutputIsCleaned(t *testing.T) {
	cases := map[string]string{
		"a\r\nb\rc\td \x1b[31mred\x1b[0m\x00  \n\n": "a\nb\nc d red",
		"\n\n  indented\n\nlast   ":                 "  indented\n\nlast",
		"\x1b[1;32m✓\x1b[m done\x1b[K\x1b]0;t\x07":  "✓ done",
	}
	for in, want := range cases {
		if got := outputText(t, fields("stderr"), map[string]any{"stderr": in}); got != "\nstderr:\n"+want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestOutputLimits(t *testing.T) {
	numbered := func(from, to int) string {
		var lines []string
		for i := from; i <= to; i++ {
			lines = append(lines, fmt.Sprintf("l%02d", i))
		}
		return strings.Join(lines, "\n")
	}
	wide := strings.Repeat("я", 150)
	wideLines := func(n int) string {
		return strings.TrimSuffix(strings.Repeat(wide+"\n", n), "\n")
	}
	cases := []struct{ name, in, want string }{
		{"the last 20 lines", numbered(1, 25), "…\n" + numbered(6, 25)},
		{"20 lines fit", numbered(1, 20), numbered(1, 20)},
		{"lines that fit 1000 characters", wideLines(10), "…\n" + wideLines(6)},
		{"1000 characters fit", strings.Repeat("я", 500) + "\n" + strings.Repeat("я", 499),
			strings.Repeat("я", 500) + "\n" + strings.Repeat("я", 499)},
		{"one more does not", strings.Repeat("я", 500) + "\n" + strings.Repeat("я", 500),
			"…\n" + strings.Repeat("я", 500)},
		{"one long line keeps its end", strings.Repeat("a", 500) + strings.Repeat("я", 1000),
			"…" + strings.Repeat("я", 1000)},
		{"a long last line after others", "first\n" + strings.Repeat("a", 500) + strings.Repeat("я", 1000),
			"…" + strings.Repeat("я", 1000)},
	}
	for _, c := range cases {
		if got := outputText(t, fields("stdout"), map[string]any{"stdout": c.in}); got != "\nstdout:\n"+c.want {
			t.Errorf("%s:\n%q\n--- want:\n%q", c.name, got, c.want)
		}
	}

	// Each field has its own limit.
	got := outputText(t, fields("stdout", "stderr"), map[string]any{"stdout": numbered(1, 25), "stderr": numbered(1, 25)})
	if want := "\nstdout:\n…\n" + numbered(6, 25) + "\nstderr:\n…\n" + numbered(6, 25); got != want {
		t.Errorf("two fields:\n%q", got)
	}
}

func TestOutputFieldsMustBeAList(t *testing.T) {
	if _, err := FromOnecheck(ev(map[string]any{"output_fields": "stderr"}, nil), "h", time.Hour, now); err == nil {
		t.Error("output_fields that is not a list is not a usable event")
	}
	if _, err := FromOnecheck(ev(map[string]any{"since": "long ago"}, nil), "h", time.Hour, now); err == nil {
		t.Error("since that is not a time is not a usable event")
	}
}
