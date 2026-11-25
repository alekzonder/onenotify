---
title: Module protocol
---

This page is for people writing a module. A module teaches onenotify a new
place to deliver to: a chat, a paging service, an SMS gateway, a webhook,
anything.

## The idea

A module is a standalone executable named `onenotify-<name>`. onenotify
starts it once per message, hands it the channel's `params` and the message
on stdin as JSON, and reads the outcome from its stdout as JSON.

A module only delivers. It never decides what is sent, to whom, or with which
words, it does not read queues and it does not retry: onenotify does all of
that, which is why a notification reads the same in every channel and why
retries, ordering and deadlines work for every module without the module
doing anything. What a module does decide is how the title and the text look
in its channel.

A module can be written in any language. For Go there is a small SDK,
`github.com/alekzonder/onenotify/pkg/module`.

Protocol version described here: **1**.

## Invocations

A module must implement two subcommands.

### `onenotify-<name> describe`

Prints a JSON description on stdout and exits with `0`. It must be fast and
free of side effects: onenotify calls it on every run and on install.

```json
{
  "protocol": 1,
  "name": "webhook",
  "version": "0.1.0",
  "description": "Post a message as JSON to an HTTP endpoint",
  "params_schema": {
    "type": "object",
    "additionalProperties": false,
    "required": ["url"],
    "properties": {
      "url": { "type": "string", "pattern": "^https?://" },
      "token": { "type": "string" }
    }
  }
}
```

| Key | Required | Description |
|---|---|---|
| `protocol` | yes | protocol version the module speaks |
| `name` | yes | module name; must equal `<name>` in the file name |
| `version` | yes | module version (semver) |
| `description` | no | one line, shown by `onenotify module list` |
| `params_schema` | no | JSON Schema (draft 2020-12) for the channel's `params`; onenotify validates `params` with it in `onenotify validate` and before every delivery |

Use `"additionalProperties": false` in `params_schema` so that typos in
`params` are caught, and `required` for what the module cannot work without.
A channel whose params do not fit is never started: its messages wait and the
user is told which param is wrong.

### `onenotify-<name> send`

Reads one request from stdin, delivers its message, prints one response on
stdout.

Request:

```json
{
  "protocol": 1,
  "channel": "hook",
  "params": { "url": "https://hooks.example.com/alerts", "token": "s3cr3t" },
  "message": {
    "id": "20261124T082319Z-send-10a582c5",
    "source": "send",
    "host": "web-01",
    "level": "info",
    "title": "[web-01] backup finished",
    "text": "42 GB in 13m",
    "tags": ["backup"],
    "created_at": "2026-11-24T08:23:19Z"
  },
  "timeout_ms": 10000
}
```

| Key | Description |
|---|---|
| `protocol` | protocol version onenotify is using for this call |
| `channel` | name of the channel in the config, for the module's own diagnostics |
| `params` | the channel's `params`, with `${VAR}` already replaced and already validated against `params_schema`. They are secrets |
| `message.id` | unique id of the message; the same on every attempt, so it can serve to deduplicate |
| `message.source` | `onecheck` or `send` |
| `message.host` | the host name |
| `message.level` | `error`, `ok`, `warning` or `info`: use it to choose an icon, a priority, a sound |
| `message.title` | one line; it already starts with `[<host>] ` |
| `message.text` | plain text of any number of lines; may be empty |
| `message.tags` | list of strings; may be empty |
| `message.created_at` | when it happened (UTC, RFC 3339) |
| `timeout_ms` | the channel's timeout; finish before it |

Response when the provider accepted the message:

```json
{ "delivered": true, "error": null }
```

Response when it did not:

```json
{
  "delivered": false,
  "error": { "message": "webhook: 429 Too Many Requests", "retry_after_ms": 120000 }
}
```

| Key | Description |
|---|---|
| `delivered` | `true` when the provider accepted the message |
| `error.message` | what went wrong, one line, **without secrets**. It is shown in the output, in `onenotify status` and in the log. Required when `delivered` is `false` |
| `error.permanent` | `true`: this very message can never be delivered through this channel. Default `false` |
| `error.retry_after_ms` | optional: do not use the channel for this long. onenotify cuts it to 24 hours |

What onenotify does with the answer:

| Answer | The message | The channel |
|---|---|---|
| `delivered: true` | leaves the queue | goes on with the next message |
| an error, not permanent | stays in the queue and is tried again by the next run | stops for this run; with `retry_after_ms`, until that time has passed |
| an error with `permanent: true` | is moved to the failed store | goes on with the next message |

## Rules of behaviour

