---
title: Configuration
---

The config is YAML. Unknown keys are errors, so a typo never goes unnoticed.
Check a config without sending anything: `onenotify validate -c <file>`.

The config holds or points to secrets: keep it readable by its owner only
(`chmod 600`) and pass tokens and passwords as `${VAR}`, see
[Getting started](/getting-started#secrets).

## Full reference

```yaml
host: web-01                  # host name in every notification; default: the system host name

log:
  level: info                 # debug | info | warn | error; --log-level overrides
  dir: /var/log/onenotify     # log file: <dir>/onenotify.log
  format: text                # text | json
  rotate:                     # rules for "onenotify logrotate"
    max_size: 10MB            # rotate when the log is larger
    keep: 5                   # rotated files to keep
    max_age: 30d              # remove rotated files older than this
    compress: true            # gzip rotated files

paths:
  modules: /usr/lib/onenotify/modules   # where onenotify-<name> executables live
  spool:   /var/lib/onenotify/spool     # one queue directory per channel
  failed:  /var/lib/onenotify/failed    # messages that expired or were rejected

sources:                      # where events are taken from
  - type: onecheck            # the only type
    path: /var/lib/onecheck/events

include:                      # more channel files (globs)
  - conf.d/*.yaml

queue:
  ttl: 24h                    # delivery deadline of a message that has none of its own
  failed_ttl: 7d              # how long the failed store keeps a message

limits:
  text: 64KB                  # a longer text given to "onenotify send" is cut

run:
  parallel: 4                 # channels served at the same time

defaults:                     # used by channels that do not set the key
  timeout: 30s

modules:
  repos:                      # default repositories for "onenotify module search"
    - alekzonder/onenotify

channels:
  - name: ops-telegram        # REQUIRED, unique. [a-z0-9][a-z0-9_.-]*
    module: telegram          # REQUIRED. module name: [a-z0-9][a-z0-9_-]*
    enabled: true             # false: nothing is queued for or sent to the channel
    timeout: 10s              # the module is killed after this
    params:                   # input of the module, see the module's page
      token: "${TELEGRAM_TOKEN}"
      chat_id: "-1001234567890"
```

Every key is optional; the values above are the defaults for root. A channel
needs `name` and `module`. Sizes accept `B`, `KB`, `MB`, `GB`, `TB`
(1024-based); durations accept `ms`, `s`, `m`, `h`, `d`, `w` and combinations
such as `1h30m`. `queue.ttl`, `queue.failed_ttl`, `limits.text`,
`defaults.timeout` and a channel's `timeout` must be positive, `run.parallel`
at least `1`.

There is no `name` key as in onecheck: one config is one instance with one
spool. To run two instances on a host, give each config its own
`paths.spool`, `paths.failed` and `log.dir`. onenotify remembers which config
file owns a spool (`<paths.spool>/.owner`) and refuses to run a second one on
it, see [Queue and delivery](/queue-and-delivery#the-lock).

## `host`

Every notification starts with the host name: `[web-01] FAIL
main/api-health`. By default it is the system host name. Set `host` to use
another one, for example a short name instead of an FQDN.

`host` must be a single non-empty line. If the key is absent and the system
host name cannot be found out, the config is rejected.

## Default paths

| | root | other users |
|---|---|---|
| `log.dir` | `/var/log/onenotify` | `$HOME/.local/state/onenotify/log` |
| `paths.modules` | `/usr/lib/onenotify/modules` | `$HOME/.local/lib/onenotify/modules` |
| `paths.spool` | `/var/lib/onenotify/spool` | `$HOME/.local/state/onenotify/spool` |
| `paths.failed` | `/var/lib/onenotify/failed` | `$HOME/.local/state/onenotify/failed` |
| `sources` | one: `/var/lib/onecheck/events` | one: `$HOME/.local/state/onecheck/events` |

Relative paths in the config (`log.dir`, `paths.*`, the `path` of a source)
are resolved against the directory of the config file.

`paths.spool` and `paths.failed` must be different directories, and neither
may lie inside the other; a config that breaks this is rejected.

onenotify creates the log, spool and failed directories with mode `0750` and
the files in them with mode `0640`. Messages can quote what was checked, so
they are not readable by everyone.

## `sources`

A source is a directory events are taken from. The only type is `onecheck`:
the [event queue](https://alekzonder.github.io/onecheck/state-and-events) of
a onecheck, its `paths.events`.

- **Without the key** there is one source: the default event queue of a
  onecheck run by the same user (the last row of the table above). A onecheck
  with default paths needs no `sources` in onenotify.
- **With the key** the list replaces the default. Name every directory, for
  example when onecheck has its own `paths.events`:

  ```yaml
  sources:
    - type: onecheck
      path: /srv/onecheck/events
  ```

- **`sources: []`** turns the intake off. onenotify then only delivers what
  `onenotify send` queues.

A source directory that does not exist yet is not an error: onecheck creates
its event queue on its first run. `onenotify validate` prints a warning.

onenotify deletes each event it has taken, so it must run as the owner of the
directory, see [Getting started](/getting-started#run-it-as-the-user-that-runs-onecheck).
Two onenotify instances must not read the same source: each event would go to
whichever took it first.

## `channels`

| Key | Required | Default | Description |
|---|---|---|---|
| `name` | yes | | unique name; lower-case letters, digits, `_`, `.`, `-`, starting with a letter or digit. It is the directory name of the channel's queue |
| `module` | yes | | the module that delivers: `telegram`, `mail`, `ntfy` or one you installed |
| `enabled` | no | `true` | `false`: no message is queued for the channel and nothing is sent to it |
| `timeout` | no | `defaults.timeout` (`30s`) | time one delivery may take; after it the module is killed and the message waits |
| `params` | depends on the module | | the module's input, checked against the module's schema |

Every event and every `onenotify send` without `--channel` goes to every
enabled channel. There is no routing by tag, config or kind of event.

Renaming a channel starts a new queue: messages still waiting under the old
name are not delivered and expire, see
[Queue and delivery](/queue-and-delivery#disabled-and-removed-channels).

## Includes (`conf.d/`)

```yaml
include:
  - conf.d/*.yaml
  - /etc/onenotify/team-*/channels.yaml
```

- Patterns are globs; relative ones are resolved against the config file's
  directory. A pattern that matches nothing is fine.
- Files are read in lexicographic order, after the channels of the main file.
- An include file may contain only the `channels` key:

  ```yaml
  # conf.d/team.yaml
  channels:
    - name: team-ntfy
      module: ntfy
      params:
        topic: team-alerts
  ```

- A channel name used twice, in any files, is an error naming both files.

## Environment variables in `params`

Strings inside a channel's `params` may reference environment variables as
`${VAR}`, which keeps secrets out of the config file:

```yaml
params:
  token: "${TELEGRAM_TOKEN}"
  chat_id: "-1001234567890"
```

- Only the `${VAR}` form is expanded, and only in `params`. Nothing else in
  the config is expanded.
- Write `$${VAR}` to get a literal `${VAR}`. Every other `$` is passed
  through unchanged.
- A variable that is not set is never replaced by an empty string. It makes
  that one channel unusable: `onenotify validate` reports it, and a run keeps
  the channel's messages in its queue until a run that has the variable. The
  other channels are not affected.
- A `${` without its closing `}` makes the channel unusable in the same way.
  The error does not quote the value.

  ```console
  $ onenotify send --title "env test" --channel phone
  time=2026-11-24T08:11:55.139Z level=WARN msg="channel cannot deliver; messages wait and will be retried" channel=phone error="environment variable NTFY_TOKEN is not set (used as ${NTFY_TOKEN} in params)"
  WAIT    phone  20261124T081155Z-send-0e6b2034  (ntfy, 0ms)
          environment variable NTFY_TOKEN is not set (used as ${NTFY_TOKEN} in params)

  1 message: 0 sent, 1 waiting, 0 failed
  ```

Where to define the variables for cron and systemd:
[Getting started](/getting-started#secrets).

Channel params are never written to the log, to the output or to the queue
files, expanded or not. Error messages name a param, never its value.

## Logging level

The level comes from, in order: `--log-level`, `log.level`, `info`.

## Two kinds of config errors

**Errors of the whole config** stop onenotify with exit code `2` before
anything is taken or sent: unreadable or invalid YAML, an unknown key, an
empty `host`, an unknown source type, `paths.spool` and `paths.failed` that
are the same or nested, a channel without a name or a module,
an invalid or duplicate channel name, an include file with keys other than
`channels`.

**Errors of a single channel** make only that channel unusable. On a run its
messages wait in its queue, the reason is reported like any other failed
delivery, and every other channel is served. This covers: an environment
variable that is not set, a module that is not installed or not compatible,
`params` the module does not accept.

`onenotify validate` prints all errors of both kinds and exits with `2` if
there are any:

```console
$ onenotify validate
/etc/onenotify/config.yaml: channel a: invalid params: /: missing topic; /: unknown key topik; /priority: does not satisfy maximum
/etc/onenotify/config.yaml: channel b: invalid params: /: does not satisfy password; /to: does not satisfy minItems
/etc/onenotify/config.yaml: channel c: invalid params: /: unknown key extra
onenotify: warning: source /var/lib/onecheck/events does not exist yet
onenotify: 3 channel(s) of /etc/onenotify/config.yaml are not usable
```

Each line names the file the channel was read from, the channel and what is
wrong with it; a param is named, its value never is.

It checks disabled channels too. It warns, without failing, about a source
directory that does not exist yet and about a config with no enabled channel:

```console
$ onenotify validate
onenotify: warning: no channel is enabled; events will stay in their source
config OK: 1 channel, 1 source (/etc/onenotify/config.yaml)
```

## Where everything is: `onenotify paths`

`onenotify paths` prints the full path of everything onenotify reads and
writes for the config it would use, so the paths do not have to be worked out
from the tables above:

```console
$ onenotify paths
config    /etc/onenotify/config.yaml  (from default location)
host      web-01
include   /etc/onenotify/conf.d/team.yaml
log       /var/log/onenotify/onenotify.log  (missing)
spool     /var/lib/onenotify/spool  (missing)
failed    /var/lib/onenotify/failed  (missing)
source    /var/lib/onecheck/events
modules   /usr/lib/onenotify/modules
binary    /usr/local/lib/onenotify/versions/0.1.0/onenotify
```

| Line | Meaning |
|---|---|
| `config` | the config file and how it was found: `--config`, `ONENOTIFY_CONFIG` or `default location` |
| `host` | the host name notifications carry |
| `include` | every include file that was read, in reading order |
| `log` | the log file, see [Logging and logrotate](/logging-and-logrotate) |
| `spool` | the queues: one directory per channel, see [Queue and delivery](/queue-and-delivery#directories) |
| `failed` | messages that expired or were rejected, kept for inspection |
| `source` | a directory events are taken from; one line per source |
| `modules` | where modules are looked for |
| `binary` | the onenotify executable itself, with symlinks resolved |

`(missing)` marks a path that does not exist yet. onenotify creates the log,
spool and failed paths when they are first needed, so they are missing on a
fresh install.

The command changes nothing and does not need the modules to be installed.
If the config file is found but cannot be loaded, its path is still printed,
followed by the error, and the exit code is `2`.

`onenotify paths --json` prints the same for scripts:

```json
{
  "host": "web-01",
  "config": {
    "path": "/etc/onenotify/config.yaml",
    "exists": true,
    "source": "default location"
  },
  "includes": [
    {
      "path": "/etc/onenotify/conf.d/team.yaml",
      "exists": true
    }
  ],
  "log": {
    "path": "/var/log/onenotify/onenotify.log",
    "exists": false
  },
  "spool": {
    "path": "/var/lib/onenotify/spool",
    "exists": false
  },
  "failed": {
    "path": "/var/lib/onenotify/failed",
    "exists": false
  },
  "sources": [
    {
      "path": "/var/lib/onecheck/events",
      "exists": true
    }
  ],
  "modules": {
    "path": "/usr/lib/onenotify/modules",
    "exists": true
  },
  "binary": {
    "path": "/usr/local/lib/onenotify/versions/0.1.0/onenotify",
    "exists": true
  }
}
```
