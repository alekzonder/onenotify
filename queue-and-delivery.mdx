---
title: Queue and delivery
---

onenotify keeps every message on disk from the moment it takes an event until
a provider has accepted the message. This page describes where messages lie,
in which order they go out, what happens when a channel cannot deliver, and
how to see all of it.

## A run

`onenotify` (the same as `onenotify run`) does two things, in this order:

1. **Intake.** Every event waiting in a [source](/configuration#sources) is
   turned into a [message](/messages) and written into the queue of every
   enabled channel. Then the event is removed from the source.
2. **Delivery.** Each channel's queue is delivered through the channel's
   module, oldest message first.

`onenotify send` does the same after queueing its own message, see
[Sending](/sending#what-send-does).

## Directories

```
/var/lib/onenotify/spool/.lock                       run lock
/var/lib/onenotify/spool/.owner                      the config file this spool belongs to
/var/lib/onenotify/spool/<channel>/<id>.json         a message waiting for delivery
/var/lib/onenotify/spool/<channel>/.status.json      state of a channel that is failing
/var/lib/onenotify/failed/<channel>/<id>.json        a message that expired or was rejected
/var/lib/onenotify/failed/_unreadable/<file>         files from a source that are not events
```

The two roots are `paths.spool` and `paths.failed`; `onenotify paths` prints
them. For example, with a mail server that is down:

```console
$ find /var/lib/onenotify | sort
/var/lib/onenotify
/var/lib/onenotify/failed
/var/lib/onenotify/spool
/var/lib/onenotify/spool/.lock
/var/lib/onenotify/spool/.owner
/var/lib/onenotify/spool/admin-mail
/var/lib/onenotify/spool/admin-mail/.status.json
/var/lib/onenotify/spool/admin-mail/20261124T081102Z-main-api-health-fail.json
/var/lib/onenotify/spool/admin-mail/20261124T081102Z-main-db-port-fail.json
/var/lib/onenotify/spool/admin-mail/20261124T081102Z-main-token-check-fail.json
/var/lib/onenotify/spool/ops-telegram
/var/lib/onenotify/spool/phone
```

A queue file is the message itself, see [Messages](/messages#fields); its
name is the message id, shortened and completed with a hash when the id is
too long for a file name. A
message that goes to several channels is one file per channel: channels share
nothing. Channel params and secrets are never in these files.

Directories are created with mode `0750`, files with mode `0640`: messages
can quote what was checked, so they are readable by the owner and the group
only. Every queue file is written to a temporary file (its name starts with a
dot), flushed to disk and then renamed into place, so a visible `.json` file
is always complete.

## Intake

For every source, the event files are taken oldest first. For each one:

1. the event is read and the message is built;
2. the message is written, durably, into the queue of **every enabled
   channel**;
3. the event file is deleted.

What follows from that order:

- **An event is never lost.** If a queue cannot be written (disk full, no
  permission), the event stays in its source, the run stops with exit code
  `2` and delivers nothing; the next run takes the event again. Delivering
  nothing is deliberate: otherwise the channels that did get the message
  would get it again on every run.
- **A crash between writing and deleting causes no duplicates.** The next
  run writes the same files again under the same names and only then starts
  to deliver.
- A channel that is down, misconfigured or missing its module still gets the
  message into its queue. Only `enabled: false` keeps a channel out.
- With no enabled channel nothing is taken: the events stay in the source
  and a warning is logged. onecheck's own `logrotate` removes them when they
  expire.
- A file that cannot be read as an event (not a JSON object, without `id`,
  `rule` or `transition`, or with a value that cannot be stored) is copied to
  `<paths.failed>/_unreadable/` under its name, logged as a warning, and
  removed from the source. The other events are taken as usual.
- `--channel` does not limit the intake: new events are queued for every
  enabled channel even when only some are being delivered.

## Delivery

Channels are served in parallel, `run.parallel` (default `4`) at a time.
Within a channel messages go out **one by one, in the order of their file
names**, which start with a UTC time: `FAIL` reaches a channel before the
`RECOVERED` that followed it.

For each message of a channel:

1. If its deadline has passed, it is moved to the failed store.
2. Otherwise the module is started with the channel's params and the
   message, and has the channel's `timeout` to deliver.
3. The outcome decides what happens next:

| Outcome | Shown as | The message | The channel |
|---|---|---|---|
| the provider accepted it | `SENT` | is deleted from the queue | goes on with the next message |
| temporary error | `WAIT` | stays in the queue | **stops for this run**; everything behind the message waits too |
| permanent error | `FAILED` | is moved to the failed store | goes on with the next message |
| deadline passed | `FAILED` | is moved to the failed store | goes on with the next message |

Stopping a channel at the first temporary error is what keeps the order, and
it keeps a channel that is down from costing one timeout per queued message.
The other channels are not affected in any way.

If a message cannot be moved to the failed store (disk full, no permission),
it stays in its queue, its channel stops for the run so that nothing
overtakes it, and the run ends with exit code `2`.

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
```

`admin-mail` has three messages; one was tried, and all three are counted as
waiting. When the mail server is back, a run empties the queue in order:

```console
$ onenotify run --channel admin-mail
SENT    admin-mail  20261124T081102Z-main-api-health-fail  (mail, 50ms)
SENT    admin-mail  20261124T081102Z-main-db-port-fail  (mail, 50ms)
SENT    admin-mail  20261124T081102Z-main-token-check-fail  (mail, 50ms)
SENT    admin-mail  20261124T081120Z-main-api-health-recovered  (mail, 51ms)
SENT    admin-mail  20261124T081120Z-main-token-check-recovered  (mail, 50ms)
...
```

### Temporary and permanent errors

**Temporary** is everything that time or the administrator can fix: no
connection, a timeout, a rate limit, a server error, and also a wrong token,
an unknown recipient or a refusal by the server's policy. The message waits
and every run tries again, until its deadline.

Problems of the channel itself are temporary too, and are reported the same
way: an environment variable that is not set, a module that is not installed
or not compatible, params the module does not accept, a module that crashed,
printed something that is not an answer, or did not finish within the
channel's `timeout` (it is then killed with everything it started).

```console
$ onenotify send --title "no module" --channel phone
time=2026-11-24T08:12:09.413Z level=WARN msg="channel cannot deliver; messages wait and will be retried" channel=phone error="module is not installed: ntfy (looked for /usr/lib/onenotify/modules/onenotify-ntfy)"
WAIT    phone  20261124T081209Z-send-a98de7e5  (ntfy, 0ms)
        module is not installed: ntfy (looked for /usr/lib/onenotify/modules/onenotify-ntfy)

1 message: 0 sent, 1 waiting, 0 failed
```

**Permanent** is only one thing: the provider rejected the content of this
very message, so sending it again cannot help. Which answers count is listed
on each module's page: [telegram](/modules/telegram#errors),
[mail](/modules/mail#errors), [ntfy](/modules/ntfy#errors). The message is
moved to the failed store and the channel goes on:

```console
$ onenotify send --title "nightly report" --text "all good" --channel ops-telegram
time=2026-11-24T08:26:37.527Z level=WARN msg="message will not be delivered; moved to the failed store" channel=ops-telegram id=20261124T082637Z-send-366b7610 reason="telegram: 400 Bad Request: message is too long"
FAILED  ops-telegram  20261124T082637Z-send-366b7610  (telegram)
        telegram: 400 Bad Request: message is too long

1 message: 0 sent, 0 waiting, 1 failed
```

The line is drawn this way on purpose. A permanent error lets the next
message through, so treating a wrong token as permanent would empty a whole
queue into the failed store because of a typo in the config.

### The deadline

Every message has a deadline, `expires_at`:

- for a onecheck event it is the event's own `expires_at`, set by onecheck's
  `events.ttl` (`24h` by default);
- for `onenotify send`, and for an event without one, it is the time of
  creation plus `queue.ttl` (`24h` by default).

A message that is still in a queue when its deadline passes is not sent any
more: a "failed" from yesterday is not news. The next run moves it to the
failed store with the reason `expired at <time>`:

```console
$ onenotify run --channel phone
time=2026-11-24T08:15:26.573Z level=WARN msg="message will not be delivered; moved to the failed store" channel=phone id=20261122T090000Z-main-old-rule-fail reason="expired at 2026-11-23T09:00:00Z"
FAILED  phone  20261122T090000Z-main-old-rule-fail  (ntfy)
        expired at 2026-11-23T09:00:00Z

1 message: 0 sent, 0 waiting, 1 failed
```

The deadline is checked on every run in every queue directory, including
those of channels that are disabled, were removed from the config, or were
not chosen with `--channel`. There is no limit on the number of attempts:
only the deadline ends them.

### A channel that asks to wait

A provider that answers with a rate limit may say how long to stay away
(`retry_after` of Telegram, `Retry-After` of ntfy). onenotify remembers that
time and does not start the channel's module before it, also in later runs.
The messages are counted as waiting, and nothing is tried:

```console
$ onenotify run --channel phone
1 message: 0 sent, 1 waiting, 0 failed
```

A pause is never longer than 24 hours: a longer one asked by a module is cut
to that, and a remembered time further away than 24 hours is ignored.

### Disabled and removed channels

Messages can be left in the queue of a channel that is no longer served:
`enabled: false` was set while messages were waiting, or the channel was
removed from the config or renamed.

- Nothing is sent to such a channel, and nothing new is queued for it.
- Its messages stay where they are and are moved to the failed store when
  their deadlines pass.
- They are **not** counted as waiting by a run and do not affect its exit
  code: "waiting" and exit code `1` concern only the channels served in that
  run. `onenotify status` shows them.
- Enabling the channel again, or putting it back under the same name,
  delivers what has not expired.
- Once the queue of a removed channel is empty, the next run removes its
  directory, and `status` stops listing it as `not in config`.

### At-least-once

A message is deleted from a queue only after its module reported the
delivery. If onenotify is killed, or the machine goes down, between the
provider accepting the message and the file being deleted, the next run sends
the message again. The same happens when a module is killed at its timeout
after the provider had already taken the message.

So a notification is delivered **at least once** per channel. A duplicate is
rare and has the same id; the mail module sends it with the same
`Message-ID`. What cannot happen is one failing channel causing repeats in
another: each channel has its own copy of the message.

### An interrupted run

On SIGINT or SIGTERM (Ctrl-C, `systemctl stop`, shutdown) onenotify kills the
running modules, leaves every message that was not confirmed in its queue,
and exits with `2`. The interruption is not held against the channel.

## The lock

Only one run works on a spool at a time. A run holds a lock
(`<paths.spool>/.lock`) from the intake to the end of the delivery.

- A second `onenotify run` exits at once with code `2`:

  ```console
  $ onenotify
  onenotify: another onenotify run is still in progress
  ```

  With a run every minute and a channel that hangs until its timeout this is
  normal: the next start does the work.
- `onenotify send` queues its message without the lock, then waits for the
  lock for up to 10 seconds, see
  [Sending](/sending#while-another-run-is-in-progress).
- `onenotify logrotate` skips the log while a run is in progress.
- `status`, `paths` and `validate` never take the lock.

The spool also remembers which config file it belongs to
(`<paths.spool>/.owner`). A different config file pointing at the same spool
is refused:

```console
$ onenotify -c /etc/onenotify/other.yaml
onenotify: spool /var/lib/onenotify/spool is already used by config /etc/onenotify/config.yaml; /etc/onenotify/other.yaml must use different paths.spool and paths.failed
```

If you move or rename the config file, delete `<paths.spool>/.owner` once;
the next run records the new location.

## `onenotify status`

Shows what is waiting and which channels are failing. It only reads, takes no
lock and can be used while a run is in progress.

```console
$ onenotify status
CHANNEL       MODULE    QUEUED  OLDEST                STATE
admin-mail    mail      7       2026-11-24 08:11 UTC  failing since 2026-11-24 08:11 UTC: mail: connect: dial tcp 10.0.0.5:587: connect: connection refused
ops-telegram  telegram  0       -                     ok
phone         ntfy      1       2026-11-24 08:11 UTC  failing since 2026-11-24 08:11 UTC: ntfy: 429 Too Many Requests: limit reached: too many requests (next try after 2026-11-24 08:13 UTC)
team-ntfy     ntfy      0       -                     disabled

failed: 1 message in /var/lib/onenotify/failed
sources:
  onecheck  /var/lib/onecheck/events  0 waiting
```

| Column | Meaning |
|---|---|
| `CHANNEL` | every channel of the config, and every queue directory left in the spool |
| `MODULE` | the channel's module; `-` for a queue that has no channel in the config |
| `QUEUED` | messages waiting in the channel's queue |
| `OLDEST` | when the oldest of them was created |
| `STATE` | see below |

| State | Meaning |
|---|---|
| `ok` | the channel is not known to be failing: its last attempt delivered, or there was nothing to deliver |
| `failing since <time>: <error>` | the channel could not deliver since that time; the error of the last attempt. `(next try after <time>)` when the provider asked to wait |
| `disabled` | `enabled: false`; queued messages wait until they expire |
| `not in config` | a queue is left from a channel that was removed or renamed |

Below the table: the number of files in the failed store (unreadable source
files included), and for each source the number of events waiting to be
taken, or `(missing)` when the directory does not exist yet.

`onenotify status --json` prints the same for scripts:

```json
{
  "channels": [
    {
      "name": "admin-mail",
      "module": "mail",
      "queued": 7,
      "oldest": "2026-11-24T08:11:02.393363196Z",
      "state": "failing",
      "failing_since": "2026-11-24T08:11:08.093904786Z",
      "last_error": "mail: connect: dial tcp 10.0.0.5:587: connect: connection refused"
    },
    {
      "name": "ops-telegram",
      "module": "telegram",
      "queued": 0,
      "state": "ok"
    },
    {
      "name": "phone",
      "module": "ntfy",
      "queued": 1,
      "oldest": "2026-11-24T08:11:31Z",
      "state": "failing",
      "failing_since": "2026-11-24T08:11:31.395932147Z",
      "last_error": "ntfy: 429 Too Many Requests: limit reached: too many requests",
      "retry_at": "2026-11-24T08:13:31.395932147Z"
    },
    {
      "name": "team-ntfy",
      "module": "ntfy",
      "queued": 0,
      "state": "disabled"
    }
  ],
  "failed": 1,
  "failed_dir": "/var/lib/onenotify/failed",
  "sources": [
    {
      "type": "onecheck",
      "path": "/var/lib/onecheck/events",
      "exists": true,
      "waiting": 0
    }
  ]
}
```

| Key | Description |
|---|---|
| `channels[].name`, `module` | the channel and its module; `module` is absent for a queue without a channel |
| `channels[].queued` | messages waiting |
| `channels[].oldest` | creation time of the oldest waiting message; absent when the queue is empty |
| `channels[].state` | `ok`, `failing`, `disabled` or `not_in_config` |
| `channels[].failing_since`, `last_error`, `retry_at` | only for `failing`; `retry_at` only when the provider asked to wait |
| `failed`, `failed_dir` | number of files in the failed store and where it is |
| `sources[]` | `type`, `path`, whether the directory `exists`, and how many events are `waiting` |

```sh
onenotify status --json | jq '.channels[] | select(.queued > 0)'
```

### The state of a failing channel

The `failing since` state is kept in `<paths.spool>/<channel>/.status.json`.
It is removed by the first delivery, and also when the queue becomes empty
because its messages expired or were rejected:

```json
{
  "failing_since": "2026-11-24T08:11:31.395932147Z",
  "consecutive": 1,
  "last_error": "ntfy: 429 Too Many Requests: limit reached: too many requests",
  "retry_at": "2026-11-24T08:13:31.395932147Z"
}
```

The file is written when a delivery fails with a temporary error
(`consecutive` counts the runs in a row that could not deliver) and deleted by
the first successful delivery. It exists so that a broken channel is reported
once and not on every run, see
[Output and exit codes](/output-and-exit-codes#stderr), and so that a rate
limit is respected between runs. Losing it is harmless: the channel is then
taken to be fine. Deleting it by hand makes the next run try the channel at
once, without waiting for `retry_at`.

## The failed store

A message that will not be delivered is not deleted: it is moved to
`<paths.failed>/<channel>/`, with when and why it failed added to it.

```console
$ cat /var/lib/onenotify/failed/ops-telegram/20261124T081131Z-send-0b3895cc.json
{
  "version": 1,
  "id": "20261124T081131Z-send-0b3895cc",
  "source": "send",
  "host": "web-01",
  "level": "info",
  "title": "[web-01] nightly report",
  "text": "all good",
  "tags": [],
  "created_at": "2026-11-24T08:11:31Z",
  "expires_at": "2026-11-25T08:11:31Z",
  "failed_at": "2026-11-24T08:11:31.396860168Z",
  "reason": "telegram: 400 Bad Request: message is too long"
}
```

`reason` is the provider's error, `expired at <time>`, or `unreadable queue
file: ...` for a file in a queue that is not a message (such a file is moved
as it is). Every move is logged as a warning, which also reaches stderr.

Files stay in the failed store for `queue.failed_ttl` (`7d` by default);
`onenotify logrotate` removes older ones, see
[Logging and logrotate](/logging-and-logrotate#the-failed-store).

### Sending a failed message again

There is no command for it. Move the file back into the channel's queue; the
next run delivers it. The two extra keys do not matter.

```console
$ mv /var/lib/onenotify/failed/phone/20261124T081131Z-send-d3cfd45b.json /var/lib/onenotify/spool/phone/
$ onenotify run --channel phone
SENT    phone  20261124T081131Z-send-d3cfd45b  (ntfy, 6ms)

1 message: 1 sent, 0 waiting, 0 failed
```

This works **before the message's deadline**. A message whose `expires_at`
has passed goes straight back to the failed store. To send it anyway, set
`expires_at` in the file to a time in the future before moving it.

An unreadable file from `_unreadable/` can be repaired and put back into the
source directory (the onecheck event queue), where the next run takes it.

Do the move as the user that runs onenotify, so that the file keeps its owner
and mode.
