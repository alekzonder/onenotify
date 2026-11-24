# Output and exit codes

A run reports its outcome in four places:

| Where | What | For |
|---|---|---|
| exit code | is anything left undelivered | cron, systemd, scripts |
| stdout | every message that was sent, has to wait or was given up; text or JSON | people, pipelines |
| stderr | warnings: a channel that started failing, a message that was given up | the mail cron sends |
| the log file | all of it, see [Logging and logrotate](/logging-and-logrotate) | later |

`onenotify`, `onenotify run` and `onenotify send` print the same report.
What is waiting between runs is shown by
[`onenotify status`](/queue-and-delivery#onenotify-status).

## Exit codes

| Code | Meaning |
|---|---|
| `0` | everything in the queues that were served was delivered, or there was nothing to do |
| `1` | at least one message is still waiting, or was moved to the failed store in this run |
| `2` | onenotify could not do its job: no or invalid config, unknown flag or channel, another run in progress, its own files could not be written, the run was interrupted (SIGINT, SIGTERM) |

`1` is not a failure of onenotify: the messages are safe in their queues and
the next run tries again. It is "look at `onenotify status` if this lasts".
In a systemd unit, declare it with `SuccessExitStatus=1`.

Only the channels served in the run count. Messages left in the queue of a
disabled or removed channel, or of a channel not chosen with `--channel`, do
not make the exit code `1`.

With code `2` the reason is on stderr, prefixed with `onenotify:`. The report
is still printed when something had been done before the failure.

For `onenotify send`, see also [Sending](/sending#exit-codes): there `2`
normally means that nothing was queued, unless the error says `the message
is queued, but ...`.

## Flags

```
onenotify run [-c <config>] [-o text|json] [--channel <name>]... [--log-level <level>]
```

| Flag | Effect |
|---|---|
| `-o, --output text\|json` | output format, default `text` |
| `--channel <name>` | deliver only to this channel; repeatable. The intake is not limited: new events are queued for every enabled channel. A name that is not in the config is an error |
| `--log-level <level>` | override `log.level` for this run and also print the log at that level on stderr |

The report is printed on stdout only. Log lines never go to stdout, so
`onenotify -o json | jq` is safe.

## Text output

```
WAIT    admin-mail    20261124T081102Z-main-api-health-fail  (mail, 6ms)
        mail: connect: dial tcp 10.0.0.5:587: connect: connection refused
SENT    ops-telegram  20261124T081102Z-main-api-health-fail  (telegram, 9ms)
SENT    ops-telegram  20261124T081102Z-main-db-port-fail  (telegram, 6ms)
SENT    ops-telegram  20261124T081102Z-main-token-check-fail  (telegram, 12ms)
SENT    phone         20261124T081102Z-main-api-health-fail  (ntfy, 8ms)
SENT    phone         20261124T081102Z-main-db-port-fail  (ntfy, 7ms)
SENT    phone         20261124T081102Z-main-token-check-fail  (ntfy, 8ms)

9 messages: 6 sent, 3 waiting, 0 failed
```

One line per message that something happened to: outcome, channel, message
id, module and how long the module took. Lines are ordered by channel name,
then by message. Under a message that was not sent, the reason.

| Outcome | Meaning |
|---|---|
| `SENT` | the provider accepted the message; it left the queue |
| `WAIT` | it could not be delivered now; it stays in the queue, and so does everything behind it in that channel, until the next run |
| `FAILED` | it will not be delivered: its deadline passed, or the provider rejected its content; it was moved to the failed store |

A `FAILED` message that was never handed to a module, such as an expired
one, has no duration:

```
FAILED  phone  20261122T090000Z-main-old-rule-fail  (ntfy)
        expired at 2026-11-23T09:00:00Z
```

The last line counts messages, not lines. `waiting` includes the messages
behind a `WAIT` in the same channel, which have no line of their own: above,
`admin-mail` has one line and three waiting messages. A channel that is
paused by a rate limit has no line at all, only the count.

When there was nothing to do, only the summary is printed:

```
0 messages
```

Control characters in an id or in an error text are printed as spaces, so
what a provider answered cannot break the lines.

## JSON output

`-o json` prints one object:

```json
{
  "ingested": 2,
  "records": [
    {
      "channel": "admin-mail",
      "module": "mail",
      "id": "20261124T081102Z-main-api-health-fail",
      "outcome": "waiting",
      "duration_ms": 5,
      "error": "mail: connect: dial tcp 10.0.0.5:587: connect: connection refused"
    },
    {
      "channel": "ops-telegram",
      "module": "telegram",
      "id": "20261124T081120Z-main-api-health-recovered",
      "outcome": "sent",
      "duration_ms": 11
    },
    {
      "channel": "ops-telegram",
      "module": "telegram",
      "id": "20261124T081120Z-main-token-check-recovered",
      "outcome": "sent",
      "duration_ms": 8
    },
    {
      "channel": "phone",
      "module": "ntfy",
      "id": "20261124T081120Z-main-api-health-recovered",
      "outcome": "sent",
      "duration_ms": 9
    },
    {
      "channel": "phone",
      "module": "ntfy",
      "id": "20261124T081120Z-main-token-check-recovered",
      "outcome": "sent",
      "duration_ms": 7
    }
  ],
  "sent": 4,
  "waiting": 5,
  "failed": 0
}
```

| Key | Description |
|---|---|
| `ingested` | events taken from the sources in this run |
| `records` | one entry per line of the text output, in the same order; an empty array when nothing happened |
| `records[].channel` | channel name |
| `records[].module` | the channel's module; empty for a queue whose channel is no longer in the config |
| `records[].id` | message id |
| `records[].outcome` | `sent`, `waiting` or `failed` |
| `records[].duration_ms` | how long the module took; `0` when no module was started |
| `records[].error` | why the message was not sent; absent for `sent` |
| `sent` | messages delivered in this run |
| `waiting` | messages left in the queues of the channels that were served, including those without a record |
| `failed` | messages moved to the failed store in this run |

Here two new events were taken, both went out through two channels, and
`admin-mail` now holds five messages behind the one that was tried.

Two things `send` prints are plain text even with `-o json`: the line
`queued <id> for <channels>` of `--queue-only`, and the same line when
another run was in progress.

## stderr

stderr carries the log at level `warn` and above, in the log's text format.
It is what cron mails when stdout goes to `/dev/null`:

```cron
* * * * *  root  set -a; . /etc/onenotify/env; /usr/local/bin/onenotify >/dev/null
```

What is a warning is chosen so that this mail is worth reading:

- **A channel that starts failing is a warning once.** The first run that
  cannot deliver to a channel prints

  ```
  time=2026-11-24T08:11:08.093Z level=WARN msg="channel cannot deliver; messages wait and will be retried" channel=admin-mail error="mail: connect: dial tcp 10.0.0.5:587: connect: connection refused"
  ```

  The following runs log `channel still cannot deliver` at level `info`,
  which stays in the log file. When the channel works again, that is logged
  at `info` too. A mail server that is down for a night is one mail.
- **A message that is given up is a warning**, each one: `message will not
  be delivered; moved to the failed store`, with the channel, the id and the
  reason.
- **A file in a source that is not an event** is a warning.
- **Events waiting while no channel is enabled** is not a warning of a run
  (it would be repeated every minute): the log has it at `info`, and
  `onenotify validate` warns about it.

A run that could not complete is an error: `onenotify: <reason>` and exit
code `2`.

With `--log-level` on the command line stderr shows everything at that level,
see [Logging and logrotate](/logging-and-logrotate).
