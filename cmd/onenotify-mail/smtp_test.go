package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeOptions script a fakeSMTP server.
type fakeOptions struct {
	// extensions are the EHLO lines, "AUTH PLAIN" when nil. STARTTLS is
	// added by startTLS.
	extensions []string
	// startTLS offers and implements STARTTLS.
	startTLS bool
	// implicitTLS speaks TLS from the first byte.
	implicitTLS bool
	// silent accepts connections and never says a word.
	silent bool
	// replies override the reply to a command. The key is a prefix of the
	// command line, compared without case: "RCPT TO:<bad@example.com>",
	// "MAIL", "AUTH". The key "." is the end of DATA. The longest matching
	// key wins.
	replies map[string]string
}

// fakeRecord is what the server saw.
type fakeRecord struct {
	connections int
	commands    []string // command verbs in order, "." for the end of DATA
	helo        string
	mailFrom    string
	rcpts       []string // every RCPT TO address, accepted or not
	auth        []string // mechanism, then the decoded client answers
	authTLS     bool     // TLS was active when AUTH arrived
	tls         bool     // TLS was active at some point
	data        string   // the raw message, dot-stuffing removed
	dataCount   int
}

func (r fakeRecord) saw(verb string) bool {
	for _, c := range r.commands {
		if c == verb {
			return true
		}
	}
	return false
}

type fakeSMTP struct {
	opts fakeOptions
	ln   net.Listener
	done chan struct{}
	wg   sync.WaitGroup

	mu    sync.Mutex
	rec   fakeRecord
	conns []net.Conn
}

func newFakeSMTP(t *testing.T, opts fakeOptions) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{opts: opts, ln: ln, done: make(chan struct{})}
	s.wg.Add(1)
	go s.acceptLoop()
	t.Cleanup(s.close)
	return s
}

func (s *fakeSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeSMTP) record() fakeRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rec
	r.commands = append([]string(nil), r.commands...)
	r.rcpts = append([]string(nil), r.rcpts...)
	r.auth = append([]string(nil), r.auth...)
	return r
}

func (s *fakeSMTP) update(f func(r *fakeRecord)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.rec)
}

func (s *fakeSMTP) close() {
	close(s.done)
	_ = s.ln.Close()
	s.mu.Lock()
	for _, c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *fakeSMTP) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.rec.connections++
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { _ = conn.Close() }()
			s.serve(conn)
		}()
	}
}

// fakeSession is one connection.
type fakeSession struct {
	s    *fakeSMTP
	conn net.Conn
	r    *bufio.Reader
	tls  bool
}

func (s *fakeSMTP) serve(conn net.Conn) {
	if s.opts.silent {
		<-s.done
		return
	}
	// A client that hangs must not hang the test run.
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	sess := &fakeSession{s: s, conn: conn}
	if s.opts.implicitTLS {
		if !sess.upgrade() {
			return
		}
	}
	sess.r = bufio.NewReader(sess.conn)
	sess.reply("220 fake.example.com ESMTP")
	for {
		line, ok := sess.readLine()
		if !ok || !sess.handle(line) {
			return
		}
	}
}

func (c *fakeSession) upgrade() bool {
	tlsConn := tls.Server(c.conn, &tls.Config{Certificates: []tls.Certificate{testCertificate()}})
	if err := tlsConn.Handshake(); err != nil {
		return false
	}
	c.conn = tlsConn
	c.r = bufio.NewReader(tlsConn)
	c.tls = true
	c.s.update(func(r *fakeRecord) { r.tls = true })
	return true
}

func (c *fakeSession) reply(lines ...string) {
	_, _ = c.conn.Write([]byte(strings.Join(lines, "\r\n") + "\r\n"))
}

func (c *fakeSession) readLine() (string, bool) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", false
	}
	return strings.TrimRight(line, "\r\n"), true
}

// override returns the scripted reply for a command line, if there is one.
func (c *fakeSession) override(line string) (string, bool) {
	best, reply := -1, ""
	for prefix, r := range c.s.opts.replies {
		if len(prefix) > best && len(line) >= len(prefix) && strings.EqualFold(line[:len(prefix)], prefix) {
			best, reply = len(prefix), r
		}
	}
	return reply, best >= 0
}

