# AGENTS.md

Rules for anyone (human or AI agent) changing this repository.

## Commits and merges

- Never add `Co-Authored-By`, `Co-Author`, `Signed-off-by: <AI>` or any similar
  attribution trailer to commit messages.
- Never create merge commits. Integrate branches with rebase only
  ("Rebase and merge" on GitHub, `git rebase` locally). History stays linear.
- Commit message style: `chg: <what changed>` / `fix: <what was fixed>` /
  `docs: <what was documented>`, lower case, imperative.

## Before you finish

- Run `make check`. It must pass: gofmt, `go vet`, `golangci-lint`, tests with
  the race detector.
- Cyclomatic complexity is limited to 25 (`gocyclo` in `.golangci.yml`).
  Do not raise the limit and do not add `//nolint:gocyclo`; split the function.
- New behaviour comes with a test. Tests must not use the network (loopback is
  fine) or write outside `t.TempDir()`.
- User-visible changes (config keys, module params, CLI flags and output, the
  text of notifications, queue and failed file formats, the module protocol)
  must be reflected in `docs/`.
- `docs/` is published as a site (Blume). Pages link to each other by site
  route, `[Messages](/messages#anchor)`, never by `.md` path; `make docs-build`
  must pass when `docs/` changes. See "Documentation site" in
  `docs/development.md`.

## Repository map

| Path | What lives there |
|---|---|
| `cmd/onenotify` | core CLI entrypoint |
| `cmd/onenotify-<name>` | bundled delivery modules (`telegram`, `mail`, `ntfy`) |
| `pkg/module` | public SDK and protocol types for module authors |
| `internal/config` | config discovery, loading, includes, `${VAR}` in channel params |
| `internal/message` | the message; building it from a onecheck event and from `send` |
| `internal/queue` | per-channel spool, failed store, run lock, channel status |
| `internal/deliver` | one run: intake from the sources, delivery per channel |
| `internal/modules` | module discovery and process execution |
| `internal/schema` | channel params against a module's JSON Schema |
| `internal/output` | rendering a run's report, exit code |
| `internal/rotate` | `onenotify logrotate`: logs and the failed store |
| `internal/github`, `internal/install` | module install/update, self-update |
| `internal/cli` | cobra commands |
| `internal/testmods` | module executables for tests, including a fake one that misbehaves |
| `install.sh` | installer script (POSIX sh); tested by `internal/installsh` |
| `docs/` | user documentation (Markdown), the Blume site published to https://alekzonder.github.io/onenotify |
| `scripts/` | documentation link check and publishing |

## Design rules that must hold

- A module only delivers. What is sent, to which channel and with which text
  is decided by the core; never add routing, retries or text building to a
  module beyond fitting the message into its channel (escaping, length).
- The module protocol is versioned (`pkg/module.ProtocolVersion`). A breaking
  change to the JSON exchanged with modules requires a new protocol version.
- A notification is never lost. An event is removed from its source only
  after its message was written, durably, into the queue of every enabled
  channel; a queue file is removed only after its module reported delivery,
  or after it was copied to the failed store. Delivery starts only after
  intake finished without an error.
- Queue and failed files are written through `fsutil.WriteFileDurable`
  (temp file, fsync, rename, fsync of the directory).
- Channel params are secrets. They are never logged, printed, or written to
  queue or failed files; error texts name a param, never its value.
- One channel never holds up another, and within a channel messages keep
  their order: a temporary error stops that channel for the run.

See `docs/development.md` for build, test and release details.
