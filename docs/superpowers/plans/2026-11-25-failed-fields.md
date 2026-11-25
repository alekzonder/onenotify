# Short Failed Fields Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In a onecheck notification, print one short line per failed field (`failures[].fields`) instead of the long `message`, with `message` as the fallback.

**Architecture:** Only `failureLines` in `internal/message/onecheck.go` and the `event` struct change. Lines are rendered first, deduplicated by their text, then limited and cut.

**Tech Stack:** Go, standard library only.

**Spec:** `docs/superpowers/specs/2026-11-25-failed-fields-design.md`

## Global Constraints

- `make check` passes (gofmt, go vet, golangci-lint, race tests); gocyclo ≤ 25, no `//nolint:gocyclo`.
- `make docs-build` passes after `docs/` changes; links by site route.
- Commits: `chg: …` / `docs: …`, lower case, no attribution trailers (AGENTS.md).
- `pkg/module` and `Message.Text` type unchanged.
- Limits: `maxFailures` = 10 lines, `maxErrorLine` = 300 characters per line.

## Review Focus

1. A field with an empty `path`: skip it; if no field of a failure is usable, fall back to its `message`.
2. A string `actual` with a newline or control characters: it stays on one line (`oneLine` after encoding).
3. `actual` that is an object or array: compact JSON, HTML not escaped (`<`, `&` kept as they are).
4. `actual: null` without `missing`: `= null`, not `: missing`.
5. A number `actual` keeps its written form (decoder uses `UseNumber`), e.g. `12345678901` and `1.50`.

---

### Task 1: Field lines in `failureLines`

**Files:**
- Modify: `internal/message/onecheck.go` (`event.Result.Failures`, `failureLines`, new helpers)
- Test: `internal/message/onecheck_test.go` (new cases in `TestFromOnecheckText`)

**Interfaces:**
- Produces: `type failedField struct { Path string; JSONPath string; Actual any; Missing bool }` (json tags `path`, `json_path`, `actual`, `missing`); `Failures []struct{ Message string; Fields []failedField }`; `func fieldLine(f failedField) string` (returns "" for an empty path); `func jsonValue(v any) string`.

- [ ] **Step 1: Write the failing tests** as cases of `TestFromOnecheckText` (`top: {"params": nil}`, `result: {"tags": nil, "failures": …}`, text starting `module: http\nat: 2026-11-23 10:02:00 UTC\nfailed 3 runs in a row:\n`):
  - docker: failure with path `containers.#(state=="running")#.2`, operator `any`, long message, fields `[{path: containers.#(state=="running")#.2.name, operator: in, actual: "smoke-tls-proxy"}]` → `- containers.#(state=="running")#.2.name = "smoke-tls-proxy"`.
  - fallback: failure without `fields` and one with `fields: []` → their messages.
  - missing: `{path: a.b, operator: exists, actual: null, missing: true}` → `- a.b: missing`; `{path: c, actual: null}` → `- c = null`.
  - json_path: `{path: body, json_path: status, actual: "degraded"}` → `- body json status = "degraded"`; with `missing: true` → `- body json status: missing`.
  - kinds: actual `{"k":"<v>&"}` → `= {"k":"<v>&"}`; actual `"a\nb"` → `= "a\nb"` (escaped by JSON, one line); number `12345678901` → `= 12345678901`.
  - empty path: failure with fields `[{path: "", actual: 1}]` and message `m` → `- m`.
  - duplicates: two failures each with field `x = 1`, plus two failures with message `same` → `- x = 1\n- same`.
  - limit: one failure with 12 fields `f0..f11` (actual 0..11) → 10 lines then `… and 2 more`.
  - redacted: `top: {"redacted": true}` with fields present → no `failed` block (assert text has no `failed`).

- [ ] **Step 2: Run** `go test ./internal/message/ -run TestFromOnecheckText` — expected FAIL (messages printed instead of fields).

- [ ] **Step 3: Implement.** `failureLines` builds the candidate lines in order: for each failure, the non-empty `fieldLine`s of its fields; if none, `oneLine(f.Message)`. Drop lines already seen (a `map[string]bool`). Print up to `maxFailures`, each `"- " + cut(line, maxErrorLine)`, then `… and N more` for the rest. `fieldLine`: `name := oneLine(path)`; append ` json ` + `oneLine(json_path)` when set; then `: missing` or ` = ` + `jsonValue(actual)`. `jsonValue`: `json.Encoder` with `SetEscapeHTML(false)`, result through `oneLine`. Split helpers so gocyclo stays low.

- [ ] **Step 4: Run** `go test ./internal/message/` — expected PASS, including the old "more failures than fit" case.

- [ ] **Step 5: Commit** `chg: show the failed fields of a onecheck failure instead of its message`.

### Task 2: Documentation

**Files:**
- Modify: `docs/messages.md` (section "A failed check", the JSON example near the top if it shows failures — it may stay, as messages are still a valid fallback)

- [ ] **Step 1:** In "A failed check", change the example to field lines (e.g. `- body json status = "degraded"` and `- status = 503`), and describe: one line per field onecheck reports (`fields`), the four forms, values as JSON, operator not shown, duplicates printed once, at most 10 lines then `… and N more`, each cut to 300 characters; an event from older onecheck without `fields` gets onecheck's message per condition as before. Link onecheck's "Failed fields" doc by full URL.
- [ ] **Step 2: Run** `make docs-build` and `make check` — expected both pass.
- [ ] **Step 3: Commit** `docs: describe failed field lines in notifications`.
