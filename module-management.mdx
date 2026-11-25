---
title: Module management
---

A module is an executable file named `onenotify-<name>` in the modules
directory (`paths.modules`). onenotify reads that directory on every run:
there is no registry to refresh and nothing to restart. A module is usable
from the moment its file is there, and gone when the file is removed.

Modules are distributed through GitHub Releases. The `onenotify module`
commands download, verify and install them; you can also copy a module file
into the directory by hand.

All commands take the modules directory from the config. Without a config,
pass `--modules-dir <dir>`.

## List

```console
$ onenotify module list
modules in /usr/lib/onenotify/modules:
NAME      VERSION  PROTOCOL  COMPATIBLE  SOURCE  DESCRIPTION
mail      0.1.0    1         yes         -       Send a message as a plain text e-mail through an SMTP server
ntfy      0.1.0    1         yes         -       Publish a message to a topic of an ntfy server
telegram  0.1.0    1         yes         -       Send a message to a Telegram chat through the Bot API
```

`SOURCE` is the repository a module was installed from; `-` means the file
was put there by hand, as above. `COMPATIBLE` says whether this onenotify can
talk to the module, see [Compatibility](#compatibility).

## Search

```sh
onenotify module search                         # repositories from modules.repos in the config
onenotify module search alekzonder/onenotify someone/onenotify-slack
```

Lists every release of the repositories and the modules each release has for
this machine's OS and architecture, with the command to install each.

## Install

```sh
onenotify module install alekzonder/onenotify                # every module of the latest release
onenotify module install alekzonder/onenotify telegram       # one module, latest release
onenotify module install alekzonder/onenotify ntfy@1.2.0     # a specific version
```

"Latest" is the newest release that is not marked as a pre-release. A
pre-release is only installed when asked for by version.

What install does, per module:

1. downloads the asset into a temporary file inside the modules directory;
2. compares its sha256 with the release's `checksums.txt` — a release without
   `checksums.txt`, or without an entry for the asset, is refused;
3. runs the downloaded file with `describe` and checks that it is the module
   it claims to be and speaks a supported protocol version;
4. renames it into place in one step, replacing a previous version;
5. records repository, version and checksum in `installed.json` in the
   modules directory.

If any step fails, nothing changes: a previously installed version keeps
working, and no partial file is left.

## Update

```sh
onenotify module update                 # all modules installed with "module install"
onenotify module update telegram ntfy   # only these
```

Each module is updated to the latest stable release of the repository it was
installed from. For every module onenotify prints either `updated <name> to
<version>` or `skipped <name>: <reason>`. Reasons include: already up to
date; the new release speaks a protocol this onenotify does not support
(update onenotify first); the module was copied in by hand, so its source is
unknown.

A skip is not a failure. An update that was attempted and failed (download
error, checksum mismatch, a module that does not run) stops the command with
exit code `2`; the installed version stays in place.

When no module was installed with `module install`:

```console
$ onenotify module update
no modules were installed with "onenotify module install"; nothing to update
```

## Remove

```sh
onenotify module remove ntfy
```

Channels that still use the module cannot deliver ("module is not
installed"); their messages wait in the queue until the module is back or
their deadlines pass.

## Updating onenotify itself

```sh
onenotify self-update                  # latest stable release
onenotify self-update --version 1.4.0  # a specific one
```

The asset `onenotify_<os>_<arch>` is downloaded next to the running binary,
verified against `checksums.txt`, started once (`version`) to prove it runs,
and then renamed over the current binary in one step. On any failure the
current binary is left untouched. The user running the command needs write
access to the directory of the binary.

A onenotify installed with `install.sh` lives in a per-version directory and
is not updated by `self-update` (the command refuses): run `install.sh`
again, see [Getting started](/getting-started#with-the-install-script).

`self-update` does not touch modules, and `module update` does not touch
onenotify. A typical sequence:

```sh
onenotify self-update && onenotify module update
```

## Compatibility

onenotify and modules are versioned independently. What must match is the
**module protocol**: the JSON a module and onenotify exchange.

- Every module reports the protocol version it speaks.
- Every onenotify supports a range of protocol versions; `onenotify version`
  prints it:

  ```console
  $ onenotify version
  onenotify 0.1.0 (protocol 1)
  ```

- A module outside that range is shown as not compatible by `module list`
  and `validate`; channels using it cannot deliver and their messages wait;
  `install` and `update` refuse to install such a version.

Module versions themselves (semver, the release tag without the leading `v`)
are only used to decide whether an update is available.

Bundled modules (`telegram`, `mail`, `ntfy`) are released together with
onenotify from the same repository and carry the same version number, but any
mix that shares a protocol version works.

## Repositories of third-party modules

Any GitHub repository can distribute modules. A release needs:

- one asset per module and platform, named
  `onenotify-<name>_<os>_<arch>`, e.g. `onenotify-slack_linux_amd64`
  (`<os>` and `<arch>` as in Go: `linux`, `darwin`; `amd64`, `arm64`);
- an asset `checksums.txt` in `sha256sum` format covering them:

  ```
  3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  onenotify-slack_linux_amd64
  ```

Module names are lower-case letters, digits, `_` and `-`. See
[Module protocol](/module-protocol) for how to write a module.

Add repositories you use to the config so that `module search` covers them:

```yaml
modules:
  repos:
    - alekzonder/onenotify
    - someone/onenotify-slack
```

## Environment

| Variable | Effect |
|---|---|
| `GITHUB_TOKEN` | sent to the GitHub API: higher rate limits, access to private repositories |
| `ONENOTIFY_GITHUB_API` | API base URL instead of `https://api.github.com` (GitHub Enterprise) |

## Security notes

- Checksums come from the same release as the files. They protect against
  corrupted or truncated downloads, not against a compromised repository.
  Only install modules from repositories you trust: a module runs with the
  privileges of onenotify and receives the secrets of its channels.
- Modules are looked up only in the modules directory, never in `PATH`. Keep
  that directory writable by root (or the onenotify user) only.
