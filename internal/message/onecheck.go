package message

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alekzonder/onenotify/pkg/module"
)

// Limits of the text built from an event.
const (
	maxParamValue = 200 // characters of one param value
	maxParamLines = 20
	maxFailures   = 10
	maxErrorLine  = 300 // characters of an error or a failure message
)

const textTime = "2006-01-02 15:04:05 UTC"

// event is the part of a onecheck event onenotify reads. Unknown keys are
// ignored, so newer versions of onecheck keep working.
type event struct {
	ID         string         `json:"id"`
	Config     string         `json:"config"`
	Rule       string         `json:"rule"`
	Transition string         `json:"transition"`
	At         *time.Time     `json:"at"`
	ExpiresAt  *time.Time     `json:"expires_at"`
	Params     map[string]any `json:"params"`
	Redacted   bool           `json:"redacted"`
	Result     *struct {
		Module      string   `json:"module"`
		Tags        []string `json:"tags"`
		Status      string   `json:"status"`
		Consecutive int      `json:"consecutive"`
		Failures    []struct {
			Message string `json:"message"`
		} `json:"failures"`
		Error *string `json:"error"`
	} `json:"result"`
}

// FromOnecheck builds a message from the content of a onecheck event file.
// An error means the content is not a usable event.
//
// What goes into the text is fixed: module, rule params as onecheck wrote
// them, tags, time and, for a failure only, the error or the failed
// conditions. Saved output never does. An event marked "redacted" gives only
// module, tags and time.
func FromOnecheck(data []byte, host string, ttl time.Duration, now time.Time) (Message, error) {
	var ev event
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // keep numbers in params as they were written
	if err := dec.Decode(&ev); err != nil {
		return Message{}, fmt.Errorf("not a onecheck event: %w", err)
	}
	switch {
	case ev.ID == "":
		return Message{}, errors.New(`not a onecheck event: no "id"`)
	case ev.Rule == "":
		return Message{}, errors.New(`not a onecheck event: no "rule"`)
	case ev.Transition == "":
		return Message{}, errors.New(`not a onecheck event: no "transition"`)
	}

	m := Message{
		Version:   Version,
		ID:        ev.ID,
		Source:    SourceOnecheck,
		Host:      host,
		Tags:      []string{},
		CreatedAt: now.UTC().Truncate(time.Second),
	}
	if ev.At != nil && !ev.At.IsZero() {
		m.CreatedAt = ev.At.UTC()
	}
	m.ExpiresAt = m.CreatedAt.Add(ttl)
	if ev.ExpiresAt != nil && !ev.ExpiresAt.IsZero() {
		m.ExpiresAt = ev.ExpiresAt.UTC()
	}

	word := strings.ToUpper(ev.Transition)
	switch ev.Transition {
	case "fail":
		m.Level = module.LevelError
	case "recovered":
		m.Level = module.LevelOK
	default:
		m.Level = module.LevelInfo
	}
	name := ev.Rule
	if ev.Config != "" {
		name = ev.Config + "/" + ev.Rule
	}
	m.Title = withHost(host, word+" "+name)

	var lines []string
	if ev.Result != nil {
		if ev.Result.Module != "" {
			lines = append(lines, "module: "+oneLine(ev.Result.Module))
		}
		for _, tag := range ev.Result.Tags {
			if tag = oneLine(tag); tag != "" {
				m.Tags = append(m.Tags, tag)
			}
		}
	}
	if !ev.Redacted {
		lines = append(lines, paramLines(ev.Params)...)
	}
	if len(m.Tags) > 0 {
		lines = append(lines, "tags: "+strings.Join(m.Tags, ", "))
	}
	if ev.At != nil && !ev.At.IsZero() {
		lines = append(lines, "at: "+ev.At.UTC().Format(textTime))
	}
	if ev.Transition == "fail" && !ev.Redacted {
		lines = append(lines, failureLines(&ev)...)
	}
	m.Text = strings.Join(lines, "\n")
	return m, nil
}

// failureLines says why the rule failed: the error of a check that could not
// run, or the conditions that did not hold.
func failureLines(ev *event) []string {
	res := ev.Result
	if res == nil {
		return nil
	}
	if res.Status == "error" {
		if res.Error == nil || oneLine(*res.Error) == "" {
			return nil
		}
		return []string{"error: " + cut(oneLine(*res.Error), maxErrorLine)}
	}
	if len(res.Failures) == 0 {
		return nil
	}
	head := "failed:"
	if res.Consecutive > 1 {
		head = fmt.Sprintf("failed %d runs in a row:", res.Consecutive)
	}
	lines := []string{head}
	for i, f := range res.Failures {
		if i == maxFailures {
			lines = append(lines, fmt.Sprintf("… and %d more", len(res.Failures)-maxFailures))
			break
		}
		lines = append(lines, "- "+cut(oneLine(f.Message), maxErrorLine))
	}
	return lines
}

// paramLines prints rule params one per line as "key: value", sorted by key.
// Nested objects become "a.b.c"; anything else that is not a scalar is
// printed as compact JSON.
func paramLines(params map[string]any) []string {
	var lines []string
	flatten("", params, &lines)
	sort.Strings(lines)
	if len(lines) > maxParamLines {
		more := len(lines) - maxParamLines
		lines = append(lines[:maxParamLines:maxParamLines], fmt.Sprintf("… and %d more params", more))
	}
	return lines
}

func flatten(prefix string, obj map[string]any, lines *[]string) {
	for key, value := range obj {
		name := oneLine(key)
		if prefix != "" {
			name = prefix + "." + name
		}
		if nested, ok := value.(map[string]any); ok && len(nested) > 0 {
			flatten(name, nested, lines)
			continue
		}
		*lines = append(*lines, name+": "+cut(scalar(value), maxParamValue))
	}
}

func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return oneLine(x)
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "?"
	}
	return oneLine(buf.String())
}
