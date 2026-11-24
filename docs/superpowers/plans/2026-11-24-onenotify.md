# onenotify Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A notification tool that takes onecheck events and ad-hoc messages and delivers them through provider modules (`telegram`, `mail`, `ntfy`), built, tested, released and documented the same way as onecheck.

**Architecture:** A core binary `onenotify` turns each event into a message, fans it out into one spool directory per channel, and delivers each channel's queue in order by starting `onenotify-<module> send` with JSON on stdin/stdout. Infrastructure (module discovery and execution, GitHub install, self-update, logging, log rotation, config loading, installer, Makefile, CI, docs site) is copied from `../onecheck` and renamed.

**Tech Stack:** Go (version of `../onecheck/go.mod`), `cobra`, `yaml.v3`, `santhosh-tekuri/jsonschema/v6`, `log/slog`; Blume for the docs site.

**Spec:** `docs/superpowers/specs/2026-11-24-onenotify-design.md`

## Global Constraints

- Go module path: `github.com/alekzonder/onenotify`. Binary `onenotify`, modules `onenotify-<name>`.
- "As in onecheck" means: read the named file in `../onecheck` and keep its behaviour; rename `onecheck` → `onenotify`, `ONECHECK_` → `ONENOTIFY_`.
- Commit style `chg:` / `fix:` / `docs:`, lower case, imperative, no attribution trailers; linear history.
- `make check` passes at the end of every task: gofmt, `go vet`, `golangci-lint` (gocyclo ≤ 25, never raised or silenced), `go test -race`.
- Tests: no network except loopback, no writes outside `t.TempDir()`.
- Directories `0750`, files `0640`. Everything under spool and failed is written through a temp file whose name starts with `.` plus rename; queue writes also fsync the file and its directory.
- Channel params are never logged, printed, or written to spool or failed files.
- Protocol version `1`. Message levels: `error`, `ok`, `warning`, `info`.
- Exit codes: `0` nothing left undelivered, `1` something is waiting or went to failed, `2` onenotify could not do its job.
- All user-visible text and documentation in English.

## Review Focus

- An event file that is valid JSON but not an object, or has `result: null`: goes to `failed/_unreadable`, the run continues.
- A title or text with control characters, `<`, `&`, emoji, or invalid UTF-8: every module delivers it without a provider-side parse error and without cutting inside a character.
- A channel name or message id containing `/` or starting with `.`: never escapes the spool directory.
- A module that prints valid JSON and then hangs, or exits non-zero after printing `delivered: true`: treated as a temporary error, the message stays.
- `onenotify send` with an empty stdin, a text file larger than `limits.text`, or `--text-file` pointing at a directory: a clear error or a truncated message, never a hang or a crash.

---

## File Structure

```
cmd/onenotify/main.go                 entrypoint
cmd/onenotify-telegram|ntfy|mail/     main.go (description, schema), send.go, send_test.go
pkg/module/                           protocol.go, run.go, run_test.go
internal/version/                     build version
internal/units/                       copied
internal/logging/                     copied; log file <dir>/onenotify.log
internal/rotate/                      logs.go copied; failed.go new
internal/github/, internal/install/   copied
internal/config/                      types.go, config.go (defaults, paths), load.go, config_test.go
internal/message/                     message.go (type, NewSend), onecheck.go (event → message), tests
internal/queue/                       atomic.go, queue.go (spool, failed, lock, owner), status.go, tests
internal/modules/                     modules.go, exec.go, testdata/fakemod
internal/deliver/                     intake.go, deliver.go, report.go, tests
internal/output/                      text and JSON rendering, exit code
internal/cli/                         root, run, send, validate, status, paths, module, selfupdate, logrotate, version
internal/testmods/                    builds bundled modules for tests
internal/installsh/                   installer test
examples/config.yaml
install.sh, Makefile, .golangci.yml, .gitignore, AGENTS.md, README.md
.github/workflows/check.yml, release.yml
scripts/docs-check-links.sh, docs-check-links-test.sh, publish-docs.sh
docs/                                 Blume site
```

