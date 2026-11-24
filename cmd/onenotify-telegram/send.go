package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/alekzonder/onenotify/internal/version"
	"github.com/alekzonder/onenotify/pkg/module"
)

const (
	defaultAPIURL = "https://api.telegram.org"

	// textLimit is Telegram's limit for a message, in UTF-16 code units.
	// Telegram counts the text after parsing entities; the module keeps the
	// escaped text within the limit, which is never less strict.
	textLimit = 4096

	ellipsis = "…"

	// maxReply bounds how much of the reply is read.
	maxReply = 1 << 20
	// maxDescription bounds the provider's description in an error text.
	maxDescription = 300
	// maxRetryAfter bounds the pause a reply may ask for.
	maxRetryAfter = 24 * time.Hour
	// fallbackTimeout applies when the request carries no timeout_ms.
	fallbackTimeout = 60 * time.Second
)

// contentErrors are the descriptions of a 400 reply that reject the content
// of this very message. Only they make an error permanent.
var contentErrors = []string{
	"message is too long",
	"can't parse entities",
	"message text is empty",
}

type params struct {
	Token           string          `json:"token"`
	ChatID          json.RawMessage `json:"chat_id"`
	MessageThreadID *int64          `json:"message_thread_id"`
	SilentOK        bool            `json:"silent_ok"`
	APIURL          string          `json:"api_url"`
}

type linkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

type sendMessage struct {
	ChatID              json.RawMessage    `json:"chat_id"`
	MessageThreadID     *int64             `json:"message_thread_id,omitempty"`
	Text                string             `json:"text"`
	ParseMode           string             `json:"parse_mode"`
	DisableNotification bool               `json:"disable_notification,omitempty"`
	LinkPreviewOptions  linkPreviewOptions `json:"link_preview_options"`
}

type apiReply struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter int64 `json:"retry_after"`
	} `json:"parameters"`
}

func send(ctx context.Context, req module.Request) error {
	var p params
	if err := module.DecodeParams(req.Params, &p); err != nil {
		return err
	}
	if err := checkParams(&p); err != nil {
		return err
	}
	body, err := json.Marshal(sendMessage{
		ChatID:              p.ChatID,
		MessageThreadID:     p.MessageThreadID,
		Text:                formatText(req.Message),
		ParseMode:           "HTML",
		DisableNotification: p.SilentOK && req.Message.Level == module.LevelOK,
		LinkPreviewOptions:  linkPreviewOptions{IsDisabled: true},
	})
	if err != nil {
		return failure(p.Token, "cannot encode the request: "+err.Error())
	}

	endpoint := strings.TrimRight(p.APIURL, "/") + "/bot" + p.Token + "/sendMessage"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return failure(p.Token, "invalid api_url or token: "+withoutURL(err))
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "onenotify-telegram/"+version.Version)

	client := &http.Client{Timeout: fallbackTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return failure(p.Token, "request failed: "+withoutURL(err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return failure(p.Token, fmt.Sprintf("cannot read the reply (%d %s): %s",
			resp.StatusCode, http.StatusText(resp.StatusCode), withoutURL(err)))
	}
	return classify(p.Token, resp, data)
}

// checkParams repeats what the schema demands, for a module started by hand,
// and fills in defaults.
func checkParams(p *params) error {
	if p.Token == "" {
		return errors.New("invalid params: token is required")
	}
	chat := bytes.TrimSpace(p.ChatID)
	if len(chat) == 0 || string(chat) == "null" {
		return errors.New("invalid params: chat_id is required")
	}
	if c := chat[0]; c != '"' && c != '-' && (c < '0' || c > '9') {
		return errors.New("invalid params: chat_id must be a string or an integer")
	}
	if p.APIURL == "" {
		p.APIURL = defaultAPIURL
	}
	if !strings.HasPrefix(p.APIURL, "http://") && !strings.HasPrefix(p.APIURL, "https://") {
		return errors.New("invalid params: api_url must start with http:// or https://")
	}
	return nil
}

// classify turns a Bot API reply into the outcome of the delivery.
func classify(token string, resp *http.Response, data []byte) error {
	var reply apiReply
	parsed := json.Unmarshal(data, &reply) == nil
	if resp.StatusCode == http.StatusOK && parsed && reply.OK {
		return nil
	}
	status := fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	if resp.StatusCode == http.StatusOK {
		return failure(token, "unexpected reply: "+status+", but not a Bot API success")
	}
	text := status
	if parsed && reply.Description != "" {
		// A description already starts with the status text:
		// "Bad Request: chat not found".
		text = fmt.Sprintf("%d %s", resp.StatusCode, module.Truncate(reply.Description, maxDescription))
	}
	err := failure(token, text)
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return module.RetryAfter(err, retryAfter(reply.Parameters.RetryAfter, resp.Header.Get("Retry-After")))
	case http.StatusBadRequest:
		desc := strings.ToLower(reply.Description)
		for _, phrase := range contentErrors {
			if strings.Contains(desc, phrase) {
				return module.Permanent(err)
			}
		}
	}
	return err
}

