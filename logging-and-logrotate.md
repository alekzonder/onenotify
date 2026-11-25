---
title: Logging and logrotate
---

## Log file

Every run appends to

```
<log.dir>/onenotify.log
```

by default `/var/log/onenotify/onenotify.log` for root and
`$HOME/.local/state/onenotify/log/onenotify.log` for other users. The
directory is created when needed. `onenotify paths` prints the exact path for
the current config.

```yaml
log:
  level: info      # debug | info | warn | error
  dir: /var/log/onenotify
  format: text     # text | json
```

| Level | What is logged |
|---|---|
| `error` | a run that could not complete |
| `warn` | the first failure of a channel; a message moved to the failed store; a file in a source that is not an event |
| `info` | one line per event taken, per message queued by `send` and per message sent; repeated failures of a channel; a channel that delivers again; a channel that is paused by a rate limit; a summary per run |
| `debug` | additionally: the start of a run with its paths; the modules that were loaded; each request to a module, with the **names** of the channel's params; what modules printed on their stderr |

The level is taken from `--log-level`, then `log.level`, then `info`:

```sh
onenotify --log-level debug --channel phone
```

`text` format:

```
time=2026-11-24T08:11:08.040Z level=INFO msg="event taken" id=20261124T081102Z-main-api-health-fail level=error title="[web-01] FAIL main/api-health" channels=3
time=2026-11-24T08:11:08.093Z level=WARN msg="channel cannot deliver; messages wait and will be retried" channel=admin-mail error="mail: connect: dial tcp 10.0.0.5:587: connect: connection refused"
time=2026-11-24T08:11:08.097Z level=INFO msg="message sent" channel=phone module=ntfy id=20261124T081102Z-main-api-health-fail duration_ms=8
time=2026-11-24T08:11:08.097Z level=INFO msg="message sent" channel=ops-telegram module=telegram id=20261124T081102Z-main-api-health-fail duration_ms=9
time=2026-11-24T08:11:08.117Z level=INFO msg="run finished" ingested=3 sent=6 waiting=3 failed=0
```

The life of a failing channel, over several runs:

```
time=2026-11-24T08:23:10.267Z level=WARN msg="channel cannot deliver; messages wait and will be retried" channel=admin-mail error="mail: connect: dial tcp 10.0.0.5:587: connect: connection refused"
time=2026-11-24T08:23:10.319Z level=INFO msg="channel still cannot deliver" channel=admin-mail error="mail: connect: dial tcp 10.0.0.5:587: connect: connection refused" failing_since=2026-11-24T08:23:10.267Z runs=2
time=2026-11-24T08:23:11.425Z level=INFO msg="message sent" channel=admin-mail module=mail id=20261124T082310Z-send-193dc6a6 duration_ms=52
time=2026-11-24T08:23:11.426Z level=INFO msg="channel delivers again" channel=admin-mail was_failing_since=2026-11-24T08:23:10.267Z
```

A message that is given up:

```
time=2026-11-24T08:11:31.400Z level=WARN msg="message will not be delivered; moved to the failed store" channel=ops-telegram id=20261124T081131Z-send-0b3895cc reason="telegram: 400 Bad Request: message is too long"
```

`json` format writes the same records as one JSON object per line.

