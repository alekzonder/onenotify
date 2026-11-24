# Messages

Everything onenotify delivers is a **message**: a title, a text, a level and a
few fields around them. A message is built in one of two ways: from a onecheck
event, by fixed rules described here, or by [`onenotify send`](/sending) from
what you pass on the command line. Modules receive the finished message and
only decide how to show it in their channel.

There are no templates: the wording of a notification is the same on every
host and in every channel.

## Fields

A message as it lies in a channel's queue:

```json
{
  "version": 1,
  "id": "20261124T081102Z-main-api-health-fail",
  "source": "onecheck",
  "host": "web-01",
  "level": "error",
  "title": "[web-01] FAIL main/api-health",
  "text": "module: http\nparams:\n  headers.Authorization: Bearer ${API_TOKEN}\n  url: https://example.com/health\ntags: api, web\nat: 2026-11-24 08:11:02 UTC\nfailed 3 runs in a row:\n- body json status: expected equals \"ok\", got \"degraded\"\n- status: expected equals 200, got 503",
  "tags": [
    "api",
    "web"
  ],
  "created_at": "2026-11-24T08:11:02.393363196Z",
  "expires_at": "2026-11-25T08:11:02.393363196Z"
}
```

| Field | Description |
|---|---|
| `version` | version of the queue file format, `1` |
| `id` | unique id. It starts with a UTC time `YYYYMMDDTHHMMSSZ`, so sorting ids sorts messages by time. For an event it is the event's id, for `send` it is `<UTC time>-send-<8 hex digits>`: the microsecond, then a random part |
| `source` | `onecheck` or `send` |
| `host` | the host name, see [`host`](/configuration#host) |
| `level` | `error`, `ok`, `warning` or `info` |
| `title` | one line, already starting with `[<host>] ` |
| `text` | plain text of any number of lines; may be empty |
| `tags` | list of strings; may be empty |
| `created_at` | when it happened (UTC): the event's `at`, or the time `send` was called |
| `expires_at` | the delivery deadline: after it the message is not sent any more, see [Queue and delivery](/queue-and-delivery#the-deadline) |

A module receives all of these except `version` and `expires_at`.

## Levels

The level tells a channel how to present a message: an icon, a priority, a
sound. It never decides whether or where a message is sent.

| Level | Comes from | telegram | ntfy |
|---|---|---|---|
| `error` | a `fail` or `reminder` event; `send --level error` | 🔴 | priority 4, tag `rotating_light` |
| `ok` | a `recovered` event; `send --level ok` | 🟢 | priority 3, tag `white_check_mark` |
| `warning` | `send --level warning` | 🟡 | priority 3, tag `warning` |
| `info` | `send` by default; an event of any other kind | ℹ️ | priority 2, tag `information_source` |

Mail carries the level in the header `X-Onenotify-Level`. Details are on the
module pages: [telegram](/modules/telegram), [mail](/modules/mail),
[ntfy](/modules/ntfy).

## From a onecheck event

onecheck writes an [event](https://alekzonder.github.io/onecheck/state-and-events#events)
for every confirmed change of a rule's state, `fail` (pass → fail) and
`recovered` (fail → pass), and a `reminder` every `remind_every` while the
rule stays failed.

**Title**: `[<host>] <WORD> <config>/<rule>`, where `<config>` is the `name`
of the onecheck config.

| Event | Word in the title | Level |
|---|---|---|
| `fail` | `FAIL` | `error` |
| `reminder` | `STILL FAILING` | `error` |
| `recovered` | `RECOVERED` | `ok` |

**Tags**: the rule's `tags`.

**Text**: these lines, in this order; a line is left out when there is
nothing to say:

1. `module: <module of the rule>`
2. `params:` and under it the rule's `params` as they are written in the
   onecheck config, [indented](#how-params-are-printed), one line per value,
   sorted
3. `tags: a, b`
4. `at: <time of the event>` in UTC
5. for `reminder` only: `failing since: <time of the fail event>` in UTC
6. for `fail` and `reminder`: why it fails
7. for `fail` and `reminder`: the [output of the check](#output-of-the-check),
   if the rule allows it

### A failed check

```
[web-01] FAIL main/api-health

module: http
params:
  headers.Authorization: Bearer ${API_TOKEN}
  url: https://example.com/health
tags: api, web
at: 2026-11-24 08:11:02 UTC
failed 3 runs in a row:
- body json status: expected equals "ok", got "degraded"
- status: expected equals 200, got 503
```

The first line is the title, the rest is the text. `failed 3 runs in a row:`
is the rule's count of consecutive failures (`fail_after: 3` here); a rule
that failed once says `failed:`. Below it is one line per condition that did
not hold, with onecheck's own message.

### A check that could not run

When the run that caused the event had status `error` (no connection, a
timeout, a missing module), the reason is given instead of conditions:

```
[web-01] FAIL main/db-port

module: http
params:
  url: http://127.0.0.1:18475/
tags: db
at: 2026-11-24 08:11:02 UTC
error: Get "http://127.0.0.1:18475/": dial tcp 127.0.0.1:18475: connect: connection refused
```

### A reminder

onecheck writes a `fail` event once. While nobody fixed the problem, it
writes a `reminder` every `remind_every` (60 minutes by default, see
onecheck's
[State and events](https://alekzonder.github.io/onecheck/state-and-events#reminders)).
onenotify delivers it as what it is, a failure that goes on:

```
[web-01] STILL FAILING main/api-health

module: http
params:
  headers.Authorization: Bearer ${API_TOKEN}
  url: https://example.com/health
tags: api, web
at: 2026-11-24 09:11:14 UTC
failing since: 2026-11-24 08:11:02 UTC
failed 63 runs in a row:
- status: expected equals 200, got 502
```

`failing since:` is the time of the `fail` event. The reason is that of the
run that wrote the reminder: the problem as it is now, which may differ from
how it began. How often reminders come, and whether they come at all, is
decided in onecheck (`remind_every`); onenotify sends every one it finds.

### Output of the check

A failed `exit_code: 0` does not say what went wrong; the last lines the
command printed do. onecheck saves them in the event, and a rule can allow a
notification to show them:

```yaml
# onecheck config
rules:
  - name: backup
    module: command
    event_output: [stderr]      # a notification may show stderr
    params: { run: /usr/local/bin/backup.sh }
    check: { exit_code: 0 }
```

A `fail` or `reminder` message of this rule then ends with the output:

```
[web-01] FAIL main/backup

module: command
params:
  run: /usr/local/bin/backup.sh
tags: backup
at: 2026-11-24 03:00:41 UTC
failed:
- exit_code: expected equals 0, got 12
stderr:
…
rsync: connection unexpectedly closed (0 bytes received so far) [sender]
rsync error: error in rsync protocol data stream (code 12) at io.c(232)
```

- Only the fields the rule names in `event_output` are shown, in that order,
  each under a line `<field>:`. Without `event_output`, which is the
  default, there is no output in a notification.
- Output keeps its lines. Colours and other terminal escape sequences are
  removed, a carriage return starts a new line, a tab or another control
  character becomes a space, spaces at line ends and empty lines at the
  start and the end are dropped.
- A field shows its **end**: the last 20 lines and at most 1000 characters,
  because what went wrong is printed last. Lines left out are marked by a
  line `…` on top. If the last line alone is longer, its last 1000
  characters are shown after a `…`.
- A field that holds a mapping, such as `headers` of the `http` module, is
  printed as `key: value` lines, [like params](#how-params-are-printed).
- A field that is empty, or missing because the rule has `output_tail: 0`,
  is left out together with its heading.
- A `recovered` message never shows output: there is nothing to explain.

### A recovery

A `recovered` message stops after the time: only the name, the params, the
tags and `at:`. No reason, no output.

```
[web-01] RECOVERED main/api-health

module: http
params:
  headers.Authorization: Bearer ${API_TOKEN}
  url: https://example.com/health
tags: api, web
at: 2026-11-24 08:11:20 UTC
```

### A hidden rule

For a rule with `event_params: false` in onecheck, the event is marked
`"redacted": true`. onenotify then leaves out the params, the reason of the
failure and the output, whatever `event_output` says. What remains is the
rule name in the title, the module, the tags and the time (in a reminder,
`failing since:` too):

```
[web-01] FAIL main/token-check

module: http
at: 2026-11-24 08:11:02 UTC
```

### How params are printed

Params are printed as a block: a line `params:` and under it `key: value`,
one per line, sorted, indented by two spaces. Nested mappings are flattened
into `a.b.c`; a list or an empty mapping is printed as compact JSON on one
line:

```
params:
  argv: ["sh","-c","exit 1"]
  deep.a.b: true
  empty: {}
  n: 5
```

The block keeps a param from passing for a line of the notification: a
`systemd` rule with `state: failed` in its params says so under `params:` in
its `RECOVERED` message too, where it is plainly the rule's setting, not the
state of the unit. A rule without params has no `params:` line.

Params are what the onecheck config file says, not what the module received:
a secret passed as `${API_TOKEN}` stays `${API_TOKEN}`, as in the examples
above.

### Limits

A notification must stay readable in a chat, so the text is bounded:

| What | Limit | When exceeded |
|---|---|---|
| one param value | 200 characters | cut, ends with `…` |
| param lines | 20 | the rest is replaced by an indented `… and N more params` |
| failed conditions | 10 | the rest is replaced by `… and N more` |
| an error or one condition's message | 300 characters | cut, ends with `…` |
| one output field | the last 20 lines, 1000 characters | the beginning is left out, marked with `…` |

Line breaks, tabs and other control characters inside a value, an error or a
condition's message become spaces: each stays on its line. Only
[output](#output-of-the-check) keeps its line breaks. Times are always
UTC. Each module additionally fits the whole message into what its channel
accepts.

### Other events

- **An event of an older onecheck** has no `params`, `redacted`, `since` or
  `output_fields` key. It is delivered like any other, without the `params:` block
  and without output, and is treated as not hidden:

  ```
  [web-01] FAIL main/disk-root

  module: disk
  at: 2026-11-24 09:00:02 UTC
  failed:
  - used_percent: expected lt 90, got 93.4
  ```

- **An event with a `transition` onenotify does not know** gets the
  transition in upper case in the title (`[web-01] FLAPPING main/odd`), the
  level `info`, and neither failure lines nor output. Unknown keys of an event are ignored.
- **The deadline** is the event's `expires_at` (onecheck's `events.ttl`,
  `24h` by default). An event without one gets `at` + `queue.ttl`.
- **An event without `result`** is still an event: its notification has the
  title and the `at:` line.
- **A file that is not an event** (not a JSON object, without `id`, `rule` or
  `transition`, or with a value that cannot be stored, such as a date beyond
  the year 9999) is moved to `<paths.failed>/_unreadable/` with a warning
  and removed from the source, see
  [Queue and delivery](/queue-and-delivery#intake).

## What a notification can reveal

A notification leaves the host: to Telegram, to ntfy.sh, to a mail server.
Know what is in it.

**Included only when the rule allows it: the saved output of the check**
(`result.output` of the event: a response body, a command's stdout and
stderr). By default none of it leaves the host. A rule that names fields in
`event_output` puts the last 20 lines of each, up to 1000 characters, into
its `fail` and `reminder` notifications, see
[Output of the check](#output-of-the-check). Allow only output you would
paste into the chat yourself: a script that prints its environment or a
response body with personal data does not belong there.

**Included: the params**, as written in the onecheck config. A secret passed
as `${VAR}` is safe; a secret typed into the onecheck config as plain text is
sent. Use `${VAR}` there, see onecheck's
[Configuration](https://alekzonder.github.io/onecheck/configuration#environment-variables-in-params).

**Included: the reason of a failure.** A condition's message names the value
that was found: `... got "degraded"`. For a condition on a response body or
on a command's output, that value is a piece of the body or output, and up to
300 characters of it per line are sent. The same goes for an error text.

To keep a rule's details out of notifications, hide the rule **in onecheck**:

```yaml
# onecheck config
rules:
  - name: token-check
    module: http
    event_params: false   # the event is marked "redacted"
    params: { url: https://example.com/token }
    check: { status: 200 }
```

Its notifications then look like [a hidden rule](#a-hidden-rule) above: no
params, no failure messages, no error text, no output. `event_params: false` can be set
for all rules in onecheck's `defaults`. See onecheck's
[State and events](https://alekzonder.github.io/onecheck/state-and-events#what-an-event-can-reveal).

onenotify's own secrets, the channel params, are never part of a message, of
a queue file or of the log.

## From `onenotify send`

`onenotify send --title "backup finished" --text "42 GB in 13m"` gives:

```
[web-01] backup finished

42 GB in 13m
```

The title is `[<host>] ` plus `--title` on one line, the text is taken as it
is, the level is `info` unless `--level` says otherwise, and the deadline is
the time of the call plus `queue.ttl`. See [Sending](/sending).