---

### Task 1: Scaffold and copied infrastructure

**Files:** `go.mod`, `Makefile`, `.golangci.yml`, `.gitignore`, `AGENTS.md`, `cmd/onenotify/main.go`, `internal/version`, `internal/units`, `internal/logging`, `internal/rotate/logs.go`, `internal/github`, `internal/install`, with their tests.

**Interfaces:**
- Produces: the packages above with the same exported API as in onecheck, except `logging` opens `<log.dir>/onenotify.log` (no name subdirectory) and `install` uses the asset prefix `onenotify` and env `ONENOTIFY_*`.

- [ ] **Step 1:** Copy the files, rename, drop what refers to rules/results. `Makefile` targets identical to onecheck's (docs targets included; they work once Task 12 lands).
- [ ] **Step 2:** `AGENTS.md` per spec §12 (rules, repository map, design rules).
- [ ] **Step 3:** `go build ./... && go test -race ./...` pass; `make lint` passes.
- [ ] **Step 4:** `git commit -m "chg: add the project skeleton and the infrastructure shared with onecheck"`

### Task 2: Module SDK and protocol

**Files:** `pkg/module/protocol.go`, `pkg/module/run.go`, `pkg/module/run_test.go`

**Interfaces — Produces:**

```go
const ProtocolVersion = 1
const (LevelError = "error"; LevelOK = "ok"; LevelWarning = "warning"; LevelInfo = "info")

type Description struct {
    Protocol     int             `json:"protocol"`
    Name         string          `json:"name"`
    Version      string          `json:"version"`
    Description  string          `json:"description"`
    ParamsSchema json.RawMessage `json:"params_schema,omitempty"`
}
type Message struct {
    ID        string    `json:"id"`
    Source    string    `json:"source"`
    Host      string    `json:"host"`
    Level     string    `json:"level"`
    Title     string    `json:"title"`
    Text      string    `json:"text"`
    Tags      []string  `json:"tags"`
    CreatedAt time.Time `json:"created_at"`
}
type Request struct {
    Protocol  int             `json:"protocol"`
    Channel   string          `json:"channel"`
    Params    json.RawMessage `json:"params"`
    Message   Message         `json:"message"`
    TimeoutMS int64           `json:"timeout_ms"`
}
type Error struct {
    Message      string `json:"message"`
    Permanent    bool   `json:"permanent,omitempty"`
    RetryAfterMS int64  `json:"retry_after_ms,omitempty"`
}
type Response struct {
    Delivered bool   `json:"delivered"`
    Error     *Error `json:"error"`
}
type Module struct {
    Description Description
    Send        func(ctx context.Context, req Request) error
}
func Main(m Module)                                    // os.Exit(Run(...))
func Run(m Module, args []string, stdin io.Reader, stdout, stderr io.Writer) int
func DecodeParams(raw json.RawMessage, v any) error    // unknown keys are errors
func Permanent(err error) error
func RetryAfter(err error, d time.Duration) error
func Truncate(s string, max int) string                // max bytes, cut on a rune boundary, ends with "…"
```

- [ ] **Step 1: Failing tests** `run_test.go`: `describe` prints the description with `protocol` filled in; `send` with `Send` returning nil → `{"delivered":true,"error":null}`, exit 0; returning a plain error → `delivered:false`, `permanent` absent; `Permanent(err)` → `permanent:true`; `RetryAfter(err, 30*time.Second)` → `retry_after_ms:30000`; wrapped with `fmt.Errorf("%w")` still classified; invalid JSON on stdin → exit 2 and a message on stderr; unknown subcommand → exit 2; `timeout_ms` reaches `Send` as a context deadline; `Truncate("héllo", 3)` is valid UTF-8 of at most 3 bytes ending in `…` only when it fits (else cut without it); `Truncate` of a short string is unchanged.
- [ ] **Step 2:** tests fail. **Step 3:** implement after `../onecheck/pkg/module/run.go`. **Step 4:** tests pass.
- [ ] **Step 5:** `git commit -m "chg: add the module protocol and the Go SDK for modules"`

