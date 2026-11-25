package message

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// ev builds an event from a base, overriding top-level keys and keys of "result".
func ev(top map[string]any, result map[string]any) []byte {
	res := map[string]any{
		"config": "main", "rule": "api-health", "module": "http", "module_version": "1.2.0",
		"tags": []string{"api", "web"}, "status": "fail", "state": "fail", "consecutive": 3,
		"failures": []map[string]any{{"path": "status", "message": "status: expected equals 200, got 503"}},
		"error":    nil, "output": map[string]any{"body": "SECRET BODY"},
	}
	for k, v := range result {
		res[k] = v
	}
	e := map[string]any{
		"id": "20261123T100200Z-main-api-health-fail", "config": "main", "rule": "api-health",
		"transition": "fail", "from": "pass", "to": "fail",
		"at": "2026-11-23T10:02:00Z", "expires_at": "2026-11-24T10:02:00Z",
		"since": "2026-11-23T10:02:00Z",
		"params": map[string]any{
			"url":     "https://example.com/health",
			"headers": map[string]any{"Authorization": "Bearer ${API_TOKEN}"},
		},
		"redacted": false,
		"result":   res,
	}
	for k, v := range top {
		if v == drop {
			delete(e, k)
			continue
		}
		e[k] = v
	}
	data, err := json.Marshal(e)
	if err != nil {
		panic(err)
	}
	return data
}

const drop = "\x00drop"

// reminder returns the top-level keys of a reminder event written an hour
// after the base fail event, with more keys overridden.
func reminder(more map[string]any) map[string]any {
	top := map[string]any{
		"id": "20261123T110215Z-main-api-health-reminder", "transition": "reminder", "from": "fail",
		"at": "2026-11-23T11:02:15Z", "since": "2026-11-23T10:02:00Z",
	}
	for k, v := range more {
		top[k] = v
	}
	return top
}

