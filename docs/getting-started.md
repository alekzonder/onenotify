# Getting started

## Install

onenotify is one static binary plus one executable per delivery module.

### With the install script

```sh
curl -fsSL https://raw.githubusercontent.com/alekzonder/onenotify/main/install.sh | sh
```

The script detects the system and CPU, asks GitHub for the latest release,
verifies the download against the release's `checksums.txt`, and installs
onenotify together with the bundled modules (`telegram`, `mail`, `ntfy`) of
the same version.

A specific version:

```sh
curl -fsSL https://raw.githubusercontent.com/alekzonder/onenotify/main/install.sh | sh -s -- --version 0.1.0
```

Every version gets its own directory, and a symlink in a bin directory points
at the chosen one:

| | Linux | macOS |
|---|---|---|
| versions | `/usr/local/lib/onenotify/versions/<version>/onenotify` | `~/.local/share/onenotify/versions/<version>/onenotify` |
| symlink | `/usr/local/bin/onenotify` | `~/.local/bin/onenotify` |
| modules | `/usr/lib/onenotify/modules` | `~/.local/lib/onenotify/modules` |

On Linux the install is system-wide, and the script calls `sudo` when it is
not run as root. On macOS everything stays in the home directory.

| Option | Description |
|---|---|
| `--version <v>` | version to install, e.g. `0.1.0`; default: the latest release. Also `ONENOTIFY_VERSION` |
| `--root <dir>` | where versions are kept, as `<dir>/versions/<v>/onenotify` |
| `--bin-dir <dir>` | where the `onenotify` symlink is created |
| `--modules-dir <dir>` | where the bundled modules go |
| `--no-modules` | install onenotify only, without modules |

Set `GITHUB_TOKEN` if GitHub's rate limit gets in the way.

To upgrade, downgrade or switch back, run the script again. A version that is
already installed is not downloaded again: the symlink is just switched to
it. Older versions are kept; delete their directories under `versions/` when
you no longer need them. `onenotify self-update` refuses to touch a binary
installed this way, because it would overwrite one version's directory with
another version.

The modules directories above are the defaults of `paths.modules`: the Linux
one for root, the macOS one for a regular user. If you run onenotify on Linux
as a regular user, set `paths.modules: /usr/lib/onenotify/modules` in the
config, or install with `--modules-dir ~/.local/lib/onenotify/modules`.

### By hand

