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
	ID         string     `json:"id"`
	Config     string     `json:"config"`
	Rule       string     `json:"rule"`
	Transition string     `json:"transition"`
	At         *time.Time `json:"at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	// Since is when the state the event reports began: for a reminder, the
	// time of the fail event.
	Since    *time.Time     `json:"since"`
	Params   map[string]any `json:"params"`
	Redacted bool           `json:"redacted"`
	// OutputFields names the keys of Result.Output the rule allows a
	// notification to show.
	OutputFields []string `json:"output_fields"`
	Result       *struct {
		Module      string   `json:"module"`
		Tags        []string `json:"tags"`
		Status      string   `json:"status"`
		Consecutive int      `json:"consecutive"`
		Failures    []struct {
			Message string `json:"message"`
			// Fields are the leaf fields the condition broke on; older
			// onecheck does not send them.
			Fields []failedField `json:"fields"`
		} `json:"failures"`
		Error  *string        `json:"error"`
		Output map[string]any `json:"output"`
	} `json:"result"`
}

// FromOnecheck builds a message from the content of a onecheck event file.
// An error means the content is not a usable event.
//
// What goes into the text is fixed: module, rule params as onecheck wrote
// them, tags, time and, for a failure and a reminder of one, the error or
// the failed conditions and the output fields the rule allows to show. A
// reminder also says since when the rule fails. An event marked "redacted"
// gives only module, tags and times.
func FromOnecheck(data []byte, host string, ttl time.Duration, now time.Time) (Message, error) {
	var ev event
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // keep numbers in params as they were written
	if err := dec.Decode(&ev); err != nil {
		return Message{}, fmt.Errorf("not a onecheck event: %w", err)
	}
	switch {
	case oneLine(ev.ID) == "":
		return Message{}, errors.New(`not a onecheck event: no "id"`)
	case ev.Rule == "":
		return Message{}, errors.New(`not a onecheck event: no "rule"`)
	case ev.Transition == "":
		return Message{}, errors.New(`not a onecheck event: no "transition"`)
	}

	m := Message{
		Version:   Version,
		ID:        oneLine(ev.ID),
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

	word, level := headline(ev.Transition)
	m.Level = level
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
	if ev.Transition == transitionReminder && ev.Since != nil && !ev.Since.IsZero() {
		lines = append(lines, "failing since: "+ev.Since.UTC().Format(textTime))
	}
	if ev.failing() && !ev.Redacted {
		lines = append(lines, failureLines(&ev)...)
		if ev.Result != nil {
			lines = append(lines, outputLines(ev.OutputFields, ev.Result.Output)...)
		}
	}
	m.Text = strings.Join(lines, "\n")
	// A message that cannot be written to a queue file is not a usable
	// event either: a date JSON cannot hold, for one.
	if _, err := json.Marshal(m); err != nil {
		return Message{}, fmt.Errorf("not a usable onecheck event: %w", err)
	}
	return m, nil
}

// Transitions of a onecheck event onenotify knows.
const (
	transitionFail      = "fail"
	transitionReminder  = "reminder"
	transitionRecovered = "recovered"
)

// headline gives the word a transition puts into the title and the level of
// its message. A transition onenotify does not know is passed on as it is.
func headline(transition string) (word, level string) {
	switch transition {
	case transitionFail:
		return "FAIL", module.LevelError
	case transitionReminder:
		return "STILL FAILING", module.LevelError
	case transitionRecovered:
		return "RECOVERED", module.LevelOK
	}
	return strings.ToUpper(transition), module.LevelInfo
}

// failing reports whether the event tells of a rule that fails: the failure
// itself or a reminder of it. Only such an event explains itself.
func (ev *event) failing() bool {
	return ev.Transition == transitionFail || ev.Transition == transitionReminder
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
	reasons := failureReasons(ev)
	if len(reasons) == 0 {
		return nil
	}
	head := "failed:"
	if res.Consecutive > 1 {
		head = fmt.Sprintf("failed %d runs in a row:", res.Consecutive)
	}
	lines := []string{head}
	for i, reason := range reasons {
		if i == maxFailures {
			lines = append(lines, fmt.Sprintf("… and %d more", len(reasons)-maxFailures))
			break
		}
		lines = append(lines, "- "+cut(reason, maxErrorLine))
	}
	return lines
}

// failedField is one leaf field a failed condition broke on.
type failedField struct {
	Path     string `json:"path"`
	JSONPath string `json:"json_path"`
	Actual   any    `json:"actual"`
	Missing  bool   `json:"missing"`
}

// failureReasons gives one line per reason the conditions failed, in their
// order and each only once: a line per failed field of a failure, or its
// message when onecheck sent no usable fields.
func failureReasons(ev *event) []string {
	var reasons []string
	seen := map[string]bool{}
	add := func(line string) {
		if !seen[line] {
			seen[line] = true
			reasons = append(reasons, line)
		}
	}
	for _, f := range ev.Result.Failures {
		used := false
		for _, field := range f.Fields {
			if line := fieldLine(field); line != "" {
				add(line)
				used = true
			}
		}
		if !used {
			add(oneLine(f.Message))
		}
	}
	return reasons
}

// fieldLine prints a failed field as "<path> = <value>" or "<path>:
// missing", with " json <json_path>" after the path when the field is
// inside parsed JSON. The value is JSON, so a string is quoted. A field
// without a path gives "".
func fieldLine(f failedField) string {
	name := oneLine(f.Path)
	if name == "" {
		return ""
	}
	if jp := oneLine(f.JSONPath); jp != "" {
		name += " json " + jp
	}
	if f.Missing {
		return name + ": missing"
	}
	return name + " = " + jsonValue(f.Actual)
}

// jsonValue prints v as compact JSON on one line.
func jsonValue(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "?"
	}
	return oneLine(buf.String())
}

// paramLines prints rule params as a block: a line "params:" and under it
// one indented line "key: value" per param, sorted by key. The block keeps a
// param such as "state: failed" from reading as a line of the text itself.
// Nested objects become "a.b.c"; anything else that is not a scalar is
// printed as compact JSON. No params, no block.
func paramLines(params map[string]any) []string {
	var lines []string
	flatten("", params, &lines)
	if len(lines) == 0 {
		return nil
	}
	sort.Strings(lines)
	if len(lines) > maxParamLines {
		more := len(lines) - maxParamLines
		lines = append(lines[:maxParamLines:maxParamLines], fmt.Sprintf("… and %d more params", more))
	}
	block := make([]string, 0, len(lines)+1)
	block = append(block, "params:")
	for _, line := range lines {
		block = append(block, paramIndent+line)
	}
	return block
}

// paramIndent sets a param line apart from the lines around the params block.
const paramIndent = "  "

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
	return jsonValue(v)
}