func TestFromOnecheckText(t *testing.T) {
	long := strings.Repeat("я", 500)
	manyParams := map[string]any{}
	for i := 0; i < 25; i++ {
		manyParams[fmt.Sprintf("p%02d", i)] = i
	}
	var manyFailures []map[string]any
	for i := 0; i < 12; i++ {
		manyFailures = append(manyFailures, map[string]any{"message": fmt.Sprintf("f%d", i)})
	}
	var manyFields []map[string]any
	for i := 0; i < 12; i++ {
		manyFields = append(manyFields, map[string]any{"path": fmt.Sprintf("f%d", i), "operator": "equals", "actual": i})
	}
	var manyParamLines []string
	for i := 0; i < 20; i++ {
		manyParamLines = append(manyParamLines, fmt.Sprintf("  p%02d: %d", i, i))
	}

	cases := []struct {
		name   string
		top    map[string]any
		result map[string]any
		title  string
		level  string
		text   string
	}{
		{
			name:  "fail with nested params",
			title: "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\n" +
				"params:\n" +
				"  headers.Authorization: Bearer ${API_TOKEN}\n" +
				"  url: https://example.com/health\n" +
				"tags: api, web\n" +
				"at: 2026-11-23 10:02:00 UTC\n" +
				"failed 3 runs in a row:\n" +
				"- status: expected equals 200, got 503",
		},
		{
			name:   "first failure",
			result: map[string]any{"consecutive": 1},
			title:  "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\nparams:\n  headers.Authorization: Bearer ${API_TOKEN}\n  url: https://example.com/health\n" +
				"tags: api, web\nat: 2026-11-23 10:02:00 UTC\nfailed:\n- status: expected equals 200, got 503",
		},
		{
			name:   "check could not run",
			top:    map[string]any{"params": nil},
			result: map[string]any{"status": "error", "error": "module \"http\" timed out after 10s", "failures": []any{}},
			title:  "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\ntags: api, web\nat: 2026-11-23 10:02:00 UTC\nerror: module \"http\" timed out after 10s",
		},
		{
			name: "recovered has no failure lines",
			top: map[string]any{"id": "20261123T101400Z-main-api-health-recovered", "transition": "recovered",
				"at": "2026-11-23T10:14:00Z", "since": "2026-11-23T10:14:00Z"},
			result: map[string]any{"status": "pass", "state": "pass", "failures": []any{}, "consecutive": 2},
			title:  "[web-01] RECOVERED main/api-health", level: "ok",
			text: "module: http\nparams:\n  headers.Authorization: Bearer ${API_TOKEN}\n  url: https://example.com/health\n" +
				"tags: api, web\nat: 2026-11-23 10:14:00 UTC",
		},
		{
			// A param named like a field of the text must not read as one:
			// "state: failed" in a recovery is the rule's param, not its state.
			name: "params that look like status lines stay inside the params block",
			top: map[string]any{"id": "20261123T101400Z-main-api-health-recovered", "transition": "recovered",
				"at": "2026-11-23T10:14:00Z", "params": map[string]any{"unit": "nginx", "state": "failed"}},
			result: map[string]any{"status": "pass", "state": "pass", "failures": []any{}, "module": "systemd", "tags": nil},
			title:  "[web-01] RECOVERED main/api-health", level: "ok",
			text: "module: systemd\nparams:\n  state: failed\n  unit: nginx\nat: 2026-11-23 10:14:00 UTC",
		},
		{
			name:  "event of an older onecheck without params",
			top:   map[string]any{"params": drop, "redacted": drop},
			title: "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\ntags: api, web\nat: 2026-11-23 10:02:00 UTC\n" +
				"failed 3 runs in a row:\n- status: expected equals 200, got 503",
		},
		{
			name:  "redacted failure shows no params and no reason",
			top:   map[string]any{"params": nil, "redacted": true},
			title: "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\ntags: api, web\nat: 2026-11-23 10:02:00 UTC",
		},
		{
			name:  "redacted wins even when params are present",
			top:   map[string]any{"redacted": true},
			title: "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\ntags: api, web\nat: 2026-11-23 10:02:00 UTC",
		},
		{
			name:   "no tags",
			top:    map[string]any{"params": map[string]any{"path": "/"}},
			result: map[string]any{"tags": []string{}, "module": "disk"},
			title:  "[web-01] FAIL main/api-health", level: "error",
			text: "module: disk\nparams:\n  path: /\nat: 2026-11-23 10:02:00 UTC\n" +
				"failed 3 runs in a row:\n- status: expected equals 200, got 503",
		},
		{
			name:   "more params than fit",
			top:    map[string]any{"params": manyParams},
			result: map[string]any{"tags": nil, "failures": []any{}},
			title:  "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\nparams:\n" + strings.Join(manyParamLines, "\n") + "\n  … and 5 more params\n" +
				"at: 2026-11-23 10:02:00 UTC",
		},
		{
			name:   "more failures than fit",
			top:    map[string]any{"params": nil},
			result: map[string]any{"tags": nil, "failures": manyFailures},
			title:  "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\nat: 2026-11-23 10:02:00 UTC\nfailed 3 runs in a row:\n" +
				"- f0\n- f1\n- f2\n- f3\n- f4\n- f5\n- f6\n- f7\n- f8\n- f9\n… and 2 more",
		},
		{
			name: "failed fields instead of the message",
			top:  map[string]any{"params": nil},
			result: map[string]any{"tags": nil, "failures": []map[string]any{{
				"path": `containers.#(state=="running")#.2`, "operator": "any",
				"message": `containers.#(state=="running")#.2: expected any [...], got {"name":"smoke-tls-proxy"}`,
				"fields": []map[string]any{{
					"path": `containers.#(state=="running")#.2.name`, "operator": "in", "actual": "smoke-tls-proxy",
				}},
			}}},
			title: "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\nat: 2026-11-23 10:02:00 UTC\nfailed 3 runs in a row:\n" +
				`- containers.#(state=="running")#.2.name = "smoke-tls-proxy"`,
		},
		{
			name: "no fields falls back to the message",
			top:  map[string]any{"params": nil},
			result: map[string]any{"tags": nil, "failures": []map[string]any{
				{"message": "old onecheck"},
				{"message": "empty fields", "fields": []any{}},
				{"message": "unusable fields", "fields": []map[string]any{{"path": " ", "actual": 1}}},
			}},
			title: "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\nat: 2026-11-23 10:02:00 UTC\nfailed 3 runs in a row:\n" +
				"- old onecheck\n- empty fields\n- unusable fields",
		},
		{
			name: "missing fields, json paths and value kinds",
			top:  map[string]any{"params": nil},
			result: map[string]any{"tags": nil, "failures": []map[string]any{{
				"message": "long",
				"fields": []map[string]any{
					{"path": "a.b", "operator": "exists", "actual": nil, "missing": true},
					{"path": "c", "operator": "equals", "actual": nil},
					{"path": "body", "json_path": "status", "operator": "equals", "actual": "degraded"},
					{"path": "body", "json_path": "id", "operator": "exists", "actual": nil, "missing": true},
					{"path": "obj", "operator": "equals", "actual": map[string]any{"k": "<v>&"}},
					{"path": "multi", "operator": "equals", "actual": "a\nb"},
					{"path": "big", "operator": "equals", "actual": json.Number("12345678901")},
					{"path": "frac", "operator": "equals", "actual": json.Number("1.50")},
				},
			}}},
			title: "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\nat: 2026-11-23 10:02:00 UTC\nfailed 3 runs in a row:\n" +
				"- a.b: missing\n- c = null\n- body json status = \"degraded\"\n- body json id: missing\n" +
				"- obj = {\"k\":\"<v>&\"}\n- multi = \"a\\nb\"\n- big = 12345678901\n- frac = 1.50",
		},
		{
			name: "duplicate lines are printed once",
			top:  map[string]any{"params": nil},
			result: map[string]any{"tags": nil, "failures": []map[string]any{
				{"message": "one", "fields": []map[string]any{{"path": "x", "actual": 1}}},
				{"message": "two", "fields": []map[string]any{{"path": "x", "actual": 1}, {"path": "x", "actual": 2}}},
				{"message": "same"},
				{"message": "same"},
			}},
			title: "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\nat: 2026-11-23 10:02:00 UTC\nfailed 3 runs in a row:\n" +
				"- x = 1\n- x = 2\n- same",
		},
		{
			name:   "more fields than fit",
			top:    map[string]any{"params": nil},
			result: map[string]any{"tags": nil, "failures": []map[string]any{{"message": "m", "fields": manyFields}}},
			title:  "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\nat: 2026-11-23 10:02:00 UTC\nfailed 3 runs in a row:\n" +
				"- f0 = 0\n- f1 = 1\n- f2 = 2\n- f3 = 3\n- f4 = 4\n- f5 = 5\n- f6 = 6\n- f7 = 7\n- f8 = 8\n- f9 = 9\n… and 2 more",
		},
		{
			name: "redacted hides failed fields",
			top:  map[string]any{"params": nil, "redacted": true},
			result: map[string]any{"tags": nil, "failures": []map[string]any{
				{"message": "m", "fields": []map[string]any{{"path": "secret", "actual": "s3cr3t"}}},
			}},
			title: "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\nat: 2026-11-23 10:02:00 UTC",
		},
		{
			name: "long multi-line failure and long param are cut",
			top:  map[string]any{"params": map[string]any{"body": long}},
			result: map[string]any{"tags": nil, "failures": []map[string]any{
				{"message": "body: got \"line one\nline two\r\n\tthree " + long + "\""}}},
			title: "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\nparams:\n  body: " + strings.Repeat("я", 200) + "…\nat: 2026-11-23 10:02:00 UTC\n" +
				"failed 3 runs in a row:\n- " +
				"body: got \"line one line two   three " + strings.Repeat("я", 300-len("body: got \"line one line two   three ")) + "…",
		},
		{
			name: "param kinds",
			top: map[string]any{"params": map[string]any{
				"argv": []any{"sh", "-c", "echo <ok> & exit"}, "n": 1.5, "big": 12345678901, "flag": true, "none": nil,
				"empty": map[string]any{}, "deep": map[string]any{"a": map[string]any{"b": "c"}},
				"mixed": []any{map[string]any{"k": "v"}, 2}, "multi": "a\nb",
			}},
			result: map[string]any{"tags": nil, "failures": []any{}},
			title:  "[web-01] FAIL main/api-health", level: "error",
			text: "module: http\n" +
				"params:\n" +
				"  argv: [\"sh\",\"-c\",\"echo <ok> & exit\"]\n" +
				"  big: 12345678901\n" +
				"  deep.a.b: c\n" +
				"  empty: {}\n" +
				"  flag: true\n" +
				"  mixed: [{\"k\":\"v\"},2]\n" +
				"  multi: a b\n" +
				"  n: 1.5\n" +
				"  none: null\n" +
				"at: 2026-11-23 10:02:00 UTC",
		},
		{
			name:   "reminder is a failure that goes on",
			top:    reminder(nil),
			result: map[string]any{"consecutive": 63},
			title:  "[web-01] STILL FAILING main/api-health", level: "error",
			text: "module: http\nparams:\n  headers.Authorization: Bearer ${API_TOKEN}\n  url: https://example.com/health\n" +
				"tags: api, web\nat: 2026-11-23 11:02:15 UTC\nfailing since: 2026-11-23 10:02:00 UTC\n" +
				"failed 63 runs in a row:\n- status: expected equals 200, got 503",
		},
		{
			name:   "reminder of a check that cannot run",
			top:    reminder(map[string]any{"params": nil}),
			result: map[string]any{"status": "error", "error": "timed out", "failures": []any{}},
			title:  "[web-01] STILL FAILING main/api-health", level: "error",
			text: "module: http\ntags: api, web\nat: 2026-11-23 11:02:15 UTC\n" +
				"failing since: 2026-11-23 10:02:00 UTC\nerror: timed out",
		},
		{
			name:  "redacted reminder keeps the time the problem began",
			top:   reminder(map[string]any{"redacted": true}),
			title: "[web-01] STILL FAILING main/api-health", level: "error",
			text: "module: http\ntags: api, web\nat: 2026-11-23 11:02:15 UTC\n" +
				"failing since: 2026-11-23 10:02:00 UTC",
		},
		{
			name:   "reminder without since",
			top:    reminder(map[string]any{"since": drop, "params": nil}),
			result: map[string]any{"tags": nil},
			title:  "[web-01] STILL FAILING main/api-health", level: "error",
			text: "module: http\nat: 2026-11-23 11:02:15 UTC\n" +
				"failed 3 runs in a row:\n- status: expected equals 200, got 503",
		},
		{
			name:  "reminder without a result",
			top:   reminder(map[string]any{"result": nil, "params": nil}),
			title: "[web-01] STILL FAILING main/api-health", level: "error",
			text: "at: 2026-11-23 11:02:15 UTC\nfailing since: 2026-11-23 10:02:00 UTC",
		},
		{
			name:   "unknown transition",
			top:    map[string]any{"transition": "flapping", "params": nil},
			result: map[string]any{"tags": nil},
			title:  "[web-01] FLAPPING main/api-health", level: "info",
			text: "module: http\nat: 2026-11-23 10:02:00 UTC",
		},
		{
			name:  "no result at all",
			top:   map[string]any{"result": nil, "params": nil},
			title: "[web-01] FAIL main/api-health", level: "error",
			text: "at: 2026-11-23 10:02:00 UTC",
		},
		{
			name:  "no config name",
			top:   map[string]any{"config": "", "result": nil, "params": nil, "at": drop},
			title: "[web-01] FAIL api-health", level: "error",
			text: "",
		},
	}
	for _, c := range cases {
		m, err := FromOnecheck(ev(c.top, c.result), "web-01", time.Hour, now)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if m.Title != c.title || m.Level != c.level {
			t.Errorf("%s: title=%q level=%q", c.name, m.Title, m.Level)
		}
		if m.Text != c.text {
			t.Errorf("%s: text:\n%s\n--- want:\n%s", c.name, m.Text, c.text)
		}
		if strings.Contains(m.Text, "SECRET BODY") {
			t.Errorf("%s: saved output leaked into the text", c.name)
		}
		if !utf8.ValidString(m.Text) || !utf8.ValidString(m.Title) {
			t.Errorf("%s: invalid UTF-8", c.name)
		}
	}
}

