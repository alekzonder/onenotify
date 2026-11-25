---
title: Sending your own messages
---

`onenotify send` sends a message of your own through the channels of the
config. It is how a backup script, a deploy script or a cron job tells you
something, with the same queue, retries and look as the notifications about
checks.

```console
$ onenotify send --title "backup finished" --text "42 GB in 13m"
SENT    admin-mail    20261124T081846Z-send-4440ec99  (mail, 54ms)
SENT    ops-telegram  20261124T081846Z-send-4440ec99  (telegram, 9ms)
SENT    phone         20261124T081846Z-send-4440ec99  (ntfy, 14ms)

3 messages: 3 sent, 0 waiting, 0 failed
```

The message arrives with the host name in front of its title:

```
[web-01] backup finished

42 GB in 13m
```

## Flags

```
onenotify send --title <text> [--text <text> | --text-file <path>] [--level <level>]
               [--tag <tag>]... [--channel <name>]... [--queue-only] [-o text|json]
```

| Flag | Default | Description |
|---|---|---|
| `--title <text>` | required | the title, one line. It must not be empty. Line breaks and tabs in it become spaces |
| `--text <text>` | | the text of the message |
| `--text-file <path>` | | read the text from this file; `-` reads standard input |
| `--level <level>` | `info` | `info`, `ok`, `warning` or `error`: how the channels present the message |
| `--tag <tag>` | | a tag of the message; repeatable |
| `--channel <name>` | every enabled channel | send only to this channel; repeatable |
| `--queue-only` | | only queue the message; the next run delivers it |
| `-o, --output text\|json` | `text` | output format |
| `-c, --config <path>` | | the config file, as for every command |

## The text

The text is plain text of any number of lines. It comes from one of:

- `--text "..."` on the command line;
- `--text-file <path>`: the content of a file;
- `--text-file -`: standard input;
- neither: the message has a title only.

`--text` together with `--text-file` is an error.

What happens to the text before it is queued:

- Line breaks at the end are removed. An empty file or an empty standard
  input gives a message without text.
- Bytes that are not valid UTF-8 and NUL bytes are dropped.
- A text longer than `limits.text` (`64KB` by default) is cut at that size,
  never inside a character, and ends with a line `… (truncated)`. Of a file
  or of standard input no more than that is read, so piping a huge log is
  safe.

Each module then fits the message into what its channel accepts: Telegram
and ntfy take about 4096 characters or bytes and get a shortened text, mail
gets all of it. For a long log, send its tail: `tail -n 50 backup.log |
onenotify send --title "backup failed" --text-file -`.

## Levels and tags

`--level` changes how a message looks, never where it goes:

| Level | For | telegram | ntfy |
|---|---|---|---|
| `info` | something happened | ℹ️ | priority 2 |
| `ok` | something finished well | 🟢, silent with `silent_ok` | priority 3 |
| `warning` | something needs a look | 🟡 | priority 3 |
| `error` | something broke | 🔴 | priority 4 |

Tags are passed to the channels. ntfy shows them next to the message;
telegram and mail do not show them.

## What `send` does

1. It writes the message into the queue of each chosen channel. From that
   moment the message is not lost, whatever happens next.
2. It then does what [`onenotify run`](/queue-and-delivery) does, for the
   chosen channels: takes in new events and delivers the queues of these
   channels, all of them and in order. A message waiting in a chosen channel
   from before goes out first, and the output lists it too.

Without `--channel` the message goes to every enabled channel. Naming a
channel that does not exist or is disabled is an error, and nothing is
queued.

With `--queue-only` only step 1 happens:

```console
$ onenotify send --title "nightly job started" --queue-only
queued 20261124T081846Z-send-05e27d50 for ops-telegram, admin-mail, phone
```

Use it when the script must not wait for a slow channel, or when it runs
without the environment variables the channels need. The script must be able
to write to the spool, so it runs as the same user as onenotify. The next
scheduled run delivers.

Delivery needs the secrets of the channels. If the config uses `${VAR}` and
the variable is not set where `send` is called, that channel's message stays
queued and the scheduled run delivers it; see
[Getting started](/getting-started#secrets) for loading the variables.

## While another run is in progress

Only one run works on the spool at a time. If one is in progress, `send`
waits for it for up to 10 seconds. After that it gives up waiting, not the
message:

```console
$ onenotify send --title "while locked" --channel phone
queued 20261124T081155Z-send-5e3bcdf5 for phone
onenotify: another run is still in progress; the message stays queued and goes out with the next run
$ echo $?
1
```

The first line is on stdout, the second on stderr.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | delivered to every chosen channel and nothing else is waiting in them; with `--queue-only`: queued |
| `1` | queued, but at least one message is still waiting in a chosen channel or was moved to the failed store; or another run was in progress |
| `2` | onenotify could not do its job. Bad arguments, an unknown or disabled channel, no enabled channel, a config that cannot be loaded, a text file that cannot be read: **nothing was queued**. A failure after the message was queued says so: the error starts with `the message is queued, but` |

With `1` the message is safe in the queue: a script does not have to retry.
The same holds for a `2` whose text starts with `the message is queued`: the
next run delivers, and sending again would produce a duplicate.

```console
$ onenotify send --title "slow one" --channel slow
...
onenotify: the message is queued, but the run failed: run interrupted
$ echo $?
2
```

Otherwise the reason is on stderr and nothing was queued:

```console
$ onenotify send --title x --channel nope
onenotify: there is no channel "nope" in /etc/onenotify/config.yaml
$ onenotify send --title x --channel team-ntfy
onenotify: channel "team-ntfy" is disabled (enabled: false)
$ onenotify send --title x --level fatal
onenotify: unknown level "fatal" (expected info, ok, warning or error)
$ onenotify send --title x --text-file /nonexistent
onenotify: cannot read the text: open /nonexistent: no such file or directory
```

The output of `send` is the output of a run, text or JSON, see
[Output and exit codes](/output-and-exit-codes).

## In scripts

A log file as the text, with a level:

```sh
onenotify send --title "backup failed" --text-file /var/log/backup.log --level error
```

```
🔴 [web-01] backup failed

tar: /srv/data/cache: file changed as we read it
tar: /srv/data/db.sock: socket ignored
rsync: connection unexpectedly closed (0 bytes received so far) [sender]
rsync error: unexplained error (code 255) at io.c(232) [sender=3.2.7]
```

What a command printed on stderr:

```sh
pg_dump mydb 2>&1 >/dev/null | onenotify send --title "pg_dump said" --text-file - --level warning
```

A message is sent even when the command printed nothing: it then has the
title only. Test for that in the script if you only want to hear about
problems.

One channel only, with a tag:

```sh
onenotify send --title "deploy done" --level ok --tag deploy --channel ops-telegram
```

A backup script that reports either way:

```sh
#!/bin/sh
# nightly backup: tell the channels how it went
log=$(mktemp)
start=$(date +%s)
if run_backup >"$log" 2>&1; then
    onenotify send --title "backup finished" --level ok \
        --text "took $(( $(date +%s) - start ))s" >/dev/null
else
    onenotify send --title "backup failed" --level error --tag backup \
        --text-file "$log" >/dev/null
fi
rm -f "$log"
```

`>/dev/null` hides the report; warnings about a channel that cannot deliver
still reach stderr. Do not let the script fail on exit code `1`: it means the
message is queued and will be retried.

Messages keep the order they were sent in, also within one second: the part
of the id after `-send-` starts with the microsecond.

Try out a channel after changing its params:

```sh
onenotify send --title test --channel admin-mail
```
