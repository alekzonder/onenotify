// Command onenotify-mail is the onenotify module that sends a message as an
// e-mail through an SMTP server.
package main

import (
	"encoding/json"

	"github.com/alekzonder/onenotify/internal/version"
	"github.com/alekzonder/onenotify/pkg/module"
)

const paramsSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["host", "from", "to"],
  "properties": {
    "host": {"type": "string", "minLength": 1, "description": "SMTP server host name or address"},
    "port": {"type": "integer", "minimum": 1, "maximum": 65535, "description": "SMTP server port; default by tls: starttls 587, tls 465, none 25"},
    "tls": {"enum": ["starttls", "tls", "none"], "description": "starttls (default): upgrade a plain connection with STARTTLS; tls: TLS from the first byte; none: plain text"},
    "username": {"type": "string", "minLength": 1, "description": "user name; authentication happens only when it is set"},
    "password": {"type": "string", "description": "password, requires username"},
    "from": {"type": "string", "minLength": 1, "description": "sender address, plain or as \"Name <address>\""},
    "to": {"type": "array", "minItems": 1, "items": {"type": "string", "minLength": 1}, "description": "recipient addresses"},
    "helo": {"type": "string", "minLength": 1, "description": "name sent in EHLO, default the system host name"},
    "insecure_skip_verify": {"type": "boolean", "description": "do not verify the server certificate, default false"}
  },
  "dependentRequired": {"password": ["username"]}
}`

var mod = module.Module{
	Description: module.Description{
		Name:         "mail",
		Version:      version.Version,
		Description:  "Send a message as a plain text e-mail through an SMTP server",
		ParamsSchema: json.RawMessage(paramsSchema),
	},
	Send: send,
}

func main() { module.Main(mod) }