func TestFromOnecheckFields(t *testing.T) {
	m, err := FromOnecheck(ev(nil, nil), "web-01", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != 1 || m.ID != "20261123T100200Z-main-api-health-fail" || m.Source != "onecheck" || m.Host != "web-01" {
		t.Errorf("m = %+v", m)
	}
	if strings.Join(m.Tags, ",") != "api,web" {
		t.Errorf("tags = %v", m.Tags)
	}
	at := time.Date(2026, 11, 23, 10, 2, 0, 0, time.UTC)
	if !m.CreatedAt.Equal(at) || !m.ExpiresAt.Equal(at.Add(24*time.Hour)) {
		t.Errorf("created=%v expires=%v", m.CreatedAt, m.ExpiresAt)
	}

	m, _ = FromOnecheck(ev(map[string]any{"expires_at": drop}, nil), "h", 2*time.Hour, now)
	if !m.ExpiresAt.Equal(at.Add(2 * time.Hour)) {
		t.Errorf("without expires_at: %v", m.ExpiresAt)
	}
	m, _ = FromOnecheck(ev(map[string]any{"expires_at": drop, "at": drop}, nil), "h", 2*time.Hour, now)
	if !m.CreatedAt.Equal(now.Truncate(time.Second)) || !m.ExpiresAt.Equal(m.CreatedAt.Add(2*time.Hour)) {
		t.Errorf("without at: created=%v expires=%v", m.CreatedAt, m.ExpiresAt)
	}
	m, _ = FromOnecheck(ev(nil, map[string]any{"tags": nil}), "h", time.Hour, now)
	if m.Tags == nil {
		t.Error("tags must be an empty list, not null")
	}
}

func TestFromOnecheckRejects(t *testing.T) {
	for name, data := range map[string][]byte{
		"not json":       []byte("{oops"),
		"empty":          nil,
		"array":          []byte(`[1,2]`),
		"string":         []byte(`"event"`),
		"number":         []byte(`42`),
		"null":           []byte(`null`),
		"no id":          ev(map[string]any{"id": drop}, nil),
		"no rule":        ev(map[string]any{"rule": ""}, nil),
		"no transition":  ev(map[string]any{"transition": drop}, nil),
		"params is text": ev(map[string]any{"params": "x"}, nil),
		"result is text": ev(map[string]any{"result": "x"}, nil),
		"bad time":       ev(map[string]any{"at": "yesterday"}, nil),
	} {
		if _, err := FromOnecheck(data, "h", time.Hour, now); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestTitleIsAlwaysOneLine(t *testing.T) {
	m, err := FromOnecheck(ev(map[string]any{"rule": "a\nb", "config": "c\td"}, nil), "h", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "[h] FAIL c d/a b" {
		t.Errorf("title = %q", m.Title)
	}
}

func TestHostileIDIsOneLine(t *testing.T) {
	m, err := FromOnecheck(ev(map[string]any{"id": "20261123T100200Z-x\nSENT a forged\x1b[31m"}, nil), "h", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(m.ID, "\n\x1b") {
		t.Errorf("id = %q", m.ID)
	}
	if _, err := FromOnecheck(ev(map[string]any{"id": "\n\t "}, nil), "h", time.Hour, now); err == nil {
		t.Error("an id of white space only must be rejected")
	}
}

func TestEventWithADateThatCannotBeStoredIsRejected(t *testing.T) {
	_, err := FromOnecheck(ev(map[string]any{"at": "9999-12-31T23:59:59Z", "expires_at": drop}, nil), "h", 48*time.Hour, now)
	if err == nil {
		t.Error("want an error: created_at + ttl is past year 9999")
	}
}