**Permanent only when the provider rejected this very message.** "The text
is too long", "the content cannot be parsed": sending the same message again
cannot help. Everything else is temporary, including a wrong token, an
unknown recipient, a refusal by the server's policy, a server that is down
and a rate limit: the administrator fixes the config, or time passes, and
the message goes out.

**When in doubt, temporary.** A permanent error lets the next message
through, so a wrong classification empties a whole queue into the failed
store because of a typo in the config. A temporary error only makes messages
wait, and a message that can never be delivered still leaves the queue when
its deadline passes.

**Never put secrets into error text.** `params` are tokens and passwords,
and so may be the address. An error message ends up in the terminal, in the
log and in cron mail. Name the param that is wrong, never its value, and
watch out for library errors that quote a URL with a token in it.

**Fit the message into the channel yourself.** Cut a text that is too long
for the provider (and end it with `…`), escape what the provider would read
as markup, drop what it cannot carry. A message must never be rejected for
its length or its characters. The title and the text can hold anything a
check or a script printed.

**Exit code.** Exit with `0` whenever you produced a response, including one
with an error. A non-zero exit code, invalid JSON on stdout, no output, or
`delivered: false` without a message means the module itself is broken;
onenotify treats that as a temporary error:

```
module "webhook" failed with exit code 3
module "webhook" returned invalid JSON: invalid character 'g' looking for beginning of value
module "webhook" did not deliver and gave no reason
module "webhook" timed out after 10s
```

**stdout is for the response only.** Write diagnostics to stderr. onenotify
puts a module's stderr into its log at level `debug`.

**Timeouts.** onenotify starts a module in its own process group and kills
the whole group a short moment after `timeout_ms`, and also when onenotify
itself is interrupted. Prefer to stop on your own before that and return an
error saying what timed out. Remember that a module killed after the provider
took the message causes a second delivery on the next run.

**No state, no side effects but the delivery.** A module is started fresh
for every message and may run in parallel with other modules, including
other instances of itself for other channels. Messages of one channel are
handed over one at a time, in order.

**Environment.** A module inherits onenotify's environment and runs with its
privileges.

## Writing a module in Go

The SDK implements the protocol; you provide a description and one function.
This is a complete module that posts a message as JSON to an HTTP endpoint:

```go
// Command onenotify-webhook posts a message as JSON to an HTTP endpoint.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alekzonder/onenotify/pkg/module"
)

type params struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// maxText is what the imaginary endpoint accepts.
const maxText = 2000

func send(ctx context.Context, req module.Request) error {
	var p params
	if err := module.DecodeParams(req.Params, &p); err != nil {
		return err
	}
	m := req.Message
	body, err := json.Marshal(map[string]any{
		"title": m.Title,
		"text":  module.Truncate(m.Text, maxText), // fit the message yourself
		"level": m.Level,
		"host":  m.Host,
		"tags":  m.Tags,
	})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader(body))
	if err != nil {
		return errors.New("webhook: param url is not a valid address") // never quote a param value
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.Token)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		// The text of a net/http error holds the URL; keep only the cause.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("webhook: %w", err) // temporary: the message waits
	}
	defer resp.Body.Close()

	failure := fmt.Errorf("webhook: %s", resp.Status)
	switch {
	case resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		// The endpoint refused this very message: retrying cannot help.
		return module.Permanent(failure)
	case resp.StatusCode == http.StatusTooManyRequests:
		seconds, _ := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
		return module.RetryAfter(failure, time.Duration(seconds)*time.Second)
	}
	// 401, 403, 404, 5xx: somebody fixes the config or the server.
	return failure
}

func main() {
	module.Main(module.Module{
		Description: module.Description{
			Name:        "webhook",
			Version:     "0.1.0",
			Description: "Post a message as JSON to an HTTP endpoint",
			ParamsSchema: json.RawMessage(`{
				"type": "object",
				"additionalProperties": false,
				"required": ["url"],
				"properties": {
					"url": {"type": "string", "pattern": "^https?://"},
					"token": {"type": "string"}
				}
			}`),
		},
		Send: send,
	})
}
```

```sh
go mod init example.com/onenotify-webhook
go get github.com/alekzonder/onenotify
go build -o /usr/lib/onenotify/modules/onenotify-webhook .
```

```yaml
channels:
  - name: hook
    module: webhook
    params:
      url: https://hooks.example.com/alerts
      token: "${HOOK_TOKEN}"
```

What the SDK does for you:

- `module.Main` handles `describe` and `send`, fills in `protocol`, applies
  `timeout_ms` to the context passed to `Send`, and turns the returned error
  into the response. A `nil` error means delivered.
