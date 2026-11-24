package message

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits of one output field shown in the text of an event.
const (
	maxOutputLines = 20
	maxOutputChars = 1000 // characters, line breaks between the lines included
)

// escapeSequence matches what a terminal would not print: colours and cursor
// movement (CSI) and window titles (OSC).
var escapeSequence = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)?`)

// outputLines prints the saved output of a check: for every field the rule
// allowed to show, in the rule's order, a line "<field>:" and the end of
// what the field holds. A field with nothing in it is left out.
//
// Unlike everything else in the text of an event, output keeps its line
// breaks: it is read as the lines a command printed.
func outputLines(fields []string, output map[string]any) []string {
	var lines []string
	seen := map[string]bool{}
	for _, field := range fields {
		name := oneLine(field)
		if name == "" || seen[field] {
			continue
		}
		seen[field] = true
		content := tail(fieldLines(output[field]))
		if len(content) == 0 {
			continue
		}
		lines = append(lines, name+":")
		lines = append(lines, content...)
	}
	return lines
}

// fieldLines turns the value of an output field into lines: a text as it
// is, cleaned; an object as "key: value" lines, like params; anything else
// as one line.
func fieldLines(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return cleanOutput(v)
	case map[string]any:
		var lines []string
		flatten("", v, &lines)
		sort.Strings(lines)
		return lines
	}
	return []string{scalar(value)}
}

// cleanOutput splits what a command printed into lines a chat can show:
// without escape sequences, with a line break where a terminal would start
// a line anew, with spaces for tabs and other control characters, without
// spaces at line ends and without empty lines at the start and the end.
func cleanOutput(s string) []string {
	s = escapeSequence.ReplaceAllString(strings.ToValidUTF8(s, ""), "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// tail keeps the end of lines within the limits of a field: what went wrong
// is at the end of an output, and onecheck saves the end too. Whole lines
// are dropped from the start and marked by a line "…"; a single line that
// is still too long keeps its end after a "…".
func tail(lines []string) []string {
	total := len(lines)
	if total > maxOutputLines {
		lines = lines[total-maxOutputLines:]
	}
	chars := len(lines) - 1
	for _, line := range lines {
		chars += utf8.RuneCountInString(line)
	}
	for len(lines) > 1 && chars > maxOutputChars {
		chars -= utf8.RuneCountInString(lines[0]) + 1
		lines = lines[1:]
	}
	if chars > maxOutputChars {
		runes := []rune(lines[0])
		return []string{"…" + string(runes[len(runes)-maxOutputChars:])}
	}
	if len(lines) < total {
		return append([]string{"…"}, lines...)
	}
	return lines
}