### Task 3: Config

**Files:** `internal/config/{types.go,config.go,load.go,config_test.go}`

**Interfaces — Produces:**

```go
type Source struct{ Type, Path string }
type Channel struct {
    Name, Module string
    Enabled      bool
    Timeout      time.Duration
    Params       map[string]any   // ${VAR} expanded
    Source       string           // file it was read from
    Err          error            // makes only this channel unusable
}
type Config struct {
    Host string; Path string; Includes []string
    Log Log; Paths Paths{Modules, Spool, Failed string}
    Sources []Source
    Queue   struct{ TTL, FailedTTL Duration }
    Limits  struct{ Text Size }
    Run     struct{ Parallel int }
    Defaults struct{ Timeout Duration }
    Modules Modules
    Channels []Channel
}
func Find(flag string) (path, source string, tried []string, err error)  // as in onecheck
func Load(path string) (*Config, error)
func (c *Config) LogFile() string
func (c *Config) Channel(name string) *Channel
func (c *Config) EnabledChannels() []*Channel   // enabled and Err == nil
```

Defaults per spec §6; root vs non-root paths as in onecheck with `onenotify`; default source `{onecheck, <onecheck default events dir for this user>}`; `sources: []` disables intake (distinguish "absent" from "empty"). Only source type `onecheck` is accepted.

- [ ] **Step 1: Failing tests:** defaults for a minimal empty config; unknown top-level key is an error; unknown channel key is a load error naming the file and line (a typo in a channel must stop the run); duplicate channel name across include files is an error; include file with a key other than `channels` is an error; `${VAR}` expanded only in channel params, undefined variable sets `Channel.Err`; `$${VAR}` literal; relative paths resolved against the config directory; `host` empty string is an error, absent uses `os.Hostname`; `sources: []` → no sources; unknown source type is an error; invalid channel name (`a/b`, `.x`, `_unreadable`) is an error; `timeout`, `queue.ttl`, `limits.text`, `run.parallel` must be positive.
- [ ] **Step 2–4:** fail, implement after `../onecheck/internal/config`, pass.
- [ ] **Step 5:** `git commit -m "chg: load the config with channels, sources and includes"`

### Task 4: Message and the onecheck event

**Files:** `internal/message/{message.go,onecheck.go,message_test.go,onecheck_test.go}`

**Interfaces — Produces:**

```go
type Message struct {
    Version   int       `json:"version"`   // 1
    ID, Source, Host, Level, Title, Text string
    Tags      []string  `json:"tags"`
    CreatedAt time.Time `json:"created_at"`
    ExpiresAt time.Time `json:"expires_at"`
}
func (m Message) Wire() module.Message
func NewSend(host, title, text, level string, tags []string, now time.Time, ttl time.Duration) Message
func FromOnecheck(data []byte, host string, ttl time.Duration) (Message, error)
func ValidLevel(s string) bool
```

Rendering rules: spec §7 exactly (line order, 200-char param values, 20 param lines, 10 failures, 300-char error lines with newlines replaced by spaces, `redacted`, unknown transition, UTC time format `2006-01-02 15:04:05 UTC`). Truncation counts runes. `NewSend` id: `<UTC 20060102T150405Z>-send-<8 hex from crypto/rand>`.

