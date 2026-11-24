// Package module is the SDK for onenotify delivery modules.
//
// A module is a standalone executable named onenotify-<name>. onenotify
// starts it once per message and exchanges JSON over stdin/stdout. A module
// only delivers: what to send, to which channel and with which text is
// decided by onenotify itself.
package module

import (
	"encoding/json"
	"time"
)

// ProtocolVersion is the version of the JSON protocol implemented by this SDK.
const ProtocolVersion = 1

// Message levels. A module uses the level to choose how a message looks in
// its channel: an icon, a priority, a sound.
const (
	LevelError   = "error"
	LevelOK      = "ok"
	LevelWarning = "warning"
	LevelInfo    = "info"
)

// Description is the answer to "onenotify-<name> describe".
type Description struct {
	Protocol     int             `json:"protocol"`
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	Description  string          `json:"description"`
	ParamsSchema json.RawMessage `json:"params_schema,omitempty"`
}

// Message is one notification to deliver.
type Message struct {
	// ID is unique and starts with a UTC timestamp.
	ID string `json:"id"`
	// Source says where the message came from: "onecheck" or "send".
	Source string `json:"source"`
	Host   string `json:"host"`
	Level  string `json:"level"`
	// Title is one line and already carries the host name.
	Title string `json:"title"`
	// Text is plain text of any number of lines; it may be empty.
	Text      string    `json:"text"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
}

// Request is sent on stdin of "onenotify-<name> send".
type Request struct {
	Protocol int `json:"protocol"`
	// Channel is the name of the channel in the config, for the module's
	// own log messages.
	Channel string `json:"channel"`
	// Params is the channel's "params", already validated against the
	// module's schema.
	Params    json.RawMessage `json:"params"`
	Message   Message         `json:"message"`
	TimeoutMS int64           `json:"timeout_ms"`
}

// Error reports that a message was not delivered.
type Error struct {
	Message string `json:"message"`
	// Permanent means this very message can never be delivered through
	// this channel, so retrying is pointless. Everything else, including a
	// wrong token or an unknown recipient, is temporary.
	Permanent bool `json:"permanent,omitempty"`
	// RetryAfterMS asks onenotify not to use the channel for this long.
	RetryAfterMS int64 `json:"retry_after_ms,omitempty"`
}

// Response is written to stdout by "onenotify-<name> send".
type Response struct {
	Delivered bool   `json:"delivered"`
	Error     *Error `json:"error"`
}
