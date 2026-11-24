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
  "text": "module: http\nheaders.Authorization: Bearer ${API_TOKEN}\nurl: https://example.com/health\ntags: api, web\nat: 2026-11-24 08:11:02 UTC\nfailed 3 runs in a row:\n- body json status: expected equals \"ok\", got \"degraded\"\n- status: expected equals 200, got 503",
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
| `error` | a `fail` event; `send --level error` | 🔴 | priority 4, tag `rotating_light` |
| `ok` | a `recovered` event; `send --level ok` | 🟢 | priority 3, tag `white_check_mark` |
| `warning` | `send --level warning` | 🟡 | priority 3, tag `warning` |
| `info` | `send` by default; an event of any other kind | ℹ️ | priority 2, tag `information_source` |

Mail carries the level in the header `X-Onenotify-Level`. Details are on the
module pages: [telegram](/modules/telegram), [mail](/modules/mail),
[ntfy](/modules/ntfy).

## From a onecheck event

onecheck writes an [event](https://alekzonder.github.io/onecheck/state-and-events#events)
for every confirmed change of a rule's state: `fail` (pass → fail) and
`recovered` (fail → pass).

**Title**: `[<host>] FAIL <config>/<rule>` or `[<host>] RECOVERED
<config>/<rule>`, where `<config>` is the `name` of the onecheck config.

**Level**: `fail` → `error`, `recovered` → `ok`.

**Tags**: the rule's `tags`.

**Text**: these lines, in this order; a line is left out when there is
nothing to say:

1. `module: <module of the rule>`
2. the rule's `params` as they are written in the onecheck config, one line
   per value, sorted
3. `tags: a, b`
4. `at: <time of the event>` in UTC
5. for `fail` only: why it failed

### A failed check

```
[web-01] FAIL main/api-health

module: http
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
url: http://127.0.0.1:18475/
tags: db
at: 2026-11-24 08:11:02 UTC
error: Get "http://127.0.0.1:18475/": dial tcp 127.0.0.1:18475: connect: connection refused
```

### A recovery

A `recovered` message has no fifth part: only the name, the params, the tags
and the time.

```
[web-01] RECOVERED main/api-health

module: http
headers.Authorization: Bearer ${API_TOKEN}
url: https://example.com/health
tags: api, web
at: 2026-11-24 08:11:20 UTC
```

### A hidden rule

For a rule with `event_params: false` in onecheck, the event is marked
`"redacted": true`. onenotify then leaves out the params and the reason of
the failure. What remains is the rule name in the title, the module, the tags
and the time:

```
[web-01] FAIL main/token-check

module: http
at: 2026-11-24 08:11:02 UTC
```

### How params are printed

Params are printed as `key: value`, one per line, sorted. Nested mappings are
flattened into `a.b.c`; a list or an empty mapping is printed as compact JSON
on one line:

```
argv: ["sh","-c","exit 1"]
deep.a.b: true
empty: {}
n: 5
```

Params are what the onecheck config file says, not what the module received:
a secret passed as `${API_TOKEN}` stays `${API_TOKEN}`, as in the examples
above.

### Limits

A notification must stay readable in a chat, so the text is bounded:

| What | Limit | When exceeded |
|---|---|---|
| one param value | 200 characters | cut, ends with `…` |
| param lines | 20 | the rest is replaced by `… and N more params` |
| failed conditions | 10 | the rest is replaced by `… and N more` |
| an error or one condition's message | 300 characters | cut, ends with `…` |

Line breaks, tabs and other control characters inside a value, an error or a
condition's message become spaces: each stays on its line. Times are always
UTC. Each module additionally fits the whole message into what its channel
accepts.

### Other events

- **An event of an older onecheck** has no `params` and no `redacted` key. It
  is delivered like any other, without param lines, and is treated as not
  hidden:

  ```
  [web-01] FAIL main/disk-root

  module: disk
  at: 2026-11-24 09:00:02 UTC
  failed:
  - used_percent: expected lt 90, got 93.4
  ```

- **An event with a `transition` onenotify does not know** gets the
  transition in upper case in the title (`[web-01] FLAPPING main/odd`), the
  level `info`, and no failure lines. Unknown keys of an event are ignored.
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

**Never included**: the saved output of the check (`result.output` of the
event: a response body, a command's stdout). It stays on the host in
onecheck's result files.

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
params, no failure messages, no error text. `event_params: false` can be set
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