- [ ] **Step 1: Failing table tests** with full expected `Title` and `Text` for: fail with two failures and nested params (the spec's example verbatim); fail with `consecutive: 1` (`failed:`); fail with `status: error`; recovered (no failure lines); event without `params` key; `params: null`; `redacted: true` on a fail (no params, no error lines); 25 params (`… and 5 more params`); 12 failures (`… and 2 more`); a 500-rune multi-line failure message (one line, 300 runes + `…`); a 300-rune param value; array and nested-array params as compact JSON; unknown transition `flapping` → level `info`, title `[h] FLAPPING main/x`; missing `expires_at` → `created_at + ttl`; not JSON, JSON array, missing `id`/`rule`/`transition` → error.
- [ ] **Step 2–4:** fail, implement, pass.
- [ ] **Step 5:** `git commit -m "chg: build a message from a onecheck event and from send arguments"`

### Task 5: Queue

**Files:** `internal/queue/{atomic.go,queue.go,status.go,queue_test.go}`

**Interfaces — Produces:**

```go
var ErrLocked = errors.New("another onenotify run is still in progress")
type Queue struct{ ... }
func Open(spool, failed, configPath string) (*Queue, error)       // creates the directories
func (q *Queue) Lock() (unlock func(), err error)                 // flock, non-blocking, then .owner check
func (q *Queue) LockWait(d time.Duration) (unlock func(), err error) // retries every 100ms up to d
func (q *Queue) Enqueue(channel string, m message.Message) error  // durable write of spool/<channel>/<id>.json
func (q *Queue) Channels() ([]string, error)                      // subdirectories of spool, sorted
func (q *Queue) List(channel string) ([]string, error)            // *.json file names, sorted, no dot-names
func (q *Queue) Load(channel, file string) (message.Message, error)
func (q *Queue) Remove(channel, file string) error
func (q *Queue) Fail(channel, file, reason string, now time.Time) error      // → failed/<channel>/, adds failed_at, reason
func (q *Queue) FailRaw(dir, file string, data []byte) error                 // failed/<dir>/<file> as is
func (q *Queue) Status(channel string) (*Status, error)           // nil when none or unreadable
func (q *Queue) SetStatus(channel string, s Status) error
func (q *Queue) ClearStatus(channel string) error
func (q *Queue) FailedCount() (int, error)
func FileName(id string) string                                   // as store.fileName in onecheck
func WriteFileDurable(path string, data []byte, perm os.FileMode) error
type Status struct {
    FailingSince time.Time  `json:"failing_since"`
    Consecutive  int        `json:"consecutive"`
    LastError    string     `json:"last_error"`
    RetryAt      *time.Time `json:"retry_at,omitempty"`
}
```

- [ ] **Step 1: Failing tests:** enqueue then list/load round-trip; two ids sort chronologically; enqueue of the same id twice leaves one file; dot-files and `.status.json` are not listed; `Fail` moves the file and the failed copy has `failed_at` and `reason`; `Fail` of an unreadable spool file moves it as is; `FailRaw` writes `failed/_unreadable/<file>`; second `Lock` returns `ErrLocked`; `LockWait` succeeds when the first lock is released within the wait; a different config path on the same spool is refused, as onecheck's `.owner`; status set/get/clear, corrupt status reads as nil; `FileName("../x")` and `FileName(".x")` stay inside the directory; file modes `0640`, directory modes `0750`.
- [ ] **Step 2–4:** fail, implement (lock and owner after `../onecheck/internal/store/store.go`), pass.
- [ ] **Step 5:** `git commit -m "chg: add the per-channel spool, the failed store, the run lock and channel status"`

### Task 6: Module discovery and execution

**Files:** `internal/modules/{modules.go,exec.go,modules_test.go}`, `internal/modules/testdata/fakemod/main.go`

**Interfaces — Produces:**

```go
type Info struct { module.Description; Path string }
func Find(dir string) (map[string]*Info, []error)        // describe every onenotify-<name>
func Describe(ctx context.Context, path string) (*Info, error)
func (i *Info) Compatible() bool
func Send(ctx context.Context, info *Info, req module.Request, timeout time.Duration, stderr io.Writer) (*module.Response, error)
var ErrInterrupted
```

`Send` returns an error for: timeout, non-zero exit, invalid or empty JSON, output over 1 MB. The caller treats every such error as temporary.

- [ ] **Step 1:** `fakemod` controlled by `params.mode`: `ok`, `temporary`, `permanent`, `retry_after`, `exit3`, `garbage`, `silent`, `hang`, `ok_then_exit3`, `ok_then_hang`; it appends each received request to the file named by `params.record`.
- [ ] **Step 2: Failing tests** for each mode (expected response or error), plus: `Find` ignores files without the prefix and non-executables; a module whose `describe` name differs from its file name is reported; protocol `2` is not compatible; a hung module is killed with its process group within `timeout + 1s`.
- [ ] **Step 3–4:** implement after `../onecheck/internal/modules`, pass.
- [ ] **Step 5:** `git commit -m "chg: find modules and run them with a time limit"`

### Task 7: Intake and delivery

**Files:** `internal/deliver/{intake.go,deliver.go,report.go,deliver_test.go}`

**Interfaces — Produces:**

```go
type Outcome string // "sent", "waiting", "failed"
type Record struct {
    Channel, Module, ID string
    Outcome    Outcome
    DurationMS int64
    Error      string   // empty for sent
}
type Report struct {
    Ingested int
    Records  []Record
    Sent, Waiting, Failed int   // Waiting counts every message left in the processed queues
}
type Options struct {
    Channels []string          // empty = all
    Now      func() time.Time
    Log      *slog.Logger
    Stderr   io.Writer         // module stderr, for debug logging
}
func Run(ctx context.Context, cfg *config.Config, q *queue.Queue, mods map[string]*modules.Info, opt Options) (*Report, error)
```

`Run` assumes the lock is held. Order: intake for every source and every enabled channel (even when `Options.Channels` narrows delivery); on any intake write error return the error without delivering; then expiry sweep over every spool subdirectory; then delivery, channels in parallel up to `cfg.Run.Parallel`, each channel sequential. Per message: spec §8 "Доставка". Channel skipped while `Status.RetryAt` is in the future. Channel params are validated against the module's `params_schema` once per run; a schema failure is a temporary error for the channel.

- [ ] **Step 1: Failing tests** with `fakemod` built once per package and a temp onecheck events directory:
  - intake writes one spool file per enabled channel and removes the source; disabled channels get nothing;
  - no enabled channels: sources untouched, a warning logged;
  - a spool directory made unwritable (file in place of the directory) → `Run` returns an error, source kept, no module was started (record file empty);
  - re-running after that error is fixed delivers exactly once per channel;
  - unreadable event → `failed/_unreadable`, source removed, run continues;
  - order: three messages are recorded by the module in id order;
  - temporary error on message 1 of channel A: messages 2 and 3 of A are not attempted, channel B gets all three, status file written with `consecutive: 1`; second run increments it; a successful run clears it;
  - permanent error: message goes to failed with the module's text as reason, the next message is attempted;
  - expired message: failed with reason `expired`, module not started; also for a channel that is not in the config;
  - `retry_after` sets `RetryAt`; a run before it does not start the module, a run after it does;
  - `ok_then_exit3`, `garbage`, `hang` → waiting, message kept;
  - module missing or incompatible → waiting, message kept;
  - `Options.Channels: ["a"]` delivers only A but still fans out to B;
  - first temporary error logs at warn, the second run logs at info;
  - no log record contains the channel's `token` param value.
- [ ] **Step 2–4:** fail, implement, pass.
- [ ] **Step 5:** `git commit -m "chg: take events into the spool and deliver each channel in order"`

### Task 8: CLI

**Files:** `internal/output/{output.go,output_test.go}`, `internal/cli/*.go` with tests, `internal/testmods/testmods.go`

**Interfaces:**
- Consumes: everything above.
- Produces: `cli.Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int`; `output.Write(w, format string, r *deliver.Report) error`; `output.ExitCode(r *deliver.Report) int`.

Commands and flags: spec §5. `module`, `self-update`, `logrotate --logs`, `version`, `paths` are adapted from `../onecheck/internal/cli`. `logrotate --failed` implemented in `internal/rotate/failed.go`: `Failed(failedDir, spoolDir string, failedTTL, ttl time.Duration, now time.Time) (removed int, err error)`.

Text output lines: `SENT`, `WAIT`, `FAILED` padded as in the spec example; the error on the next line indented by 8 spaces; summary `N messages: X sent, Y waiting, Z failed`, or `0 messages`.

- [ ] **Step 1: Failing tests** (`cli_test.go`, real fakemod installed as a module in a temp modules dir):
  - `run` with one event and two channels: exit 0, two `SENT` lines, summary; with one channel failing: exit 1, a `WAIT` line with the error; `-o json` parses and has the same counts;
  - `run` while the lock is held: exit 2 and the lock message;
  - `send --title t --text x`: exit 0, the module recorded title `[<host>] t`, level `info`; `--level error --tag a --tag b --channel one` reaches only that channel with those values; unknown channel: exit 2; `--text` with `--text-file`: exit 2; `--text-file -` reads stdin; a file larger than `limits.text` is cut and ends with `\n… (truncated)`; `--text-file <dir>`: exit 2; `--level loud`: exit 2; `--queue-only`: exit 0, spool has the file, module not started; no enabled channels: exit 2;
  - `send` also takes waiting events in (an event file present before `send` is delivered by it);
  - `validate`: ok config exit 0; missing module, params not matching the schema, incompatible module: exit 2 naming the channel; missing source directory: warning, exit 0;
  - `status`: table with queue length, oldest, `ok` / `failing since …` / `disabled` / `not in config`; `--json` parses;
  - `paths` and `paths --json` list config, log, spool, failed, sources, modules, binary with `(missing)`;
  - `logrotate --failed` removes failed files older than `queue.failed_ttl` and leaves newer ones;
  - `version` prints the version and the protocol range.
- [ ] **Step 2–4:** fail, implement, pass.
- [ ] **Step 5:** `git commit -m "chg: add the command line: run, send, validate, status, paths, module management, logrotate"`

### Task 9: Module `telegram`

**Files:** `cmd/onenotify-telegram/{main.go,send.go,send_test.go}`

Params, format, limits and error classes: spec §10 "telegram". Request: `POST <api_url>/bot<token>/sendMessage`, JSON body `chat_id`, `text`, `parse_mode: "HTML"`, optional `message_thread_id`, `disable_notification` (true only when `silent_ok` and level `ok`), `link_preview_options: {is_disabled: true}`. Text is cut so the whole message is at most 4096 UTF-16 code units after escaping `&`, `<`, `>`; the cut never splits an entity or a character.

- [ ] **Step 1: Failing tests** against `httptest`: body fields for each level (icon prefix, bold escaped title, blank line, escaped text); `silent_ok`; thread id; a 10 000-character text is delivered within the limit and ends with `…`; a text of `&` characters is cut correctly; `429` with `parameters.retry_after: 7` → `retry_after_ms: 7000`, not permanent; `400` "message is too long" → permanent; `400` "chat not found", `401`, `403`, `500` → temporary; connection refused → temporary and the error text does not contain the token; `TestDescriptionSchema`: the schema compiles, rejects unknown keys and a missing `token`.
- [ ] **Step 2–4:** fail, implement, pass.
- [ ] **Step 5:** `git commit -m "chg: add the telegram module"`

### Task 10: Module `ntfy`

**Files:** `cmd/onenotify-ntfy/{main.go,send.go,send_test.go}`

Spec §10 "ntfy". `POST <url>` with JSON `topic`, `title`, `message`, `priority`, `tags`; `Authorization: Bearer <token>` or basic auth. Message cut to 4096 bytes on a rune boundary; an empty text is sent as the title only.

- [ ] **Step 1: Failing tests:** JSON body and priority/tags per level; message tags appended after the level tag; `priority` param overrides; bearer and basic auth headers; 5000-byte Cyrillic text cut to valid UTF-8 ≤ 4096 bytes; `429` with `Retry-After: 30` → `retry_after_ms: 30000`; `413` → permanent; `400`, `401`, `403`, `404`, `500`, refused connection → temporary; the error text never contains the token or password; schema test (unknown key, missing `topic`, `token` together with `username` rejected).
- [ ] **Step 2–4:** fail, implement, pass.
- [ ] **Step 5:** `git commit -m "chg: add the ntfy module"`

### Task 11: Module `mail`

**Files:** `cmd/onenotify-mail/{main.go,send.go,message.go,send_test.go,smtp_test.go}`

Spec §10 "mail". Headers: `From`, `To`, `Subject` (RFC 2047 via `mime.QEncoding` when not ASCII), `Date`, `Message-ID: <<id>@<domain of from>>`, `MIME-Version`, `Content-Type: text/plain; charset=utf-8`, `Content-Transfer-Encoding: quoted-printable`, `X-Onenotify-Level`. Lines in the body are CRLF and dot-stuffed by the SMTP writer.

- [ ] **Step 1:** A fake SMTP server for tests (`smtp_test.go`) on a loopback listener: scripted replies per command, optional STARTTLS with a self-signed certificate, records the envelope and the data.
- [ ] **Step 2: Failing tests:** envelope and headers of a delivered mail; a Cyrillic subject decodes back to the title; body round-trips through quoted-printable, including a line starting with `.`; several recipients; AUTH PLAIN is sent only with `username`, and only over TLS unless `tls: none`; `tls: starttls` against a server without STARTTLS → temporary error, nothing sent; `tls: tls` implicit TLS works; `552` on DATA → permanent; `550` on RCPT, `535` on AUTH, `451`, refused connection → temporary; the error text does not contain the password; schema test (missing `host`, `from`, empty `to`, bad `tls` value). A test-only param `insecure_skip_verify` is needed for the self-signed certificate: make it a real documented param, default `false`.
- [ ] **Step 3–4:** implement with `net/smtp` and `crypto/tls`, pass.
- [ ] **Step 5:** `git commit -m "chg: add the mail module"`

### Task 12: Examples, installer, CI

**Files:** `examples/config.yaml`, `internal/cli/examples_test.go`, `install.sh`, `internal/installsh/install_test.go`, `.github/workflows/{check,release}.yml`

- [ ] **Step 1:** `examples/config.yaml`: every key with its default and a comment, one channel per bundled module using `${VAR}` for secrets.
- [ ] **Step 2: Failing test** `TestExamplesLoadAndValidate`: loads the example with the needed env set, validates every channel against the real bundled modules, and fails when a directory `cmd/onenotify-<name>` has no example channel.
- [ ] **Step 3:** `install.sh` and its test copied from onecheck with `MODULES="telegram mail ntfy"` and renamed variables; workflows copied.
- [ ] **Step 4:** `make check && make build && make release-assets` pass; `dist/release` holds 16 binaries and `checksums.txt`.
- [ ] **Step 5:** `git commit -m "chg: add the example config, the installer and the CI workflows"`

### Task 13: Documentation site and README

**Files:** `README.md`, `docs/` (pages of spec §12, `blume.config.ts`, `package.json`, `package-lock.json`, `meta.ts`, `modules/meta.ts`, `public/`), `scripts/*`

- [ ] **Step 1:** Copy the Blume project files and scripts from onecheck; base `/onenotify`, title and description changed.
- [ ] **Step 2:** Write the pages. Every command output, JSON and config shown in the docs is produced by running the built binary against a temp setup, not written by hand.
- [ ] **Step 3:** `make docs-build` passes.
- [ ] **Step 4:** `git commit -m "docs: add the documentation site and the readme"`

### Task 14: End-to-end check and release

- [ ] **Step 1:** With the real built binaries: a temp config with an `ntfy` channel pointed at a local HTTP listener and a `mail` channel pointed at a closed port; drop a real onecheck event (produced by the onecheck built from `../onecheck` with a failing rule) into the source directory; `onenotify run` → the listener received the expected title and text, exit 1, mail waiting; `onenotify status` shows it; second run does not resend to ntfy.
- [ ] **Step 2:** `make check` passes in both repositories.
- [ ] **Step 3:** Whole-branch review by a fresh reviewer; fix what it finds.
- [ ] **Step 4:** Push `main`; tag and push `v0.4.0` in onecheck and `v0.1.0` in onenotify; confirm both release workflows finish and the assets are listed.
