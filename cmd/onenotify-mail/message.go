package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
	"unicode"

	"github.com/alekzonder/onenotify/pkg/module"
)

const (
	// foldAt is the line length header lines are folded at when possible.
	foldAt = 78
	// maxLine is the longest line RFC 5322 allows, without the CRLF.
	maxLine = 998
	// maxMessageIDLocal keeps the Message-ID header on one legal line.
	maxMessageIDLocal = 200
)

// buildMessage returns the complete RFC 5322 message: headers, an empty line
// and the quoted-printable body. Every line ends with CRLF and the whole
// message is 7-bit ASCII.
func buildMessage(msg module.Message, from *mail.Address, to []*mail.Address, now time.Time) []byte {
	recipients := make([]string, len(to))
	for i, a := range to {
		recipients[i] = a.String()
	}
	var b bytes.Buffer
	header := func(name, value string) {
		b.WriteString(name + ": " + value + "\r\n")
	}
	header("From", from.String())
	// One address per line: a long list must not exceed the line limit.
	header("To", strings.Join(recipients, ",\r\n "))
	header("Subject", unstructured("Subject", msg.Title))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", messageID(msg.ID, from.Address, now))
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	header("X-Onenotify-Level", unstructured("X-Onenotify-Level", msg.Level))
	header("X-Onenotify-Host", unstructured("X-Onenotify-Host", msg.Host))
	b.WriteString("\r\n")
	b.Write(encodeBody(msg.Text))
	return b.Bytes()
}

// unstructured returns value as the body of the unstructured header name. The
// result never carries a line break that is not a fold, so a value cannot add
// a header of its own. Non-ASCII text is RFC 2047 encoded.
func unstructured(name, value string) string {
	value = cleanHeaderText(value)
	if !isASCII(value) {
		// Encoded words hold no spaces: the spaces mime puts between them
		// are the only ones, and each is a place to fold.
		return strings.ReplaceAll(mime.QEncoding.Encode("utf-8", value), " ", "\r\n ")
	}
	// "=?" could be read as the start of an encoded word by the receiver.
	if !strings.Contains(value, "=?") {
		if folded, ok := foldASCII(len(name)+len(": "), value); ok {
			return folded
		}
	}
	return encodeASCIIWords(value)
}

// cleanHeaderText drops invalid UTF-8 and turns every control character,
// CR and LF included, into a space.
func cleanHeaderText(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// foldASCII folds value at spaces so that lines stay within foldAt where the
// words allow it. Unfolding gives value back unchanged. It reports false when
// a line would still be longer than maxLine.
func foldASCII(prefixLen int, value string) (string, bool) {
	var b strings.Builder
	lineLen := prefixLen
	start := 0
	flush := func(end int) bool {
		tok := value[start:end]
		if lineLen > 0 && start > 0 && lineLen+len(tok) > foldAt {
			b.WriteString("\r\n")
			lineLen = 0
		}
		b.WriteString(tok)
		lineLen += len(tok)
		start = end
		return lineLen <= maxLine
	}
	for i := 1; i+1 < len(value); i++ {
		// A fold goes before the last space of a run, so that the next
		// line starts with one space followed by a word.
		if value[i] == ' ' && value[i+1] != ' ' {
			if !flush(i) {
				return "", false
			}
		}
	}
	if !flush(len(value)) {
		return "", false
	}
	return b.String(), true
}

// encodeASCIIWords encodes an ASCII value as RFC 2047 Q encoded words, one
// per line. It is for the values mime.QEncoding leaves as they are but which
// cannot be sent raw.
func encodeASCIIWords(value string) string {
	const chunk = 20 // at most 60 encoded characters per word
	var words []string
	for len(value) > 0 {
		n := min(chunk, len(value))
		var w strings.Builder
		w.WriteString("=?utf-8?q?")
		for i := 0; i < n; i++ {
			c := value[i]
			switch {
			case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
				w.WriteByte(c)
			case c == ' ':
				w.WriteByte('_')
			default:
				fmt.Fprintf(&w, "=%02X", c)
			}
		}
		w.WriteString("?=")
		words = append(words, w.String())
		value = value[n:]
	}
	return strings.Join(words, "\r\n ")
}

// messageID builds "<id@domain>" from the message id and the domain of the
// sender address, keeping only characters that are legal in a Message-ID.
func messageID(id, fromAddr string, now time.Time) string {
	local := strings.Trim(dotAtom(id), ".")
	if len(local) > maxMessageIDLocal {
		local = strings.Trim(local[:maxMessageIDLocal], ".")
	}
	if local == "" {
		local = now.UTC().Format("20060102T150405Z") + "-" + randomHex()
	}
	domain := fromAddr
	if i := strings.LastIndexByte(fromAddr, '@'); i >= 0 {
		domain = fromAddr[i+1:]
	}
	domain = strings.Trim(dotAtom(domain), ".")
	if domain == "" {
		domain = "localhost"
	}
	return "<" + local + "@" + domain + ">"
}

// dotAtom replaces everything except letters, digits, "-", "_" and single
// dots with "_".
func dotAtom(s string) string {
	var b strings.Builder
	prevDot := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_'
		if c == '.' && !prevDot {
			ok = true
		}
		prevDot = c == '.'
		if !ok {
			c = '_'
		}
		b.WriteByte(c)
	}
	return b.String()
}

func randomHex() string {
	var buf [8]byte
	// rand.Read never fails.
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// encodeBody returns text quoted-printable encoded with CRLF line endings.
// Invalid UTF-8 is dropped. A text that does not end with a line break ends
// with a soft one, so decoding gives back exactly the text.
func encodeBody(text string) []byte {
	text = strings.ToValidUTF8(text, "")
	if text == "" {
		return nil
	}
	var b bytes.Buffer
	w := quotedprintable.NewWriter(&b)
	// Writes to a bytes.Buffer do not fail.
	_, _ = w.Write([]byte(text))
	_ = w.Close()
	if !bytes.HasSuffix(b.Bytes(), []byte("\r\n")) {
		b.WriteString("=\r\n")
	}
	return b.Bytes()
}
