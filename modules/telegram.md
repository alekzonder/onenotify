---
title: telegram module
sidebar:
  label: telegram
---

Sends a message to a Telegram chat, group or channel through the
[Bot API](https://core.telegram.org/bots/api) (`sendMessage`).

```yaml
channels:
  - name: ops-telegram
    module: telegram
    timeout: 10s
    params:
      token: "${TELEGRAM_TOKEN}"
      chat_id: "-1001234567890"
      silent_ok: true
```

## Params

| Key | Required | Default | Description |
|---|---|---|---|
| `token` | yes | | the bot token from @BotFather |
| `chat_id` | yes | | the chat to send to: a number (as a string or a number), or `@channelusername` for a public channel |
| `message_thread_id` | no | | topic id in a forum supergroup; a number |
| `silent_ok` | no | `false` | `true`: messages of level `ok` arrive without a sound |
| `api_url` | no | `https://api.telegram.org` | address of the Bot API server, `http://` or `https://`; for a self-hosted Bot API server |

Pass the token through the environment, see
[Configuration](/configuration#environment-variables-in-params). Write a
numeric `chat_id` in quotes or without, both work; group ids are negative.

Proxy settings are taken from the usual `HTTPS_PROXY` / `NO_PROXY`
environment variables.

## How a notification looks

An icon for the level, the title in bold, an empty line, the text:

```
🔴 [web-01] FAIL main/api-health

module: http
headers.Authorization: Bearer ${API_TOKEN}
url: https://example.com/health
tags: api, web
at: 2026-11-24 08:11:02 UTC
failed 3 runs in a row:
- body json status: expected equals "ok", got "degraded"
- status: expected equals 200, got 503
```

| Level | Icon | Sound |
|---|---|---|
| `error` | 🔴 | yes |
| `ok` | 🟢 | yes; none with `silent_ok: true` |
| `warning` | 🟡 | yes |
| `info` | ℹ️ | yes |

A message without text is the icon and the title alone.

The message is sent with `parse_mode: HTML`, and both the title and the text
are escaped (`<`, `>`, `&`), so whatever a check or a script printed arrives
as it is and cannot break the formatting. Link previews are turned off. The
tags of a message are not shown separately; for a onecheck event they are in
the text.

## Limits

Telegram accepts 4096 characters per message (counted in UTF-16 code units).
A longer message is cut to fit and ends with `…`. The text is cut first; the
title is cut only when it does not fit on its own. A message is never split
into several and never rejected for its length.

## Errors

| What happened | Outcome |
|---|---|
| `429 Too Many Requests` | temporary; the channel is not used again until the time Telegram asked for (`retry_after`) has passed |
| `400` saying `message is too long`, `can't parse entities` or `message text is empty` | **permanent**: Telegram rejected this very message; it goes to the failed store |
| any other `400` (`chat not found`, `message thread not found`, a group that became a supergroup, ...) | temporary |
| `401`, `403`, `404`, `5xx`, any other answer | temporary |
| no connection, DNS, TLS, timeout | temporary |

Temporary means the message stays in the queue and is tried again by the next
run, see [Queue and delivery](/queue-and-delivery#temporary-and-permanent-errors).
A wrong token or a wrong `chat_id` is temporary on purpose: you fix the
config and the waiting messages go out.

Error texts as they appear in the output, in `onenotify status` and in the
log:

```
telegram: 400 Bad Request: chat not found
telegram: 401 Unauthorized
telegram: 429 Too Many Requests: retry after 120
telegram: 400 Bad Request: message is too long
```

The token is part of the request address. The module cuts it out of every
error text; where it would appear, the text says `<token>`.

## Setting up a bot

1. In Telegram, talk to [@BotFather](https://t.me/BotFather): `/newbot`,
   choose a name and a user name. BotFather answers with the token, which
   looks like `123456789:AAF...`. That is `token`.
2. Decide where notifications go and let the bot write there:
   - **to you**: open the bot and press *Start*. A bot cannot write first to
     someone who never talked to it;
   - **to a group**: add the bot to the group;
   - **to a channel**: add the bot as an administrator of the channel.
3. Find the `chat_id`. Send any message to the bot (or in the group), then
   ask the Bot API what it saw:

   ```sh
   curl -s "https://api.telegram.org/bot$TELEGRAM_TOKEN/getUpdates" | jq '.result[].message.chat'
   ```

   `id` in the answer is the `chat_id`: a positive number for a private
   chat, a negative one for a group, `-100...` for a supergroup or a channel.
   A public channel can also be addressed as `@channelusername`.
4. For a topic of a forum supergroup, set `message_thread_id` to the topic's
   id. Copy a link to a message of the topic: in
   `https://t.me/c/1234567890/42/1001` the topic is `42`, the number before
   the message number.
5. Try it:

   ```sh
   onenotify send --title test --channel ops-telegram
   ```

`chat not found` means the `chat_id` is wrong or the bot was never started by
that user or added to that group. `Forbidden: bot was blocked by the user`
and `bot is not a member of the channel chat` say what is missing.
