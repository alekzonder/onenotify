// Package output renders the report of a run and derives the exit code.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/alekzonder/onenotify/internal/deliver"
)

// Exit codes that follow from a report.
const (
	ExitOK      = 0
	ExitPending = 1 // something is still waiting or will never be delivered
)

// ExitCode is 0 when the run left nothing undelivered.
func ExitCode(r *deliver.Report) int {
	if r.Waiting > 0 || r.Failed > 0 {
		return ExitPending
	}
	return ExitOK
}

// Summary is the last line of the text output.
func Summary(r *deliver.Report) string {
	total := r.Total()
	if total == 0 {
		return "0 messages"
	}
	noun := "messages"
	if total == 1 {
		noun = "message"
	}
	return fmt.Sprintf("%d %s: %d sent, %d waiting, %d failed", total, noun, r.Sent, r.Waiting, r.Failed)
}

var labels = map[deliver.Outcome]string{
	deliver.Sent:    "SENT",
	deliver.Waiting: "WAIT",
	deliver.Failed:  "FAILED",
}

// Write prints the report as "text" or "json".
func Write(w io.Writer, format string, r *deliver.Report) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	width := 0
	for _, rec := range r.Records {
		width = max(width, len(rec.Channel))
	}
	for _, rec := range r.Records {
		detail := rec.Module
		if rec.Module != "" && (rec.Outcome != deliver.Failed || rec.DurationMS > 0) {
			detail = fmt.Sprintf("%s, %s", rec.Module, duration(rec.DurationMS))
		}
		if detail != "" {
			detail = "  (" + detail + ")"
		}
		if _, err := fmt.Fprintf(w, "%-7s %-*s  %s%s\n", labels[rec.Outcome], width, rec.Channel, printable(rec.ID), detail); err != nil {
			return err
		}
		if rec.Error != "" {
			if _, err := fmt.Fprintf(w, "        %s\n", printable(rec.Error)); err != nil {
				return err
			}
		}
	}
	if len(r.Records) > 0 {
		fmt.Fprintln(w)
	}
	_, err := fmt.Fprintln(w, Summary(r))
	return err
}

// printable keeps text that came from outside (an event, a provider's
// answer) from breaking the line it is printed on or driving the terminal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "?"))
}

func duration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}