// handle answers one command and reports whether the session goes on.
func (c *fakeSession) handle(line string) bool {
	verb, arg, _ := strings.Cut(line, " ")
	verb = strings.ToUpper(verb)
	c.s.update(func(r *fakeRecord) {
		r.commands = append(r.commands, verb)
		switch verb {
		case "EHLO", "HELO":
			r.helo = arg
		case "MAIL":
			r.mailFrom = angleAddr(arg)
		case "RCPT":
			r.rcpts = append(r.rcpts, angleAddr(arg))
		case "AUTH":
			r.authTLS = c.tls
		}
	})
	if reply, ok := c.override(line); ok && verb != "AUTH" {
		c.reply(reply)
		return true
	}
	switch verb {
	case "EHLO":
		c.ehlo()
	case "STARTTLS":
		if !c.s.opts.startTLS || c.tls {
			c.reply("502 not available")
			return true
		}
		c.reply("220 go ahead")
		return c.upgrade()
	case "AUTH":
		return c.auth(line, arg)
	case "DATA":
		return c.data()
	case "QUIT":
		c.reply("221 bye")
		return false
	default: // HELO, MAIL, RCPT, RSET, NOOP
		c.reply("250 ok")
	}
	return true
}

func (c *fakeSession) ehlo() {
	exts := c.s.opts.extensions
	if exts == nil {
		exts = []string{"AUTH PLAIN"}
	}
	lines := []string{"250-fake.example.com"}
	if c.s.opts.startTLS && !c.tls {
		lines = append(lines, "250-STARTTLS")
	}
	for _, e := range exts {
		lines = append(lines, "250-"+e)
	}
	c.reply(append(lines, "250 HELP")...)
}

// auth plays AUTH PLAIN and AUTH LOGIN and records what the client sent.
func (c *fakeSession) auth(line, arg string) bool {
	mech, initial, _ := strings.Cut(arg, " ")
	mech = strings.ToUpper(mech)
	answers := []string{mech}
	if initial != "" {
		answers = append(answers, unbase64(initial))
	}
	if mech == "LOGIN" {
		for _, prompt := range []string{"Username:", "Password:"} {
			c.reply("334 " + base64.StdEncoding.EncodeToString([]byte(prompt)))
			answer, ok := c.readLine()
			if !ok {
				return false
			}
			answers = append(answers, unbase64(answer))
		}
	}
	c.s.update(func(r *fakeRecord) { r.auth = answers })
	if reply, ok := c.override(line); ok {
		c.reply(reply)
		return true
	}
	c.reply("235 authenticated")
	return true
}

// data reads the message up to the lone dot, keeping the bytes as they came.
func (c *fakeSession) data() bool {
	c.reply("354 go ahead")
	var b strings.Builder
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return false
		}
		if line == ".\r\n" {
			break
		}
		b.WriteString(strings.TrimPrefix(line, "."))
	}
	c.s.update(func(r *fakeRecord) {
		r.commands = append(r.commands, ".")
		r.data = b.String()
		r.dataCount++
	})
	if reply, ok := c.override("."); ok {
		c.reply(reply)
		return true
	}
	c.reply("250 queued")
	return true
}

func angleAddr(arg string) string {
	i, j := strings.IndexByte(arg, '<'), strings.IndexByte(arg, '>')
	if i < 0 || j < i {
		return arg
	}
	return arg[i+1 : j]
}

func unbase64(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "not base64: " + s
	}
	return string(b)
}

var (
	testCertOnce sync.Once
	testCert     tls.Certificate
)

// testCertificate returns a self-signed certificate for 127.0.0.1 that no
// client trusts.
func testCertificate() tls.Certificate {
	testCertOnce.Do(func() {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			panic(err)
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject:      pkix.Name{CommonName: "fake smtp"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			DNSNames:     []string{"localhost"},
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			panic(err)
		}
		testCert = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	})
	return testCert
}