- `module.Permanent(err)` marks an error as a rejection of this very
  message; `module.RetryAfter(err, d)` asks onenotify to leave the channel
  alone for `d`. An error without either is temporary.
- `module.DecodeParams` decodes `params` strictly (unknown keys are errors).
- `module.Truncate(s, max)` cuts a string to at most `max` bytes without
  splitting a character, ends a cut string with `…` and drops invalid UTF-8.
- `module.LevelError`, `LevelOK`, `LevelWarning`, `LevelInfo` are the
  levels.

## Writing a module in another language

Anything that can read stdin and print JSON works. A complete module in
shell, which appends every message as one line to a file (it needs `jq`):

```sh
#!/bin/sh
# onenotify-logfile: appends every message as one line to a file. Needs jq.
case "$1" in
  describe)
    cat <<'JSON'
{"protocol": 1, "name": "logfile", "version": "0.1.0",
 "description": "Append a message as one line to a file",
 "params_schema": {"type": "object", "additionalProperties": false,
   "required": ["path"],
   "properties": {"path": {"type": "string", "minLength": 1}}}}
JSON
    ;;
  send)
    request=$(cat)
    path=$(printf '%s' "$request" | jq -r '.params.path')
    line=$(printf '%s' "$request" | jq -r '.message | "\(.created_at) \(.level) \(.title) | \(.text | gsub("\n"; " / "))"')
    if printf '%s\n' "$line" >>"$path" 2>/dev/null; then
      printf '{"delivered": true, "error": null}\n'
    else
      # cannot write now: temporary, the message waits and is tried again
      jq -cn --arg m "logfile: cannot write to the file given in param path" \
        '{delivered: false, error: {message: $m}}'
    fi
    ;;
  *)
    echo "usage: $0 describe|send" >&2
    exit 2
    ;;
esac
```

```yaml
channels:
  - name: journal
    module: logfile
    params:
      path: /var/log/notifications.log
```

```console
$ onenotify send --title "module test" --text "line one
line two" --channel journal
SENT    journal  20261124T082410Z-send-078b39d1  (logfile, 19ms)

1 message: 1 sent, 0 waiting, 0 failed
$ cat /var/log/notifications.log
2026-11-24T08:24:10Z info [web-01] module test | line one / line two
```

## Trying a module by hand

```console
$ onenotify-webhook describe
{"protocol":1,"name":"webhook","version":"0.1.0","description":"Post a message as JSON to an HTTP endpoint","params_schema":{"type":"object","additionalProperties":false,"required":["url"],"properties":{"url":{"type":"string","pattern":"^https?://"},"token":{"type":"string"}}}}
```

```console
$ echo '{"protocol":1,"channel":"try","params":{"url":"http://127.0.0.1:18476/hook"},"message":{"id":"20261124T100200Z-send-00000000","source":"send","host":"web-01","level":"info","title":"[web-01] hello","text":"first try","tags":[],"created_at":"2026-11-24T10:02:00Z"},"timeout_ms":5000}' | onenotify-webhook send
{"delivered":false,"error":{"message":"webhook: dial tcp 127.0.0.1:18476: connect: connection refused"}}
```

Then through onenotify:

```sh
onenotify module list          # is it found, is it compatible
onenotify validate             # do my channels fit its params
onenotify send --title test --channel hook --log-level debug
```

```console
$ onenotify module list
modules in /usr/lib/onenotify/modules:
NAME      VERSION  PROTOCOL  COMPATIBLE  SOURCE  DESCRIPTION
logfile   0.1.0    1         yes         -       Append a message as one line to a file
mail      0.1.0    1         yes         -       Send a message as a plain text e-mail through an SMTP server
ntfy      0.1.0    1         yes         -       Publish a message to a topic of an ntfy server
telegram  0.1.0    1         yes         -       Send a message to a Telegram chat through the Bot API
webhook   0.1.0    1         yes         -       Post a message as JSON to an HTTP endpoint
```

Test the unhappy paths too: an endpoint that is down (the message must
wait), a rate limit, a text of 64 KB, a title with `<`, `&` and emoji.

## Distributing a module

Publish a GitHub release with assets named `onenotify-<name>_<os>_<arch>`
and a `checksums.txt`; then anyone can `onenotify module install
<owner/repo>`. Details:
[Module management](/module-management#repositories-of-third-party-modules).

## Protocol versions

The protocol version is a single integer. It changes only when the JSON
exchanged above changes incompatibly. onenotify supports a range of versions
(`onenotify version` prints it) and passes the module's own version back in
each request. Adding optional keys to requests or responses does not change
the version: modules must ignore request keys they do not know, and onenotify
ignores response keys it does not know.
