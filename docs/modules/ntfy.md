---
title: ntfy module
sidebar:
  label: ntfy
---

Publishes a message to a topic of an [ntfy](https://ntfy.sh) server:
`ntfy.sh` or one you host yourself. The ntfy apps turn it into a push
notification on a phone or a desktop.

```yaml
channels:
  - name: phone
    module: ntfy
    params:
      url: https://ntfy.sh
      topic: my-server-alerts
      token: "${NTFY_TOKEN}"
```

## Params

| Key | Required | Default | Description |
|---|---|---|---|
| `topic` | yes | | the topic to publish to |
| `url` | no | `https://ntfy.sh` | `http://` or `https://` address of the ntfy server |
| `token` | no | | access token, sent as a bearer token; cannot be combined with `username` |
| `username` | no | | user name for HTTP basic authentication; requires `password` |
| `password` | no | | password for HTTP basic authentication; requires `username` |
| `priority` | no | by level, see below | ntfy priority `1`..`5` for every message of this channel |

Pass the token or the password through the environment, see
[Configuration](/configuration#environment-variables-in-params). Without
`token` and `username` the message is published anonymously.

Proxy settings are taken from the usual `HTTP_PROXY` / `HTTPS_PROXY` /
`NO_PROXY` environment variables.

## How a notification looks

The title of the message is the ntfy title, the text is the ntfy message.
This is what the module publishes for a failed check:

```json
{
  "topic": "my-server-alerts",
  "title": "[web-01] FAIL main/api-health",
  "message": "module: http\nparams:\n  headers.Authorization: Bearer ${API_TOKEN}\n  url: https://example.com/health\ntags: api, web\nat: 2026-11-24 08:11:02 UTC\nfailed 3 runs in a row:\n- body json status = \"degraded\"\n- status = 503",
  "priority": 4,
  "tags": ["rotating_light", "api", "web"]
}
```

The level sets the priority and the first tag. ntfy shows a tag that is an
emoji short code as an icon in front of the title:

| Level | Priority | First tag | Icon |
|---|---|---|---|
| `error` | 4 (high) | `rotating_light` | 🚨 |
| `warning` | 3 (default) | `warning` | ⚠️ |
| `ok` | 3 (default) | `white_check_mark` | ✅ |
| `info` | 2 (low) | `information_source` | ℹ️ |

- `priority` in the params replaces the priority of every level. Use it for
  a channel that must always, or never, make a sound.
- The tags of the message (the rule's `tags`, `--tag` of `send`) follow the
  level tag. A comma inside a tag becomes a space; empty and repeated tags
  are dropped.
- A message without text gets its title as the ntfy message too. ntfy would
  otherwise put the word `triggered` there.

## Limits

| What | Limit |
|---|---|
| the text | 4096 bytes, the limit of an ntfy server by default |
| the title | 1024 bytes |
| the whole request | 8000 bytes |

A longer text is cut, never inside a character, and ends with `…`. A text
full of quotes or line breaks grows when it is encoded and is cut further
until the request fits. A message is never rejected by the module for its
length.

## Errors

| What happened | Outcome |
|---|---|
| `413 Request Entity Too Large` | **permanent**: the server rejected this very message for its size; it goes to the failed store |
| `429 Too Many Requests` | temporary; with a `Retry-After` header the channel is not used again before that time |
| `400`, `401`, `403`, `404`, `5xx`, any other answer | temporary |
| a redirect | temporary; the module does not follow redirects and says where the server pointed: set that address as `url` |
| no connection, DNS, TLS, timeout | temporary |

Temporary means the message stays in the queue and is tried again by the next
run, see [Queue and delivery](/queue-and-delivery#temporary-and-permanent-errors).
A wrong token or a topic you may not write to is temporary on purpose: you
fix the config and the waiting messages go out.

Error texts quote the server:

```
ntfy: 401 Unauthorized: unauthorized
ntfy: 429 Too Many Requests: limit reached: too many requests
ntfy: 413 Request Entity Too Large: request entity too large
```

The token and the password never appear in an error text.

## Setup hints

**ntfy.sh without an account.** Topics on `ntfy.sh` are not protected:
anyone who knows the name can read the topic and write to it. The topic name
is the password, so choose one that cannot be guessed, and remember what a
notification [can reveal](/messages#what-a-notification-can-reveal):

```yaml
params:
  topic: web-01-alerts-7c1f0a93e5
```

Subscribe to the same topic in the ntfy app. The public server limits how
many messages an address may publish; when the limit is hit, the module gets
`429` and the messages wait.

**An access token.** With an ntfy account, or on your own server, reserve
the topic and publish with a token. Create one in the account settings of the
web app, or on your server:

```sh
ntfy token add alerts-user        # prints tk_...
```

```yaml
params:
  topic: web-01-alerts
  token: "${NTFY_TOKEN}"
```

**A self-hosted server.** Set `url` to its address. A server behind a path
prefix works too:

```yaml
params:
  url: https://ntfy.example.com
  topic: alerts
  username: onenotify
  password: "${NTFY_PASSWORD}"
```

Use either `token` or `username` with `password`, not both.

Try the channel:

```sh
onenotify send --title test --channel phone
```
