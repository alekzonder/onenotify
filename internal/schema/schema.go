// Package schema validates channel params against the JSON Schema a module
// publishes.
package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// Schema is a compiled params schema. A nil *Schema accepts everything.
type Schema struct {
	s *jsonschema.Schema
}

// Compile compiles a module's params_schema. No schema gives a nil *Schema.
func Compile(raw json.RawMessage) (*Schema, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid params_schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("params.json", doc); err != nil {
		return nil, fmt.Errorf("invalid params_schema: %w", err)
	}
	s, err := c.Compile("params.json")
	if err != nil {
		return nil, fmt.Errorf("invalid params_schema: %w", err)
	}
	return &Schema{s}, nil
}

// Validate checks params (JSON) against the schema. The error says which
// param is wrong and how, but never quotes a value: params hold secrets and
// the error is logged and printed.
func (s *Schema) Validate(params json.RawMessage) error {
	if s == nil {
		return nil
	}
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage(`{}`)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(params))
	if err != nil {
		return errors.New("invalid params: not valid JSON")
	}
	err = s.s.Validate(doc)
	if err == nil {
		return nil
	}
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) {
		return errors.New("invalid params: they do not match the module's schema")
	}
	var msgs []string
	collect(verr, &msgs)
	sort.Strings(msgs)
	return fmt.Errorf("invalid params: %s", strings.Join(msgs, "; "))
}

func collect(e *jsonschema.ValidationError, out *[]string) {
	if len(e.Causes) == 0 {
		loc := "/" + strings.Join(e.InstanceLocation, "/")
		*out = append(*out, loc+": "+describe(e.ErrorKind))
		return
	}
	for _, cause := range e.Causes {
		collect(cause, out)
	}
}

// describe explains a violation using names from the schema only.
func describe(k jsonschema.ErrorKind) string {
	switch x := k.(type) {
	case *kind.Required:
		return "missing " + strings.Join(x.Missing, ", ")
	case *kind.AdditionalProperties:
		return "unknown key " + strings.Join(x.Properties, ", ")
	case *kind.Type:
		return fmt.Sprintf("must be %s, got %s", strings.Join(x.Want, " or "), x.Got)
	}
	path := k.KeywordPath()
	if len(path) == 0 {
		return "does not match the schema"
	}
	return "does not satisfy " + path[len(path)-1]
}
