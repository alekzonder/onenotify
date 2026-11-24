package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alekzonder/onenotify/internal/deliver"
)

var report = &deliver.Report{
	Ingested: 1,
	Records: []deliver.Record{
		{Channel: "ops-telegram", Module: "telegram", ID: "20261123T100200Z-main-api-health-fail", Outcome: deliver.Sent, DurationMS: 312},
		{Channel: "admin-mail", Module: "mail", ID: "20261123T100200Z-main-api-health-fail", Outcome: deliver.Waiting, DurationMS: 10042,
			Error: "dial tcp 10.0.0.5:587: connection refused"},
		{Channel: "phone", Module: "ntfy", ID: "20261123T090000Z-send-4f2a9c1e", Outcome: deliver.Failed,
			Error: "expired at 2026-11-24T09:00:00Z"},
	},
	Sent: 1, Waiting: 3, Failed: 1,
}

func TestWriteText(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, "text", report); err != nil {
		t.Fatal(err)
	}
	want := `SENT    ops-telegram  20261123T100200Z-main-api-health-fail  (telegram, 312ms)
WAIT    admin-mail    20261123T100200Z-main-api-health-fail  (mail, 10.0s)
        dial tcp 10.0.0.5:587: connection refused
FAILED  phone         20261123T090000Z-send-4f2a9c1e  (ntfy)
        expired at 2026-11-24T09:00:00Z

5 messages: 1 sent, 3 waiting, 1 failed
`
	if buf.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestWriteNothingToDo(t *testing.T) {
	var buf bytes.Buffer
	empty := &deliver.Report{Records: []deliver.Record{}}
	if err := Write(&buf, "text", empty); err != nil || buf.String() != "0 messages\n" {
		t.Errorf("out=%q err=%v", buf.String(), err)
	}
	if ExitCode(empty) != ExitOK {
		t.Error("an empty run must exit with 0")
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, "json", report); err != nil {
		t.Fatal(err)
	}
	var got deliver.Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Sent != 1 || got.Waiting != 3 || got.Failed != 1 || got.Ingested != 1 || len(got.Records) != 3 ||
		got.Records[1].Outcome != deliver.Waiting || got.Records[1].Error == "" {
		t.Errorf("got %+v", got)
	}
}

func TestExitCodeAndSummary(t *testing.T) {
	cases := []struct {
		r       deliver.Report
		code    int
		summary string
	}{
		{deliver.Report{Sent: 2}, 0, "2 messages: 2 sent, 0 waiting, 0 failed"},
		{deliver.Report{Sent: 1}, 0, "1 message: 1 sent, 0 waiting, 0 failed"},
		{deliver.Report{Sent: 1, Waiting: 1}, 1, "2 messages: 1 sent, 1 waiting, 0 failed"},
		{deliver.Report{Failed: 1}, 1, "1 message: 0 sent, 0 waiting, 1 failed"},
	}
	for _, c := range cases {
		if got := ExitCode(&c.r); got != c.code {
			t.Errorf("%+v: exit %d", c.r, got)
		}
		if got := Summary(&c.r); got != c.summary {
			t.Errorf("%+v: %q", c.r, got)
		}
	}
}

func TestControlCharactersDoNotReachTheTerminal(t *testing.T) {
	var buf bytes.Buffer
	r := &deliver.Report{Waiting: 1, Records: []deliver.Record{{Channel: "a", Module: "m", Outcome: deliver.Waiting,
		ID: "id\nSENT    a  forged", Error: "bad \x1b[31mred\r\nnext\xff"}}}
	if err := Write(&buf, "text", r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "\n") != 4 || strings.ContainsAny(out, "\x1b\r") || strings.Contains(out, "\nSENT") {
		t.Errorf("out = %q", out)
	}
}

func TestRecordWithoutAModule(t *testing.T) {
	var buf bytes.Buffer
	r := &deliver.Report{Failed: 1, Records: []deliver.Record{{Channel: "old", ID: "x", Outcome: deliver.Failed, Error: "expired"}}}
	if err := Write(&buf, "text", r); err != nil || !strings.HasPrefix(buf.String(), "FAILED  old  x\n        expired\n") {
		t.Errorf("out=%q err=%v", buf.String(), err)
	}
}
