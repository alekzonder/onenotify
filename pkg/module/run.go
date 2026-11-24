package module

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
	"unicode/utf8"
)

// SendFunc delivers one message. A nil error means the provider accepted it.
// Wrap an error with Permanent when the provider rejected the content of this
// very message, and with RetryAfter when it asked to slow down.
type SendFunc func(ctx context.Context, req Request) error

// Module is a complete delivery module: its description and its sender.
type Module struct {
	Description Description
	Send        SendFunc
}

// Main runs the module with the process arguments and standard streams and
// exits. Call it from func main.
func Main(m Module) {
	os.Exit(Run(m, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// Run executes one module invocation and returns the process exit code.
func Run(m Module, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "describe":
		d := m.Description
		d.Protocol = ProtocolVersion
		return writeJSON(stdout, stderr, d)
	case "send":
		return runSend(m, stdin, stdout, stderr)
	default:
		name := m.Description.Name
		fmt.Fprintf(stderr, "onenotify-%s is a onenotify module and is normally started by onenotify.\n\n"+
			"usage:\n  onenotify-%s describe   print module description as JSON\n"+
			"  onenotify-%s send       read a request from stdin, deliver its message, print the outcome as JSON\n", name, name, name)
		return 2
	}
}

func runSend(m Module, stdin io.Reader, stdout, stderr io.Writer) int {
	var req Request
	if err := json.NewDecoder(stdin).Decode(&req); err != nil {
		fmt.Fprintf(stderr, "invalid request: %v\n", err)
		return 2
	}
	if req.Protocol != ProtocolVersion {
		fmt.Fprintf(stderr, "unsupported protocol %d (module speaks protocol %d)\n", req.Protocol, ProtocolVersion)
		return 2
	}
	ctx := context.Background()
	if req.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	resp := Response{Delivered: true}
	if err := m.Send(ctx, req); err != nil {
		resp = Response{Error: toError(err)}
	}
	return writeJSON(stdout, stderr, resp)
}

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

type retryAfterError struct {
	err   error
	after time.Duration
}

func (e *retryAfterError) Error() string { return e.err.Error() }
func (e *retryAfterError) Unwrap() error { return e.err }

// Permanent marks err as a rejection of this very message: onenotify will not
// retry it. Use it only when the provider refused the content. A wrong token,
// an unknown recipient or a server that is down are not permanent: the
// administrator fixes the config and the message goes out.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err}
}

// RetryAfter marks err as "the channel must not be used for d", for a
// provider that answers with a rate limit.
func RetryAfter(err error, d time.Duration) error {
	if err == nil {
		return nil
	}
	return &retryAfterError{err, d}
}

func toError(err error) *Error {
	out := &Error{Message: err.Error()}
	var p *permanentError
	if errors.As(err, &p) {
		out.Permanent = true
	}
	var r *retryAfterError
	if errors.As(err, &r) && r.after > 0 {
		out.RetryAfterMS = r.after.Milliseconds()
	}
	return out
}

// DecodeParams decodes a channel's params into v. Unknown keys are errors.
func DecodeParams(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

const ellipsis = "…"

// Truncate cuts s to at most max bytes without splitting a character. A cut
// string ends with "…" when there is room for it. Invalid UTF-8 in s is
// dropped, cut or not, so the result can always be sent as text.
func Truncate(s string, max int) string {
	s = toValid(s)
	if len(s) <= max {
		return s
	}
	if max <= 0 {
		return ""
	}
	room := max - len(ellipsis)
	suffix := ellipsis
	if room < 0 {
		room, suffix = max, ""
	}
	for room > 0 && !utf8.RuneStart(s[room]) {
		room--
	}
	return s[:room] + suffix
}

func toValid(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return string(bytes.ToValidUTF8([]byte(s), nil))
}

func writeJSON(stdout, stderr io.Writer, v any) int {
	if err := json.NewEncoder(stdout).Encode(v); err != nil {
		fmt.Fprintf(stderr, "cannot write response: %v\n", err)
		return 1
	}
	return 0
}
