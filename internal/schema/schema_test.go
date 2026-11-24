package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

const example = `{
  "type": "object", "additionalProperties": false, "required": ["token", "to"],
  "properties": {
    "token": {"type": "string", "pattern": "^[0-9]+:"},
    "to": {"type": "array", "minItems": 1, "items": {"type": "string"}},
    "tls": {"enum": ["starttls", "tls", "none"]},
    "port": {"type": "integer"}
  }
}`

func TestValidate(t *testing.T) {
	s, err := Compile(json.RawMessage(example))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(json.RawMessage(`{"token":"1:a","to":["x"],"tls":"tls","port":25}`)); err != nil {
		t.Errorf("valid params: %v", err)
	}
	cases := map[string]string{
		`{"to":["x"]}`:                                    "/: missing token",
		`{"token":"1:a","to":["x"],"nope":1}`:             "/: unknown key nope",
		`{"token":"SECRETVALUE","to":["x"]}`:              "/token: does not satisfy pattern",
		`{"token":"1:a","to":[]}`:                         "/to: does not satisfy minItems",
		`{"token":"1:a","to":["x"],"tls":"SECRETVALUE"}`:  "/tls: does not satisfy enum",
		`{"token":"1:a","to":["x"],"port":"SECRETVALUE"}`: "/port: must be integer, got string",
		`{"token":5,"to":"SECRETVALUE"}`:                  "/to: must be array, got string; /token: must be string, got number",
	}
	for params, want := range cases {
		err := s.Validate(json.RawMessage(params))
		if err == nil || err.Error() != "invalid params: "+want {
			t.Errorf("%s: %v, want %s", params, err, want)
		}
		if err != nil && strings.Contains(err.Error(), "SECRETVALUE") {
			t.Errorf("%s: the error quotes a value: %v", params, err)
		}
	}
	if err := s.Validate(nil); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("no params: %v", err)
	}
	if err := s.Validate(json.RawMessage(`{oops`)); err == nil {
		t.Error("broken JSON must be an error")
	}
}

func TestNoSchemaAcceptsEverything(t *testing.T) {
	for _, raw := range []string{"", "null", "  "} {
		s, err := Compile(json.RawMessage(raw))
		if err != nil || s != nil {
			t.Errorf("%q: s=%v err=%v", raw, s, err)
		}
		if err := s.Validate(json.RawMessage(`{"anything":1}`)); err != nil {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

func TestCompileErrors(t *testing.T) {
	for _, raw := range []string{`{oops`, `{"type": 5}`} {
		if _, err := Compile(json.RawMessage(raw)); err == nil {
			t.Errorf("%s: no error", raw)
		}
	}
}
