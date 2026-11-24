// Package message defines the notification onenotify queues and delivers,
// and builds it from its two sources: a onecheck event and "onenotify send".
package message

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
	"unicode"

	"github.com/alekzonder/onenotify/pkg/module"
)

// Version is the version of the queue file format.
const Version = 1

// Sources of a message.
const (
	SourceOnecheck = "onecheck"
	SourceSend     = "send"
)

// Message is one notification, as it lies in a channel's queue.
type Message struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Source  string `json:"source"`
	Host    string `json:"host"`
	Level   string `json:"level"`
	// Title is one line and already starts with "[<host>] ".
	Title     string    `json:"title"`
	Text      string    `json:"text"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt is the delivery deadline.
	ExpiresAt time.Time `json:"expires_at"`
}

// Wire returns the part of the message a module receives.
func (m Message) Wire() module.Message {
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	return module.Message{
		ID: m.ID, Source: m.Source, Host: m.Host, Level: m.Level,
		Title: m.Title, Text: m.Text, Tags: tags, CreatedAt: m.CreatedAt,
	}
}

// ValidLevel reports whether s is a message level.
func ValidLevel(s string) bool {
	switch s {
	case module.LevelError, module.LevelOK, module.LevelWarning, module.LevelInfo:
		return true
	}
	return false
}

const idTime = "20060102T150405Z"

// NewSend builds the message of "onenotify send".
func NewSend(host, title, text, level string, tags []string, now time.Time, ttl time.Duration) Message {
	now = now.UTC()
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	if tags == nil {
		tags = []string{}
	}
	return Message{
		Version:   Version,
		ID:        now.Format(idTime) + "-send-" + hex.EncodeToString(suffix[:]),
		Source:    SourceSend,
		Host:      host,
		Level:     level,
		Title:     withHost(host, title),
		Text:      strings.ToValidUTF8(text, ""),
		Tags:      tags,
		CreatedAt: now.Truncate(time.Second),
		ExpiresAt: now.Add(ttl).Truncate(time.Second),
	}
}

func withHost(host, title string) string {
	return "[" + host + "] " + oneLine(title)
}

// oneLine makes s safe to use as a single line: line breaks, tabs and other
// control characters become spaces, invalid UTF-8 is dropped.
func oneLine(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// cut shortens s to at most max characters, marking the cut with "…".
func cut(s string, max int) string {
	n := 0
	for i := range s {
		if n == max {
			return s[:i] + "…"
		}
		n++
	}
	return s
}
