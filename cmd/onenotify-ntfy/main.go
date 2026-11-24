// Command onenotify-ntfy is the onenotify module that publishes messages to
// an ntfy server (https://ntfy.sh or a self-hosted one).
package main

import (
	"encoding/json"

	"github.com/alekzonder/onenotify/internal/version"
	"github.com/alekzonder/onenotify/pkg/module"
)

// token and username/password are two alternative ways to authenticate:
// "not" forbids giving both, "dependentRequired" forbids half of the pair.
const paramsSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["topic"],
  "properties": {
    "url": {"type": "string", "pattern": "^https?://", "description": "http:// or https:// address of the ntfy server, default https://ntfy.sh"},
    "topic": {"type": "string", "minLength": 1, "description": "topic to publish to"},
    "token": {"type": "string", "minLength": 1, "description": "access token, sent as a bearer token; cannot be combined with username and password"},
    "username": {"type": "string", "minLength": 1, "description": "user name for HTTP basic authentication, together with password"},
    "password": {"type": "string", "description": "password for HTTP basic authentication, together with username"},
    "priority": {"type": "integer", "minimum": 1, "maximum": 5, "description": "ntfy priority 1..5 for every message; default depends on the level: error 4, warning 3, ok 3, info 2"}
  },
  "dependentRequired": {
    "username": ["password"],
    "password": ["username"]
  },
  "not": {"required": ["token", "username"]}
}`

var mod = module.Module{
	Description: module.Description{
		Name:         "ntfy",
		Version:      version.Version,
		Description:  "Publish a message to a topic of an ntfy server",
		ParamsSchema: json.RawMessage(paramsSchema),
	},
	Send: send,
}

func main() { module.Main(mod) }
