# Short failed fields in onecheck notifications

## Intent

A notification about a failed onecheck rule must say what broke. Today
`failureLines` (`internal/message/onecheck.go`) prints each
`result.failures[].message` cut to 300 characters. For `any`/`not`/`some` the
message holds the whole element and the whole condition, so the cut drops the
part that matters (which container is extra, for one).

onecheck (commit `84e7eef`, "Failed fields" in its `docs/output-and-results.md`)
now adds `fields` to each failure: the leaf fields the condition broke on, each
with `path`, optional `json_path`, `operator`, `actual` and optional `missing`.
At most 20 per failure, no duplicates. Older onecheck sends no `fields`.

## Goals

- When a failure has `fields`, print one short line per field instead of its
  `message`.
- When it has none (older onecheck), print `message` as today.

## Non-goals

- No config key to bring back full messages. The old format stays only as the
  fallback for events without `fields` (decided by the user).
- No change to the module protocol: `Message.Text` stays plain text.

## Approaches

- (a) Fallback only: `fields` when present, `message` otherwise. **Picked.**
- (b) A config key choosing between fields and messages: global only (the
  text is built once at intake for all channels), affects only new events;
  rejected as a setting nobody needs.
- (c) Both, fields and a cut message below them: makes the notification long
  again, which is the problem being solved; rejected.

## Design

Line forms, under the existing `failed:` / `failed N runs in a row:` head:

```
- <path> = <value>
- <path>: missing
- <path> json <json_path> = <value>
- <path> json <json_path>: missing
```

Example:

```
failed 3 runs in a row:
- containers.#(state=="running")#.2.name = "smoke-tls-proxy"
- status: expected equals 200, got 503
```

- `<value>` is `actual` as compact JSON (strings quoted, HTML not escaped),
  on one line.
- `operator` is not shown: without the expected value it explains little.
  (Our call.)
- Failures keep their order; a failure with `fields` contributes its field
  lines, one without contributes its `message` line.
- Duplicate lines are dropped: fields are equal when `path`, `json_path`,
  `missing` and the value are equal; equal message lines are printed once.
  (Comparing the rendered line covers both.)
- The limit counts printed lines, not failures: at most `maxFailures` (10),
  then `… and N more` with N the remaining distinct lines. Each line is cut
  to `maxErrorLine` (300) characters.
- Unchanged: `redacted` hides all of it, status `error` prints `error: …`.

## Tests

The docker example; fallback to `message`; `missing`; `json_path`; duplicates
across failures; the limit; `redacted`.

## Docs

`docs/messages.md`: the failed-check example and the description of field
lines and the fallback.
