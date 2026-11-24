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
	"unicode"

	"github.com/alekzonder/onenotify/internal/version"
	"github.com/alekzonder/onenotify/pkg/module"
)

const (
	defaultURL = "https://ntfy.sh"

	// maxMessageBytes is ntfy's default limit for a message body.
	maxMessageBytes = 4096
	// maxBodyBytes keeps the whole JSON request below what ntfy reads for
	// a JSON publish (twice the message limit, 8192 bytes by default).
	// JSON escaping makes a 4096-byte text with many quotes, line breaks
	// or control characters longer than that, and ntfy would answer 413.
	maxBodyBytes = 8000
	// maxTitleBytes keeps an absurdly long title from eating the body.
	maxTitleBytes = 1024
	// maxErrorDetailBytes limits how much of the server's answer goes
	// into the error text.
	maxErrorDetailBytes = 200
	// maxRetryAfter caps a Retry-After a broken proxy might send.
	maxRetryAfter = 24 * time.Hour
	// fallbackTimeout applies only when the request carries no timeout.
	fallbackTimeout = 30 * time.Second

	defaultPriority = 3
	redacted        = "***"
)

type params struct {
	URL      string `json:"url"`
	Topic    string `json:"topic"`
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
	Priority *int   `json:"priority"`
}

// publication is the body of ntfy's JSON publish API.
type publication struct {
	Topic    string   `json:"topic"`
	Title    string   `json:"title,omitempty"`
	Message  string   `json:"message"`
	Priority int      `json:"priority"`
	Tags     []string `json:"tags,omitempty"`
}

// target is where and as whom to publish.
type target struct {
	url      string
	token    string
	username string
	password string
}

// secrets lists the values that must never show up in an error text.
func (t target) secrets() []string {
	return []string{t.token, t.password}
}

func send(ctx context.Context, req module.Request) error {
	var p params
	if err := module.DecodeParams(req.Params, &p); err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	t, err := newTarget(p)
	if err != nil {
		return err
	}
	if p.Topic == "" {
		return errors.New("ntfy: param topic is required")
	}
	if p.Priority != nil && (*p.Priority < 1 || *p.Priority > 5) {
		return errors.New("ntfy: param priority must be between 1 and 5")
	}
	body, err := encodeBody(newPublication(p, req.Message), req.Message.Text)
	if err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	if err := post(ctx, t, body); err != nil {
		return redact(err, t.secrets())
	}
	return nil
}

// newTarget checks the address and the credentials. The error never quotes
// the url param: it may hold credentials.
func newTarget(p params) (target, error) {
	raw := p.URL
	if raw == "" {
		raw = defaultURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return target{}, errors.New("ntfy: param url must be an http:// or https:// address")
	}
	if p.Token != "" && p.Username != "" {
		return target{}, errors.New("ntfy: params token and username cannot be used together")
	}
	if (p.Username == "") != (p.Password == "") {
		return target{}, errors.New("ntfy: params username and password must be given together")
	}
	t := target{token: p.Token, username: p.Username, password: p.Password}
	// Credentials written into the url (https://user:pass@host) are taken
	// out of it, so they cannot get into an error text, and used only when
	// the params give no other credentials.
	if u.User != nil {
		if t.token == "" && t.username == "" {
			t.username = u.User.Username()
			t.password, _ = u.User.Password()
		}
		u.User = nil
	}
	// The JSON API is served at the root of the server.
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	u.RawPath = ""
	u.Fragment = ""
	t.url = u.String()
	return t, nil
}

// levelLook returns the ntfy priority and the tag (an emoji short code that
// ntfy shows as an icon before the title) for a message level.
func levelLook(level string) (priority int, tag string) {
	switch level {
	case module.LevelError:
		return 4, "rotating_light"
	case module.LevelWarning:
		return 3, "warning"
	case module.LevelOK:
		return 3, "white_check_mark"
	case module.LevelInfo:
		return 2, "information_source"
	}
	return defaultPriority, ""
}

// newPublication builds everything but the message text.
func newPublication(p params, m module.Message) publication {
	priority, levelTag := levelLook(m.Level)
	if p.Priority != nil {
		priority = *p.Priority
	}
	return publication{
		Topic:    p.Topic,
		Title:    module.Truncate(oneLine(m.Title), maxTitleBytes),
		Priority: priority,
		Tags:     tags(levelTag, m.Tags),
	}
}

