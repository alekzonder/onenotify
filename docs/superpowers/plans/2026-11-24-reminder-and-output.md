# Reminder Event and Output Block Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A onecheck `reminder` event becomes an `error` notification titled `STILL FAILING` with failure reasons and a `failing since:` line; a `fail` or `reminder` notification shows the output fields the rule allowed.

**Architecture:** Everything happens in `message.FromOnecheck`, where the text of an event is built by fixed rules. The output block lives in a new file of the same package. Modules, the module protocol and the queue file format do not change.

**Tech Stack:** Go, standard library only.

**Spec:** `docs/superpowers/specs/2026-11-24-reminder-and-output-design.md`

## Global Constraints

- `make check` passes (gofmt, vet, golangci-lint, race tests); cyclomatic complexity ≤ 25, no `//nolint:gocyclo`. `FromOnecheck` is already long: move decisions into helpers.
- Commit messages: `chg:` / `fix:` / `docs:`, lower case, imperative; no attribution trailers; no merge commits.
- Tests use no network and write nothing outside `t.TempDir()`.
- Docs pages link by site route, never by `.md` path; `make docs-build` passes.
- Output limits, verbatim from the spec: last **20** lines and at most **1000** characters per field; the beginning is dropped.
- An event that is delivered today is still delivered (events without `since`, `output_fields`, `result.output`).

## Review Focus

1. `output_fields` names a field whose value is a single line longer than 1000 characters (a minified JSON body): the last 1000 characters with a leading `…`, never an empty block.
2. Output with `\r` progress redraws and ANSI colours: readable lines, no `[31m` debris, no control characters in the text.
3. `output_fields` repeats a name or names a key missing from `result.output`: one block per name, no empty header.
4. A reminder whose event has no `result` or no `since`: still a `STILL FAILING` error message, no panic, no `failing since:` line.
5. A field name in `output_fields` containing a line break: the header stays one line.

Each has a test in the task that owns the code (1, 2, 3, 5 in Task 2; 4 in Task 1).

---

### Task 1: Reminder

**Files:**
- Modify: `internal/message/onecheck.go`
- Test: `internal/message/onecheck_test.go`

**Interfaces:**
- Produces: `event.Since *time.Time` (`json:"since"`); `func (ev *event) failing() bool` — true for `fail` and `reminder`; used by Task 2 to decide whether output is shown.

- [ ] **Step 1: Failing tests.** Add cases to the table of `TestFromOnecheckText` (base for all: `"id": "20261123T110215Z-main-api-health-reminder", "transition": "reminder", "from": "fail", "at": "2026-11-23T11:02:15Z", "since": "2026-11-23T10:02:00Z"`, result `"consecutive": 63`):
  - reminder: title `[web-01] STILL FAILING main/api-health`, level `error`, text `module: http\nheaders.Authorization: Bearer ${API_TOKEN}\nurl: https://example.com/health\ntags: api, web\nat: 2026-11-23 11:02:15 UTC\nfailing since: 2026-11-23 10:02:00 UTC\nfailed 63 runs in a row:\n- status: expected equals 200, got 503`
  - reminder of a check that cannot run (`status: error`, `error: "timed out"`): ends with `failing since: …\nerror: timed out`
  - redacted reminder: `module: http\ntags: api, web\nat: 2026-11-23 11:02:15 UTC\nfailing since: 2026-11-23 10:02:00 UTC`
  - reminder without `since` (`drop`): no `failing since:` line
  - reminder with `"result": nil, "params": nil`: text `at: …\nfailing since: …`
  - the existing fail cases get `"since"` equal to `at` in the base event of `ev()` and must stay unchanged (no `failing since:` for `fail`); a `recovered` case with `since` has no such line either.
- [ ] **Step 2:** `go test ./internal/message/` — the new cases fail.
- [ ] **Step 3: Implement.** Add `Since`; move title word and level into `func headline(transition string) (word, level string)` (`fail` → `FAIL`/error, `reminder` → `STILL FAILING`/error, `recovered` → `RECOVERED`/ok, other → upper-cased/info); print `failing since: ` + `textTime` after `at:` for `reminder` only when `Since` is set and non-zero; failure lines when `ev.failing() && !ev.Redacted`. Update the doc comment of `FromOnecheck`.
- [ ] **Step 4:** `go test -race ./internal/message/` — PASS.
- [ ] **Step 5:** Commit `chg: deliver a reminder event as a still failing error with its reasons`.

### Task 2: Output block

**Files:**
- Create: `internal/message/output.go`
- Modify: `internal/message/onecheck.go` (event fields, one call), `internal/message/onecheck_test.go` (the leak check)
- Test: `internal/message/output_test.go`

