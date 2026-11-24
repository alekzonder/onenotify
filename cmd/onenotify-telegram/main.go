// Command onenotify-telegram is the onenotify module that sends messages to
// a Telegram chat through the Bot API.
package main

import (
	"encoding/json"

	"github.com/alekzonder/onenotify/internal/version"
	"github.com/alekzonder/onenotify/pkg/module"
)

const paramsSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["token", "chat_id"],
  "properties": {
    "token": {"type": "string", "minLength": 1, "description": "bot token from @BotFather"},
    "chat_id": {"type": ["string", "integer"], "description": "chat id (a number) or @channelusername"},
    "message_thread_id": {"type": "integer", "description": "topic id in a forum supergroup"},
    "silent_ok": {"type": "boolean", "description": "send messages of level ok without sound, default false"},
    "api_url": {"type": "string", "pattern": "^https?://", "description": "Bot API server, default https://api.telegram.org"}
  }
}`

var mod = module.Module{
	Description: module.Description{
		Name:         "telegram",
		Version:      version.Version,
		Description:  "Send a message to a Telegram chat through the Bot API",
		ParamsSchema: json.RawMessage(paramsSchema),
	},
	Send: send,
}

func main() { module.Main(mod) }