// retryAfter is the pause asked for by a 429 reply: parameters.retry_after,
// or else the Retry-After header. Zero means the reply named no pause.
func retryAfter(seconds int64, header string) time.Duration {
	if seconds <= 0 {
		seconds, _ = strconv.ParseInt(strings.TrimSpace(header), 10, 64)
	}
	if seconds <= 0 {
		return 0
	}
	if seconds > int64(maxRetryAfter/time.Second) {
		return maxRetryAfter
	}
	return time.Duration(seconds) * time.Second
}

// failure builds the module's error. The token is cut out of the text,
// wherever it came from.
func failure(token, text string) error {
	text = "telegram: " + text
	if token != "" {
		text = strings.ReplaceAll(text, token, "<token>")
		if escaped := url.PathEscape(token); escaped != token {
			text = strings.ReplaceAll(text, escaped, "<token>")
		}
	}
	return errors.New(text)
}

// withoutURL is the text of an error of net/http without the request URL,
// which holds the token.
func withoutURL(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return uerr.Err.Error()
	}
	return err.Error()
}

func icon(level string) string {
	switch level {
	case module.LevelError:
		return "🔴"
	case module.LevelOK:
		return "🟢"
	case module.LevelWarning:
		return "🟡"
	default:
		return "ℹ️"
	}
}

// formatText renders a message as Telegram HTML: an icon, the title in bold,
// an empty line, the text. The result is at most textLimit UTF-16 code
// units: the text is cut first, and the title only when it does not fit on
// its own.
func formatText(m module.Message) string {
	head := icon(m.Level)
	if title := cleanTitle(m.Title); title != "" {
		const open, closing = " <b>", "</b>"
		room := textLimit - utf16Len(head) - len(open) - len(closing)
		head += open + escapeCut(title, room) + closing
	}
	text := strings.TrimRight(clean(m.Text), " \t\n")
	const gap = "\n\n"
	room := textLimit - utf16Len(head) - len(gap)
	if text == "" || room < 1 {
		return head
	}
	return head + gap + escapeCut(text, room)
}

// clean makes s safe to send: invalid UTF-8 becomes U+FFFD, line ends become
// "\n", control characters other than "\n" and "\t" are dropped.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == '\r':
			return '\n'
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			return -1
		}
		return r
	}, s)
}

// cleanTitle is clean for the title, which is one line.
func cleanTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		return r
	}, clean(s))
	return strings.TrimSpace(s)
}

// escaped is how a character looks in Telegram HTML.
func escaped(r rune) string {
	switch r {
	case '&':
		return "&amp;"
	case '<':
		return "&lt;"
	case '>':
		return "&gt;"
	}
	return string(r)
}

// escapeCut escapes s for Telegram HTML and keeps the result within limit
// UTF-16 code units. It cuts between characters of s, so never inside an
// entity or a surrogate pair; a cut result ends with "…". s must be valid
// UTF-8.
func escapeCut(s string, limit int) string {
	var b strings.Builder
	used := 0
	// fit is the length of b at the last point where "…" still fits.
	fit := 0
	for _, r := range s {
		e := escaped(r)
		used += utf16Len(e)
		if used > limit {
			if limit < 1 {
				return ""
			}
			return strings.TrimRight(b.String()[:fit], " \t\n") + ellipsis
		}
		b.WriteString(e)
		if used < limit {
			fit = b.Len()
		}
	}
	return b.String()
}

// utf16Len is the length of s in UTF-16 code units.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}