**Interfaces:**
- Consumes: `(*event).failing()`, `flatten`, `scalar`, `oneLine` from the package.
- Produces: `event.OutputFields []string` (`json:"output_fields"`), `event.Result.Output map[string]any` (`json:"output"`), `func outputLines(fields []string, output map[string]any) []string`, constants `maxOutputLines = 20`, `maxOutputChars = 1000`.

- [ ] **Step 1: Failing tests** in `output_test.go`, through `FromOnecheck` with the `ev()` helper (`params: nil`, `tags: nil`, so the expected text is short):
  - `TestOutputBlock`: `output_fields: ["stderr"]`, output `{"stdout": "SECRET BODY", "stderr": "one\ntwo\n"}` → text ends with `- status: expected equals 200, got 503\nstderr:\none\ntwo`; same for a `reminder`.
  - `TestOutputNotShown`: `recovered`, unknown transition, `redacted: true` (with `output_fields` set anyway), `output_fields: []`, key absent → no `stderr:` and no output text.
  - `TestOutputFields`: two fields in the order of `output_fields`; a repeated name once; a missing key, `null`, `""` and `"\n \n"` skipped with their headers; object `{"headers": {"content-type": "text/plain", "x": {"y": 1}}}` → `headers:\ncontent-type: text/plain\nx.y: 1`; number `7` → `n:\n7`; field name `"std\nerr"` → header `std err:`.
  - `TestOutputIsCleaned`: `"a\r\nb\rc\td\x1b[31mred\x1b[0m\x00  \n\n"` → `a\nb\nc d red`; invalid UTF-8 dropped.
  - `TestOutputLimits`: 25 lines `l01..l25` → `…\nl06\n…\nl25` (marker line then the last 20); 10 lines of 150 `я` → `…` then the last 6 lines (6×150+5 newlines ≤ 1000); one line of 1500 `я` → `…` + 1000 `я`, no separate marker line; exactly 20 lines and exactly 1000 characters → unchanged, no marker.
  - In `TestFromOnecheckText` the base event gets no `output_fields`; the `SECRET BODY` check stays.
- [ ] **Step 2:** `go test ./internal/message/` — FAIL (undefined / no block).
- [ ] **Step 3: Implement `outputLines`.** For each distinct name in order: value string → `cleanOutput`; `map[string]any` → `flatten` lines sorted and joined by `\n`; `nil` → skip; anything else → `scalar`. Then `tail`. Skip when empty. Emit `oneLine(name) + ":"` and the lines; a name that is empty after `oneLine` is skipped.
  - `cleanOutput(s string) string`: `strings.ToValidUTF8`; remove `\x1b\[[0-?]*[ -/]*[@-~]` (package-level regexp); `\r\n` and `\r` → `\n`; other control characters → space; right-trim each line; drop leading and trailing empty lines.
  - `tail(lines []string) []string`: keep the last `maxOutputLines`; drop lines from the start while more than one remains and the rune count including joining newlines exceeds `maxOutputChars`; a single line over the limit becomes `"…"` + its last `maxOutputChars` runes; if whole lines were dropped and the first line was not cut this way, prepend the line `…`.
  - Call from `FromOnecheck` after the failure lines, under the same condition.
- [ ] **Step 4:** `go test -race ./internal/message/` — PASS.
- [ ] **Step 5:** Commit `chg: show the output fields a rule allows after the failure reasons`.

### Task 3: Documentation

**Files:**
- Modify: `docs/messages.md`, `docs/index.md`, `docs/modules/mail.md`

- [ ] **Step 1:** `docs/messages.md`: levels table (`error` comes from a `fail` or `reminder` event); intro of "From a onecheck event" names the three events; title and level lines; text list gets `failing since:` (item 5, reminder only), reasons (6, `fail` and `reminder`), output (7); new sections "A reminder" and "Output of the check" with the example of the spec; "A recovery" and "A hidden rule" mention output and the reminder; limits table gets the output row; the sentence about line breaks excepts output; "What a notification can reveal": "Never included" becomes "Included only when the rule allows it" with a link to onecheck's `state-and-events#output-in-notifications`.
- [ ] **Step 2:** `docs/index.md`: reminders are no longer "deliberately not there"; say they come from onecheck. `docs/modules/mail.md`: the subject word list gets `STILL FAILING`.
- [ ] **Step 3:** `make check && make docs-build` — both pass.
- [ ] **Step 4:** Commit `docs: describe reminders and the output block of a notification`.
