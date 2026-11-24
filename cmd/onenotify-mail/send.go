package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alekzonder/onenotify/pkg/module"
)

const (
	tlsStartTLS = "starttls"
	tlsImplicit = "tls"
	tlsNone     = "none"

	// defaultTimeout bounds the conversation when the request has no timeout.
	defaultTimeout = 30 * time.Second

	// codeTooBig is the SMTP reply "message exceeds the size limit", the
	// only rejection of the message itself.
	codeTooBig = 552
)

var (
	errNoStartTLS      = errors.New("the server does not offer STARTTLS; nothing was sent")
	errUnencryptedAuth = errors.New("refusing to send credentials over an unencrypted connection")
)

type params struct {
	Host               string   `json:"host"`
	Port               int      `json:"port"`
	TLS                string   `json:"tls"`
	Username           string   `json:"username"`
	Password           string   `json:"password"`
	From               string   `json:"from"`
	To                 []string `json:"to"`
	Helo               string   `json:"helo"`
	InsecureSkipVerify bool     `json:"insecure_skip_verify"`
}

// settings are the params with defaults applied and addresses parsed.
type settings struct {
	params
	from *mail.Address
	to   []*mail.Address
}

func send(ctx context.Context, req module.Request) error {
	var p params
	if err := module.DecodeParams(req.Params, &p); err != nil {
		return classify(fmt.Errorf("mail: %w", err), p)
	}
	s, err := resolve(p)
	if err != nil {
		return classify(err, p)
	}
	data := buildMessage(req.Message, s.from, s.to, time.Now())
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}
	return classify(deliver(ctx, s, data), p)
}

// classify turns err into the module's answer: permanent only when the
// server refused this very message for its size (552 in reply to MAIL, DATA
// or the message itself), temporary for everything else. A 552 in reply to
// RCPT means "that mailbox is full", which is not about the message and
// passes. The password never leaves the module.
func classify(err error, p params) error {
	if err == nil {
		return nil
	}
	out := errors.New(scrub(err.Error(), p.Username, p.Password))
	var reply *textproto.Error
	if !errors.As(err, &reply) || reply.Code != codeTooBig {
		return out
	}
	var at *smtpError
	if errors.As(err, &at) && strings.HasPrefix(at.stage, "RCPT") {
		return out
	}
	return module.Permanent(out)
}

// scrub is the last line of defence: it removes the password, in every form
// it is sent in, from an error text, and puts the text on one line.
func scrub(text, username, password string) string {
	if password != "" {
		for _, secret := range []string{
			base64.StdEncoding.EncodeToString([]byte("\x00" + username + "\x00" + password)),
			base64.StdEncoding.EncodeToString([]byte(password)),
			password,
		} {
			text = strings.ReplaceAll(text, secret, "***")
		}
	}
	return strings.Join(strings.Fields(text), " ")
}

// defaultPort is the port used when params do not name one.
func defaultPort(tlsMode string) int {
	switch tlsMode {
	case tlsImplicit:
		return 465
	case tlsNone:
		return 25
	default:
		return 587
	}
}

func defaultHelo() string {
	if name, err := os.Hostname(); err == nil && name != "" {
		return name
	}
	return "localhost"
}

func resolve(p params) (*settings, error) {
	if p.Host == "" {
		return nil, errors.New("mail: host is required")
	}
	switch p.TLS {
	case "":
		p.TLS = tlsStartTLS
	case tlsStartTLS, tlsImplicit, tlsNone:
	default:
		return nil, errors.New("mail: tls must be starttls, tls or none")
	}
	if p.Port == 0 {
		p.Port = defaultPort(p.TLS)
	}
	if p.Port < 1 || p.Port > 65535 {
		return nil, errors.New("mail: port must be between 1 and 65535")
	}
	if p.Password != "" && p.Username == "" {
		return nil, errors.New("mail: password requires username")
	}
	if p.Helo == "" {
		p.Helo = defaultHelo()
	}
	s := &settings{params: p}
	var err error
	if s.from, err = parseAddress("from", p.From); err != nil {
		return nil, err
	}
	if len(p.To) == 0 {
		return nil, errors.New("mail: to is required and must not be empty")
	}
	for _, raw := range p.To {
		addr, err := parseAddress("to", raw)
		if err != nil {
			return nil, err
		}
		s.to = append(s.to, addr)
	}
	return s, nil
}

// parseAddress accepts "user@example.com" and "Name <user@example.com>".
func parseAddress(param, raw string) (*mail.Address, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("mail: %s: an address is required", param)
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return nil, fmt.Errorf("mail: %s: invalid address %q: %s", param, raw, strings.TrimPrefix(err.Error(), "mail: "))
	}
	// The address goes into "MAIL FROM:<...>" as it is.
	if strings.ContainsAny(addr.Address, " <>\t\r\n") {
		return nil, fmt.Errorf("mail: %s: unsupported address %q", param, raw)
	}
	return addr, nil
}