Messages at `warn` and above are also printed on stderr, so cron mails them;
which ones those are is described in
[Output and exit codes](/output-and-exit-codes#stderr). With `--log-level` on
the command line, stderr shows everything at that level as well, so a run can
be watched in the terminal:

```console
$ onenotify send --title "debug test" --text hello --channel phone --log-level debug
time=2026-11-24T08:11:55.229Z level=INFO msg="message queued" id=20261124T081155Z-send-3c199c74 level=info title="[web-01] debug test" channels=phone
time=2026-11-24T08:11:55.229Z level=DEBUG msg="run started" config=/etc/onenotify/config.yaml host=web-01 version=0.1.0 log_file=/var/log/onenotify/onenotify.log spool=/var/lib/onenotify/spool
time=2026-11-24T08:11:55.237Z level=DEBUG msg="module loaded" module=ntfy version=0.1.0 protocol=1 path=/usr/lib/onenotify/modules/onenotify-ntfy
time=2026-11-24T08:11:55.239Z level=DEBUG msg=sending channel=phone module=ntfy id=20261124T081155Z-send-3c199c74 params="[token topic url]" timeout=30s
time=2026-11-24T08:11:55.250Z level=INFO msg="message sent" channel=phone module=ntfy id=20261124T081155Z-send-3c199c74 duration_ms=10
time=2026-11-24T08:11:55.250Z level=INFO msg="run finished" ingested=0 sent=1 waiting=0 failed=0
SENT    phone  20261124T081155Z-send-3c199c74  (ntfy, 10ms)

1 message: 1 sent, 0 waiting, 0 failed
```

`log.level` in the config changes only the log file, never stderr. Log lines
never go to stdout, which carries only the report.

### What is never logged

**Channel params are not logged at any level**: no tokens, no passwords, no
addresses. Where a request to a module is logged at `debug`, the params are
replaced by the list of their names, as in `params="[token topic url]"`
above. Error texts of the bundled modules have the secrets cut out.

The log does contain titles of messages (`title="[web-01] FAIL
main/api-health"`), never their text. At `debug` it also contains whatever a
module wrote to its stderr.

If the log file cannot be opened, onenotify prints a warning, logs to stderr
for that run, and still delivers.

## `onenotify logrotate`

onenotify does not depend on the system's logrotate. One command takes care
of both things that would otherwise grow forever: the log and the failed
store.

```sh
onenotify logrotate            # the log and the failed store
onenotify logrotate --logs     # only the log
onenotify logrotate --failed   # only the failed store
```

```console
$ onenotify logrotate
logs: rotated /var/log/onenotify/onenotify.log
failed: removed 9 old from /var/lib/onenotify/failed
$ onenotify logrotate
logs: nothing to rotate (/var/log/onenotify/onenotify.log)
failed: removed 0 old from /var/lib/onenotify/failed
```

Schedule it, for example once a day:

```cron
23 3 * * *  root  /usr/local/bin/onenotify logrotate >/dev/null
```

It does not send anything and does not need the channels' secrets in its
environment.

### Logs

```yaml
log:
  rotate:
    max_size: 10MB   # rotate when the log is larger than this
    keep: 5          # number of rotated files to keep
    max_age: 30d     # also remove rotated files older than this
    compress: true   # gzip rotated files
```

When `onenotify.log` is larger than `max_size`:

- `onenotify.log` becomes `onenotify.log.1` (or `onenotify.log.1.gz`);
- existing `onenotify.log.N` move to `N+1`;
- the next run starts a new `onenotify.log`.

On every call, rotated files with an index above `keep`, or older than
`max_age`, are removed. The log is only rotated when it is too large, so
calling `logrotate` often is harmless.

Rotation takes the run lock. If a run is in progress at that moment the log
is skipped with a message and handled by the next call:

```console
$ onenotify logrotate
logs: skipped, a run is in progress
failed: removed 0 old from /var/lib/onenotify/failed
```

### The failed store

```yaml
queue:
  failed_ttl: 7d
```

A message that expired or was rejected is kept in
[the failed store](/queue-and-delivery#the-failed-store) so that you can look
at it. `onenotify logrotate` removes the files that failed more than
`queue.failed_ttl` ago. The age of a message is counted from its `failed_at`;
a file without one, such as an unreadable event in `_unreadable/`, is judged
by its modification time.

Temporary files left in the spool or the failed store by an interrupted
write are removed once they are older than `queue.ttl`.

Messages **waiting in the spool are never touched** by `logrotate`: a run
moves them to the failed store when their deadlines pass.

Events nobody took from onecheck's queue are not onenotify's to clean:
`onecheck logrotate` removes them when they expire.