1. Download `onenotify_<os>_<arch>` from the
   [releases](https://github.com/alekzonder/onenotify/releases), verify it
   against `checksums.txt`, and put it into `PATH` as `onenotify`:

   ```sh
   install -m 0755 onenotify_linux_amd64 /usr/local/bin/onenotify
   onenotify version
   ```

2. Install the bundled modules (`telegram`, `mail`, `ntfy`):

   ```sh
   onenotify module install alekzonder/onenotify --modules-dir /usr/lib/onenotify/modules
   onenotify module list --modules-dir /usr/lib/onenotify/modules
   ```

   Once a config exists, `--modules-dir` is not needed: the directory is
   taken from `paths.modules`.

### From source

```sh
git clone https://github.com/alekzonder/onenotify && cd onenotify
make build
install -m 0755 dist/onenotify /usr/local/bin/onenotify
install -d /usr/lib/onenotify/modules
install -m 0755 dist/modules/onenotify-* /usr/lib/onenotify/modules/
```

`make build` needs Go (the version in `go.mod` or newer) and puts the binary
into `dist/onenotify` and the modules into `dist/modules/`.

## Run it as the user that runs onecheck

onenotify removes each event from onecheck's event queue after it has taken
it. onecheck creates that directory with mode `0750`, so only its owner can
delete files there. Run onenotify as the same user as onecheck, usually root.
That is also what makes the default work without configuration: without a
`sources` key onenotify reads the default event queue of a onecheck run by
the same user.

## First config

onenotify looks for its config in this order:

1. `--config <path>` (`-c`)
2. `$ONENOTIFY_CONFIG`
3. `/etc/onenotify/config.yaml`
4. `$HOME/.config/onenotify/config.yaml`

A config needs nothing but channels. Every key has a default, and every
event and every `onenotify send` goes to every enabled channel:

```yaml
# /etc/onenotify/config.yaml
channels:
  - name: ops-telegram
    module: telegram
    params:
      token: "${TELEGRAM_TOKEN}"
      chat_id: "-1001234567890"

  - name: admin-mail
    module: mail
    params:
      host: smtp.example.com
      username: alerts@example.com
      password: "${SMTP_PASSWORD}"
      from: "Alerts <alerts@example.com>"
      to:
        - admin@example.com

  - name: phone
    module: ntfy
    params:
      topic: my-server-alerts
      token: "${NTFY_TOKEN}"
```

Each channel has three parts:

- `name` — your name for the destination; it appears in the output, in the
  log and as a directory name of the queue;
- `module` — which module delivers;
- `params` — the module's input: where to deliver and with which
  credentials. See [telegram](/modules/telegram), [mail](/modules/mail),
  [ntfy](/modules/ntfy).

See [examples/config.yaml](https://github.com/alekzonder/onenotify/blob/main/examples/config.yaml)
for a fuller example and [Configuration](/configuration) for every key.

## Secrets

Channel params hold tokens and passwords. Two rules keep them where they
belong:

- **Keep the config readable by its owner only**: `chmod 600
  /etc/onenotify/config.yaml`.
- **Pass secrets as `${VAR}`**, as in the config above. onenotify replaces
  `${VAR}` in channel params with the value of the environment variable, so
  the config file can be shown, copied and committed without the secrets.

Put the variables into one file that both cron and systemd can read: plain
`NAME=value` lines, no `export`, no spaces around `=`.

```sh
# /etc/onenotify/env
TELEGRAM_TOKEN=123456:AAF...
SMTP_PASSWORD=...
NTFY_TOKEN=tk_...
```

```sh
chmod 600 /etc/onenotify/env
```

In a shell, load it before running onenotify by hand:

```sh
set -a; . /etc/onenotify/env; set +a
```

A variable that is not set does not break the run. Only the channel that uses
it is unusable: its messages are queued and wait until a run that has the
variable. `onenotify validate` reports it:

```console
$ onenotify validate
/etc/onenotify/config.yaml: channel admin-mail: environment variable SMTP_PASSWORD is not set (used as ${SMTP_PASSWORD} in params)
onenotify: 1 channel(s) of /etc/onenotify/config.yaml are not usable
$ echo $?
2
```

## Validate and try it

```sh
onenotify validate      # reports every config problem at once, sends nothing
onenotify paths         # where the config, log, queues and sources are
onenotify send --title test                       # a test message to every channel
onenotify send --title test --channel admin-mail  # or to one channel
```

`validate` checks the config, that every channel's module is installed and
compatible, and that the params fit the module. Whether a token or an address
really works is only known by sending:

```console
$ onenotify send --title test
time=2026-11-24T08:17:01.826Z level=WARN msg="channel cannot deliver; messages wait and will be retried" channel=admin-mail error="mail: connect: dial tcp 10.0.0.5:587: connect: connection refused"
WAIT    admin-mail    20261124T081701Z-send-b1e90170  (mail, 5ms)
        mail: connect: dial tcp 10.0.0.5:587: connect: connection refused
SENT    ops-telegram  20261124T081701Z-send-b1e90170  (telegram, 12ms)
SENT    phone         20261124T081701Z-send-b1e90170  (ntfy, 10ms)

3 messages: 2 sent, 1 waiting, 0 failed
```

The message arrives as `[web-01] test`: the host name is put in front of
every title. A message that could not be delivered is not lost: it waits in
the channel's queue and goes out with a later run, see
[Queue and delivery](/queue-and-delivery).

The exit code is `0` when nothing is left undelivered, `1` when something is
waiting or was given up, `2` when onenotify could not run at all. Details:
[Output and exit codes](/output-and-exit-codes).

## Schedule it

onenotify has no scheduler of its own and no daemon. Each run takes the new
events and retries what is waiting.

### cron

```cron
# /etc/cron.d/onenotify
* * * * *   root  set -a; . /etc/onenotify/env; /usr/local/bin/onenotify >/dev/null
23 3 * * *  root  /usr/local/bin/onenotify logrotate >/dev/null
```

The first line delivers once a minute; `set -a; . /etc/onenotify/env` gives
the run the secrets. The second line rotates the log and cleans the failed
store once a day, see [Logging and logrotate](/logging-and-logrotate).

stdout is thrown away and stderr is left for cron to mail. onenotify writes a
warning to stderr the first time a channel fails and when a message is given
up, not on every retry, so a channel that is down for an hour produces one
mail, not sixty.

With a run every minute a notification is delayed by up to a minute after
onecheck's own run.

### systemd: instant delivery

A `.path` unit starts onenotify the moment onecheck writes an event; a timer
next to it retries what could not be delivered.

```ini
# /etc/systemd/system/onenotify.service
[Unit]
Description=onenotify: deliver notifications

[Service]
Type=oneshot
EnvironmentFile=/etc/onenotify/env
ExecStart=/usr/local/bin/onenotify
# exit code 1 means "something is still waiting", which is not a unit failure
SuccessExitStatus=1
```

```ini
# /etc/systemd/system/onenotify.path
[Unit]
Description=Run onenotify when onecheck writes an event

[Path]
PathChanged=/var/lib/onecheck/events
Unit=onenotify.service

[Install]
WantedBy=multi-user.target
```

```ini
# /etc/systemd/system/onenotify.timer
[Unit]
Description=Retry waiting notifications every minute

[Timer]
OnCalendar=minutely
AccuracySec=5s

[Install]
WantedBy=timers.target
```

```sh
systemctl daemon-reload
systemctl enable --now onenotify.path onenotify.timer
```

- The service runs as root unless you add `User=`; use the user that runs
  onecheck.
- `EnvironmentFile=` is where the secrets come from. Without secrets in the
  environment, drop the line.
- The path unit watches the directory `onenotify paths` prints as `source`.
  It only reacts to changes in that directory, so it does not retry by
  itself; that is the timer's job. The timer also picks up an event written
  while a run was already in progress.
- Both units start the same service; systemd never runs two instances of it.
  If a run is started by hand at that moment, the later one exits with `2`
  ("another onenotify run is still in progress") and the next start does the
  work.

Keep the daily `onenotify logrotate` in cron, or give it a service and a
timer of its own.

## Messages of your own

Any script can send through the same channels:

```sh
onenotify send --title "backup finished" --text "42 GB in 13m"
```

See [Sending](/sending).

## What gets written where

With the default paths, for root:

| What | Where |
|---|---|
| messages waiting for delivery | `/var/lib/onenotify/spool/<channel>/` |
| messages that expired or were rejected | `/var/lib/onenotify/failed/<channel>/` |
| log | `/var/log/onenotify/onenotify.log` |
| modules | `/usr/lib/onenotify/modules/` |
| events are taken from | `/var/lib/onecheck/events/` |

For other users everything lives under `$HOME/.local/state/onenotify/` and
`$HOME/.local/lib/onenotify/modules/`. All of it is configurable:
[Configuration](/configuration).