// deliver runs one SMTP conversation. The connection is closed when ctx ends,
// so no step can outlive it.
func deliver(ctx context.Context, s *settings, data []byte) error {
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	var conn net.Conn
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return stageError(ctx, "connect", err)
	}
	defer func() { _ = conn.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	raw := conn
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()

	tlsConf := &tls.Config{
		ServerName:         s.Host,
		InsecureSkipVerify: s.InsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}
	if s.TLS == tlsImplicit {
		tlsConn := tls.Client(conn, tlsConf)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return stageError(ctx, "TLS handshake with "+addr, err)
		}
		conn = tlsConn
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return stageError(ctx, "greeting from "+addr, err)
	}
	stage, err := converse(c, s, tlsConf, data)
	if err != nil {
		var reply *textproto.Error
		if errors.As(err, &reply) {
			// The server answered, so the connection is usable: leave politely.
			_ = c.Quit()
		}
		return stageError(ctx, stage, err)
	}
	return nil
}

// stageError names the step of the conversation that failed.
func stageError(ctx context.Context, stage string, err error) error {
	var reply *textproto.Error
	switch {
	case errors.As(err, &reply):
		// The server's own words, as they came.
		return &smtpError{stage: stage, text: fmt.Sprintf("mail: %s: %d %s", stage, reply.Code, reply.Msg), err: err}
	case ctx.Err() != nil || errors.Is(err, os.ErrDeadlineExceeded):
		return fmt.Errorf("mail: %s: timed out", stage)
	}
	return fmt.Errorf("mail: %s: %w", stage, err)
}

// smtpError is an SMTP reply with the step it answered.
type smtpError struct {
	stage string
	text  string
	err   error
}

func (e *smtpError) Error() string { return e.text }
func (e *smtpError) Unwrap() error { return e.err }

// converse talks to the server up to the accepted message. On failure it
// returns the name of the step that failed.
func converse(c *smtp.Client, s *settings, tlsConf *tls.Config, data []byte) (string, error) {
	if err := c.Hello(s.Helo); err != nil {
		return "EHLO", err
	}
	encrypted := s.TLS == tlsImplicit
	if s.TLS == tlsStartTLS {
		// Never fall back to plain text: a server (or somebody in between)
		// that hides STARTTLS gets nothing.
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return "STARTTLS", errNoStartTLS
		}
		if err := c.StartTLS(tlsConf); err != nil {
			return "STARTTLS", err
		}
		encrypted = true
	}
	if s.Username != "" {
		if err := authenticate(c, s, encrypted); err != nil {
			return "AUTH", err
		}
	}
	return transfer(c, s, data)
}

func authenticate(c *smtp.Client, s *settings, encrypted bool) error {
	plainTextAllowed := s.TLS == tlsNone
	if !encrypted && !plainTextAllowed {
		return errUnencryptedAuth
	}
	ok, offered := c.Extension("AUTH")
	if !ok {
		return errors.New("the server does not offer authentication")
	}
	auth, err := chooseAuth(offered, s.Username, s.Password, plainTextAllowed)
	if err != nil {
		return err
	}
	return c.Auth(auth)
}

// chooseAuth picks PLAIN when the server offers it and LOGIN otherwise.
func chooseAuth(offered, username, password string, plainTextAllowed bool) (smtp.Auth, error) {
	login := false
	for _, mech := range strings.Fields(strings.ToUpper(offered)) {
		switch mech {
		case "PLAIN":
			return &plainAuth{username: username, password: password, plainTextAllowed: plainTextAllowed}, nil
		case "LOGIN":
			login = true
		}
	}
	if login {
		return &loginAuth{username: username, password: password, plainTextAllowed: plainTextAllowed}, nil
	}
	return nil, fmt.Errorf("the server offers neither PLAIN nor LOGIN authentication (it offers: %s)", offered)
}

// plainAuth is AUTH PLAIN. Unlike smtp.PlainAuth it can be told to work over
// a connection without TLS, for "tls: none" which the user chose explicitly.
type plainAuth struct {
	username, password string
	plainTextAllowed   bool
}

func (a *plainAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS && !a.plainTextAllowed {
		return "", nil, errUnencryptedAuth
	}
	return "PLAIN", []byte("\x00" + a.username + "\x00" + a.password), nil
}

func (a *plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected server challenge")
	}
	return nil, nil
}

// loginAuth is AUTH LOGIN, for servers that do not offer PLAIN.
type loginAuth struct {
	username, password string
	plainTextAllowed   bool
	step               int
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS && !a.plainTextAllowed {
		return "", nil, errUnencryptedAuth
	}
	a.step = 0
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(_ []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	a.step++
	switch a.step {
	case 1:
		return []byte(a.username), nil
	case 2:
		return []byte(a.password), nil
	default:
		return nil, errors.New("unexpected server challenge")
	}
}

// transfer sends the envelope and the message. One rejected recipient fails
// the whole delivery before DATA: a partial delivery would be repeated for
// the accepted recipients on retry.
func transfer(c *smtp.Client, s *settings, data []byte) (string, error) {
	if err := c.Mail(s.from.Address); err != nil {
		return "MAIL FROM <" + s.from.Address + ">", err
	}
	for _, rcpt := range s.to {
		if err := c.Rcpt(rcpt.Address); err != nil {
			return "RCPT TO <" + rcpt.Address + ">", err
		}
	}
	w, err := c.Data()
	if err != nil {
		return "DATA", err
	}
	if _, err := w.Write(data); err != nil {
		return "DATA", err
	}
	if err := w.Close(); err != nil {
		return "end of DATA", err
	}
	// The server has the message; a failed QUIT changes nothing.
	_ = c.Quit()
	return "", nil
}