// tags puts the level tag first and the message's own tags after it. ntfy
// keeps tags as a comma-separated list, so a comma inside a tag would split
// it in two: commas become spaces. Empty and repeated tags are skipped.
func tags(levelTag string, own []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, tag := range append([]string{levelTag}, own...) {
		tag = strings.TrimSpace(oneLine(strings.ReplaceAll(tag, ",", " ")))
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

// oneLine drops invalid UTF-8 and turns control characters into spaces.
func oneLine(s string) string {
	s = strings.ToValidUTF8(s, "")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// encodeBody adds the text to pub and returns the JSON request body.
//
// The text is cut to ntfy's message limit, and further when JSON escaping
// makes the request too long, so a message is never rejected for its size.
//
// An empty text: ntfy replaces an empty message with the word "triggered",
// which under a title like "[web-01] OK main/api" reads as noise. The title
// is therefore sent as the message too. The title itself is kept, so such a
// notification looks like every other one from the channel (bold title,
// level icon) and only repeats its one line in the body.
func encodeBody(pub publication, text string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		text = pub.Title
	}
	pub.Message = module.Truncate(text, maxMessageBytes)
	body, err := marshal(pub)
	if err != nil || len(body) <= maxBodyBytes {
		return body, err
	}
	// Too long once escaped: find the longest cut that fits. With an empty
	// message the body is sent as it is, there is nothing left to cut.
	fits, tooLong := 0, maxMessageBytes
	for {
		limit := (fits + tooLong) / 2
		pub.Message = module.Truncate(text, limit)
		if body, err = marshal(pub); err != nil {
			return nil, err
		}
		if limit == fits {
			return body, nil
		}
		if len(body) <= maxBodyBytes {
			fits = limit
		} else {
			tooLong = limit
		}
	}
}

// marshal encodes without HTML escaping: "<" as "<" only wastes the
// size limit.
func marshal(pub publication) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(pub); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// client does not follow redirects: a redirected POST is repeated as GET,
// and the 200 of the server's web page would pass for a delivery.
var client = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func post(ctx context.Context, t target, body []byte) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, fallbackTimeout)
		defer cancel()
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("User-Agent", "onenotify-ntfy/"+version.Version)
	switch {
	case t.token != "":
		hreq.Header.Set("Authorization", "Bearer "+t.token)
	case t.username != "":
		hreq.SetBasicAuth(t.username, t.password)
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return classify(resp, answer)
}

// classify turns the server's answer into the module's outcome. Only 413 is
// permanent: the server refused this very message for its size. Everything
// else, a wrong token or an unknown topic included, is fixed by the
// administrator or by time, and the message must wait for that.
func classify(resp *http.Response, answer []byte) error {
	code := resp.StatusCode
	if code >= 200 && code <= 299 {
		return nil
	}
	text := "ntfy: " + strconv.Itoa(code)
	if s := http.StatusText(code); s != "" {
		text += " " + s
	}
	if detail := errorDetail(resp, answer); detail != "" {
		text += ": " + detail
	}
	err := errors.New(text)
	switch code {
	case http.StatusRequestEntityTooLarge:
		return module.Permanent(err)
	case http.StatusTooManyRequests:
		if d := retryAfter(resp.Header.Get("Retry-After")); d > 0 {
			return module.RetryAfter(err, d)
		}
	}
	return err
}

// errorDetail extracts what the server said: the "error" field of ntfy's
// JSON answer, or where a redirect points.
func errorDetail(resp *http.Response, answer []byte) string {
	if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
		if loc, err := url.Parse(resp.Header.Get("Location")); err == nil && loc.Host != "" {
			return "redirect to " + loc.Scheme + "://" + loc.Host + loc.Path + ", set it as the url param"
		}
		return "unexpected redirect"
	}
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(answer, &e) != nil {
		return ""
	}
	return module.Truncate(strings.TrimSpace(oneLine(e.Error)), maxErrorDetailBytes)
}

// retryAfter reads a Retry-After header given in seconds.
func retryAfter(v string) time.Duration {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	if n > int64(maxRetryAfter/time.Second) {
		return maxRetryAfter
	}
	return time.Duration(n) * time.Second
}

// redactedError replaces the text of an error and keeps its classification.
type redactedError struct {
	text string
	err  error
}

func (e *redactedError) Error() string { return e.text }
func (e *redactedError) Unwrap() error { return e.err }

// redact is the last line of defence: whatever text an error got, the
// secrets are cut out of it.
func redact(err error, secrets []string) error {
	text := err.Error()
	clean := text
	for _, s := range secrets {
		if s != "" {
			clean = strings.ReplaceAll(clean, s, redacted)
		}
	}
	if clean == text {
		return err
	}
	return &redactedError{text: clean, err: err}
}
