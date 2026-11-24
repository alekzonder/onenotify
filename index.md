---
title: onenotify
description: Notifications for onecheck events and your own scripts.
sidebar:
  label: Introduction
---

Notifications for a server. [onecheck](https://alekzonder.github.io/onecheck)
writes an event every time a check fails or recovers; onenotify takes those
events, turns each into a message with the host name in it, and delivers it to
every channel you configured: Telegram, mail, ntfy. `onenotify send` sends a
message of your own the same way. Cron (or a systemd timer) runs `onenotify`;
nothing keeps running in between.

```console
$ onenotify
time=2026-11-24T08:11:08.093Z level=WARN msg="channel cannot deliver; messages wait and will be retried" channel=admin-mail error="mail: connect: dial tcp 10.0.0.5:587: connect: connection refused"
WAIT    admin-mail    20261124T081102Z-main-api-health-fail  (mail, 6ms)
        mail: connect: dial tcp 10.0.0.5:587: connect: connection refused
SENT    ops-telegram  20261124T081102Z-main-api-health-fail  (telegram, 9ms)
SENT    ops-telegram  20261124T081102Z-main-db-port-fail  (telegram, 6ms)
SENT    ops-telegram  20261124T081102Z-main-token-check-fail  (telegram, 12ms)
SENT    phone         20261124T081102Z-main-api-health-fail  (ntfy, 8ms)
SENT    phone         20261124T081102Z-main-db-port-fail  (ntfy, 7ms)
SENT    phone         20261124T081102Z-main-token-check-fail  (ntfy, 8ms)

9 messages: 6 sent, 3 waiting, 0 failed
$ echo $?
1
```

What arrived in the Telegram chat:

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

## How it works

- **onenotify decides, modules deliver.** A channel names a module
  (`telegram`, `mail`, `ntfy`, ...), a separate executable `onenotify-<name>`
  living in a modules directory. What is sent, to whom and with which text is
  decided by onenotify, so a notification reads the same in every channel. A
  module only knows how to hand a title and a text to one kind of
  destination.
- **A queue per channel.** A run first moves every waiting event into the
  queue of every enabled channel, then delivers each queue in order, oldest
  first. A mail server that is down delays nothing in Telegram, causes no
  repeats there, and gets its messages, in order, once it works again.
- **Nothing is lost.** An event is removed from onecheck's queue only after
  its message is safely on disk in every channel's queue. A message leaves a
  queue only when the provider accepted it, or when it is moved to the failed
  store because its deadline passed or the provider rejected it. Every such
  case is logged and shown by `onenotify status`.
- **Modules are picked up dynamically.** onenotify looks into the modules
  directory on every run. Install a module and the next run can use it.

## Quick start

```sh
# 1. install the latest onenotify and the bundled modules
curl -fsSL https://raw.githubusercontent.com/alekzonder/onenotify/main/install.sh | sh

# 2. write a config
mkdir -p /etc/onenotify && cat > /etc/onenotify/config.yaml <<'YAML'
channels:
  - name: phone
    module: ntfy
    params:
      topic: my-server-alerts-4f2a9c
YAML
chmod 600 /etc/onenotify/config.yaml

# 3. check the config, then send a test message
onenotify validate
onenotify send --title test
```

[Getting started](/getting-started) walks through installation, the first
config and scheduling with cron or systemd.

## Documentation

| | |
|---|---|
| [Getting started](/getting-started) | install, first config, cron and systemd |
| [Configuration](/configuration) | every config key, includes, defaults, `${VAR}` |
| [Messages](/messages) | how a onecheck event becomes a notification, what it can reveal |
| [Sending](/sending) | `onenotify send`: your own messages from scripts |
| Modules | [telegram](/modules/telegram), [mail](/modules/mail), [ntfy](/modules/ntfy) |
| [Queue and delivery](/queue-and-delivery) | queues, retries, deadlines, the failed store, `onenotify status` |
| [Output and exit codes](/output-and-exit-codes) | text and JSON output, exit codes, stderr |
| [Module management](/module-management) | search, install, update, remove, self-update |
| [Logging and logrotate](/logging-and-logrotate) | log file, levels, rotation, cleaning the failed store |
| [Module protocol](/module-protocol) | write your own module, in any language |
| [Development](/development) | build, test, release |

`onenotify --help` and `onenotify <command> --help` describe every command
and flag. The source is on [GitHub](https://github.com/alekzonder/onenotify).

Deliberately not there: a daemon or an HTTP API, message templates, routing
of events to some channels only, reminders about a check that is still
failing. Every event goes to every enabled channel, once.
