# Development

Rules for contributors (commit messages, merges, limits) are in
[AGENTS.md](https://github.com/alekzonder/onenotify/blob/main/AGENTS.md). This page is about how to build, test and release.

## Requirements

- Go (the version in `go.mod` or newer)
- `make`
- network access the first time `make lint` runs: it installs a pinned
  `golangci-lint` into `./bin`
- for the documentation site only: Node.js 22 and npm

## Make targets

| Target | What it does |
|---|---|
| `make build` | builds `dist/onenotify` and `dist/modules/onenotify-{telegram,mail,ntfy}` |
| `make test` | `go test -race ./...` |
| `make fmt` | formats the code with `gofmt` |
| `make lint` | `golangci-lint`, including cyclomatic complexity |
| `make check` | `fmt-check` + `go vet` + `lint` + `test`; **must pass before a change is done** |
| `make release-assets` | cross-builds release assets and `checksums.txt` into `dist/release/` |
| `make docs-build` | checks documentation links and builds the site into `docs/dist/` |
| `make docs` | builds the site and publishes it to branch `docs` |
| `make clean` | removes `dist/` |

Builds are static (`CGO_ENABLED=0`). The version is injected with
`make build VERSION=1.2.3`; without it the version is `dev`.

A new bundled module is picked up by the Makefile automatically: every
directory `cmd/onenotify-<name>` is built as a module.

## Cyclomatic complexity

`gocyclo` runs as part of `make lint` with a limit of **25** per function
(`.golangci.yml`). It applies to tests too. When a function goes over the
limit, split it; do not raise the limit and do not silence the linter.

## Layout

```
cmd/onenotify/             core CLI entrypoint
cmd/onenotify-telegram/    bundled modules: main.go (description, params schema),
cmd/onenotify-mail/          send.go (delivery)
cmd/onenotify-ntfy/
pkg/module/                public SDK for module authors: protocol types, Main
internal/config/           find, load, include, ${VAR} in channel params, defaults
internal/message/          the message; building it from a onecheck event and from "send"
internal/queue/            per-channel spool, failed store, run lock, channel status
internal/deliver/          one run: intake from the sources, delivery per channel
internal/modules/          find modules, describe, run a module process
internal/schema/           channel params against a module's JSON Schema
internal/output/           text and JSON rendering of a run's report, exit code
internal/logging/          slog setup
internal/rotate/           logrotate: logs and the failed store
internal/fsutil/           atomic and durable file writes
internal/units/            sizes and durations of the config
internal/github/           GitHub Releases client
internal/install/          module install/update/remove, self-update
internal/cli/              cobra commands
internal/testmods/         builds modules for tests, including a fake one that misbehaves
internal/installsh/        tests of install.sh
examples/                  example config, validated by a test
install.sh                 installer script (POSIX sh)
docs/                      user documentation, built into the site by Blume
scripts/                   documentation link check and publishing
```

How a run flows through the packages:

```
cli.run
 └─ config.Find / config.Load
 └─ queue.Open, Lock
 └─ deliver.Run
     ├─ intake: per event    message.FromOnecheck
     │                       queue.Enqueue for every enabled channel
     │                       remove the event from its source
     └─ per channel, in parallel; per message, in order:
          modules.Load, schema.Validate     is the channel usable
          modules.Send                      starts onenotify-<module> send
          queue.Remove | queue.Fail | SetStatus
 └─ output.Write, output.ExitCode
```

`cli.send` builds the message with `message.NewSend`, calls `queue.Enqueue`
for the chosen channels and then does the same run for them.

## Design rules

- **A module only delivers.** What is sent, to which channel and with which
  text is decided by the core. Never add routing, retries or text building to
  a module beyond fitting the message into its channel (escaping, length).
- **A notification is never lost.** An event is removed from its source only
  after its message was written, durably, into the queue of every enabled
  channel; a queue file is removed only after its module reported delivery,
  or after it was copied to the failed store. Delivery starts only after
  intake finished without an error. Keep these orders.
- **Durable files.** Queue and failed files are written through
  `fsutil.WriteFileDurable` (temp file, fsync, rename, fsync of the
  directory).
- **Channel params are secrets.** They are never logged, printed, or written
  to queue or failed files; error texts name a param, never its value.
- **One channel never holds up another**, and within a channel messages keep
  their order: a temporary error stops that channel for the run.
- **The protocol is a contract.** Changing the JSON exchanged with modules
  incompatibly requires a new `module.ProtocolVersion` and a range in
  `internal/modules` that still accepts the old one.

## Tests

- Unit tests sit next to the code. Building a message from an event
  (`internal/message`) is table-driven, with the expected title and text
  written out.
- The queue and a run are tested on real files in `t.TempDir()`
  (`internal/queue`, `internal/deliver`), with a fake module from
  `internal/testmods/fakemod` that can misbehave on purpose: fail, hang,
  print garbage.
- Bundled modules are tested against a local server in the test: HTTP for
  `telegram` and `ntfy`, a small SMTP server for `mail`. The tests cover the
  request, truncation, escaping, the classification of errors and that no
  secret reaches an error text.
- CLI tests (`internal/cli`) run the commands end to end with the real
  modules, built once per package through `internal/testmods`.
- Module management is tested against an in-memory GitHub client
  (`internal/install`) and against a local HTTP server speaking the GitHub
  API shape (`internal/github`, `internal/cli`).
- `install.sh` is run by `internal/installsh` against a local server.
- `examples/config.yaml` (with its `conf.d/` includes) is loaded and
  validated against all bundled modules by `TestExamplesLoadAndValidate`, so
  examples cannot drift from the code. The test also fails when a bundled
  module (`cmd/onenotify-<name>`) has no example channel: a new module comes
  with an example.
- Tests must not use the network (loopback is fine) and must not write
  outside `t.TempDir()`.

Run one package or one test:

```sh
go test ./internal/message
go test ./internal/cli -run TestExamplesLoadAndValidate -v
```

## Adding a bundled module

1. Create `cmd/onenotify-<name>/` with `main.go` (description, params
   schema) and `send.go`, following the existing modules and the
   [rules of behaviour](/module-protocol#rules-of-behaviour).
2. Test `send` against a local server: the request, a text that is too long,
   characters the provider treats as markup, every class of error, and that
   no secret appears in an error text.
3. Add a channel using the module to `examples/config.yaml`.
4. Add `docs/modules/<name>.md` (with `title` and `sidebar.label`
   frontmatter like the other module pages), list it in
   `docs/modules/meta.ts` and link it from `README.md` and `docs/index.md`.
5. Add the module to `MODULES` in `install.sh`.

## Releasing

Releases are built by GitHub Actions (`.github/workflows/release.yml`) when a
tag `vX.Y.Z` is pushed:

```sh
git tag v1.2.0
git push origin v1.2.0
```

The workflow runs `make check`, then `make release-assets VERSION=1.2.0` and
publishes a GitHub release with:

```
onenotify_<os>_<arch>
onenotify-telegram_<os>_<arch>
onenotify-mail_<os>_<arch>
onenotify-ntfy_<os>_<arch>
checksums.txt
```

for linux and darwin on amd64 and arm64. These names are what `install.sh`,
`onenotify module install` and `onenotify self-update` look for.

After the release is published, the workflow's `docs` job runs `make docs`,
which publishes the documentation of the tagged commit (see
[Documentation site](#documentation-site)).

Environment overrides useful when testing update flows against a fork:

| Variable | Effect |
|---|---|
| `ONENOTIFY_GITHUB_API` | API base URL instead of `https://api.github.com` |
| `ONENOTIFY_SELF_UPDATE_REPO` | repository `self-update` downloads from |
| `ONENOTIFY_SELF_UPDATE_TARGET` | path of the binary `self-update` replaces |
| `ONENOTIFY_DOWNLOAD_URL` | base URL `install.sh` downloads release assets from |

## Documentation site

The pages under `docs/` are published at <https://alekzonder.github.io/onenotify>
by [Blume](https://useblume.dev). `docs/` is the Blume project and its own
content root:

| Path | What it is |
|---|---|
| `docs/blume.config.ts` | site settings: base `/onenotify`, excluded folders |
| `docs/package.json`, `docs/package-lock.json` | the pinned Blume version |
| `docs/index.md` | the home page |
| `docs/meta.ts`, `docs/modules/meta.ts` | sidebar order |
| `docs/public/` | static files served at the site root |

Every `.md` page under `docs/` is published, except `docs/superpowers/`
(internal designs and plans) and build folders. To keep a new folder off the
site, add it to `content.exclude` in `docs/blume.config.ts`.

**Links between pages use site routes, never `.md` paths:**
`[Messages](/messages#limits)`, `[ntfy](/modules/ntfy)`. Blume prefixes the
base `/onenotify` itself; it does not rewrite relative `.md` links, so they
would break on the site. `scripts/docs-check-links.sh` fails the build when
one appears. Links from outside the site (README, CLI messages) use the full
URL `https://alekzonder.github.io/onenotify/<route>`.

Console examples in the documentation are real output of the tool. When
behaviour or wording changes, run the command again and replace the example.

Preview locally with `cd docs && npm ci && npm run dev`.
`make docs-build` runs the link check, `blume doctor` and `blume build`; CI
runs it on every push and pull request (`.github/workflows/check.yml`).

`make docs` (`scripts/publish-docs.sh`) builds the site and commits
`docs/dist` to branch `docs`, creating it on first use. GitHub Pages serves
branch `docs`, path `/`. The release workflow runs it for every tag, so the
site always shows the latest release.
