package message

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 23, 10, 2, 0, 500, time.UTC)

func TestNewSend(t *testing.T) {
	m := NewSend("web-01", "backup\nfailed\t now", "line 1\nline 2\xff", "error", []string{"a"}, now, 2*time.Hour)
	if !regexp.MustCompile(`^20261123T100200Z-send-[0-9a-f]{8}$`).MatchString(m.ID) {
		t.Errorf("id = %q", m.ID)
	}
	if m.Title != "[web-01] backup failed  now" || m.Text != "line 1\nline 2" {
		t.Errorf("title=%q text=%q", m.Title, m.Text)
	}
	if m.Version != 1 || m.Source != "send" || m.Host != "web-01" || m.Level != "error" || len(m.Tags) != 1 {
		t.Errorf("m = %+v", m)
	}
	if !m.CreatedAt.Equal(now.Truncate(time.Second)) || m.ExpiresAt.Sub(m.CreatedAt) != 2*time.Hour {
		t.Errorf("created=%v expires=%v", m.CreatedAt, m.ExpiresAt)
	}
	other := NewSend("web-01", "t", "", "info", nil, now, time.Hour)
	if other.ID == m.ID {
		t.Error("two messages of the same second must have different ids")
	}
	if other.Tags == nil || other.Wire().Tags == nil {
		t.Error("tags must be an empty list, not null")
	}
}

func TestWire(t *testing.T) {
	m := NewSend("h", "t", "x", "ok", []string{"a", "b"}, now, time.Hour)
	w := m.Wire()
	if w.ID != m.ID || w.Source != "send" || w.Host != "h" || w.Level != "ok" || w.Title != "[h] t" ||
		w.Text != "x" || strings.Join(w.Tags, ",") != "a,b" || !w.CreatedAt.Equal(m.CreatedAt) {
		t.Errorf("wire = %+v", w)
	}
}

func TestValidLevel(t *testing.T) {
	for _, l := range []string{"error", "ok", "warning", "info"} {
		if !ValidLevel(l) {
			t.Errorf("%s must be valid", l)
		}
	}
	for _, l := range []string{"", "fail", "ERROR", "loud"} {
		if ValidLevel(l) {
			t.Errorf("%q must not be valid", l)
		}
	}
}
