# onenotify

Notifications for a server. [onecheck](https://github.com/alekzonder/onecheck)
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
  store because its deadline passed or the provider rejected it.
- **Modules are picked up dynamically.** onenotify looks into the modules
  directory on every run. Install a module and the next run can use it.

## Quick start

```sh
# 1. install the latest onenotify and the bundled modules
curl -fsSL https://raw.githubusercontent.com/alekzonder/onenotify/main/install.sh | sh

# 2. write a config; the token comes from the environment
mkdir -p /etc/onenotify && cat > /etc/onenotify/config.yaml <<'YAML'
channels:
  - name: ops-telegram
    module: telegram
    params:
      token: "${TELEGRAM_TOKEN}"
      chat_id: "-1001234567890"
YAML
echo 'TELEGRAM_TOKEN=123456:replace-me' > /etc/onenotify/env
chmod 600 /etc/onenotify/config.yaml /etc/onenotify/env

# 3. check the config, then send a test message
set -a; . /etc/onenotify/env; set +a
onenotify validate
onenotify send --title test

# 4. schedule it, as the same user as onecheck
cat > /etc/cron.d/onenotify <<'CRON'
* * * * *   root  set -a; . /etc/onenotify/env; /usr/local/bin/onenotify >/dev/null
23 3 * * *  root  /usr/local/bin/onenotify logrotate >/dev/null
CRON
```

## Documentation

The documentation is published at <https://alekzonder.github.io/onenotify>.

| | |
|---|---|
| [Getting started](https://alekzonder.github.io/onenotify/getting-started) | install, first config, cron and systemd |
| [Configuration](https://alekzonder.github.io/onenotify/configuration) | every config key, includes, defaults, `${VAR}` |
| [Messages](https://alekzonder.github.io/onenotify/messages) | how a onecheck event becomes a notification, what it can reveal |
| [Sending](https://alekzonder.github.io/onenotify/sending) | `onenotify send`: your own messages from scripts |
| Modules | [telegram](https://alekzonder.github.io/onenotify/modules/telegram), [mail](https://alekzonder.github.io/onenotify/modules/mail), [ntfy](https://alekzonder.github.io/onenotify/modules/ntfy) |
| [Queue and delivery](https://alekzonder.github.io/onenotify/queue-and-delivery) | queues, retries, deadlines, the failed store, `onenotify status` |
| [Output and exit codes](https://alekzonder.github.io/onenotify/output-and-exit-codes) | text and JSON output, exit codes, stderr |
| [Module management](https://alekzonder.github.io/onenotify/module-management) | search, install, update, remove, self-update |
| [Logging and logrotate](https://alekzonder.github.io/onenotify/logging-and-logrotate) | log file, levels, rotation, cleaning the failed store |
| [Module protocol](https://alekzonder.github.io/onenotify/module-protocol) | write your own module, in any language |
| [Development](https://alekzonder.github.io/onenotify/development) | build, test, release |

`onenotify --help` and `onenotify <command> --help` describe every command and flag.

## Build from source

```sh
make build     # dist/onenotify and dist/modules/onenotify-{telegram,mail,ntfy}
make check     # gofmt, go vet, golangci-lint (cyclomatic complexity <= 25), tests
```
