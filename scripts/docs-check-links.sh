#!/usr/bin/env bash
# Fails when a documentation page links to another page by a relative .md
# path. The site is built with Blume, which does not rewrite such links, so
# pages link to each other by site route instead: [checks](/checks#anchor).
# It also fails when a page has a level-1 heading ("# Title"): Blume prints
# the page title itself, so the title goes into the front matter (title:)
# and a "# " heading would show it twice.
# See "Documentation site" in docs/development.md.
# Usage: scripts/docs-check-links.sh [DOCS_DIR]
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd -P)"
docs="${1:-$repo_root/docs}"

# A Markdown link target without a scheme whose path ends in .md, with an
# optional #anchor: ](checks.md) ](../checks.md#x) ](./a/b.md "title").
pattern='\]\((\./|\.\./)*[^):#[:space:]]*\.md(#[^)[:space:]]*)?([[:space:]][^)]*)?\)'

if found="$(
  cd "$docs" &&
    grep -rnE --include='*.md' --include='*.mdx' \
      --exclude-dir=superpowers --exclude-dir=node_modules \
      --exclude-dir=dist --exclude-dir=.blume \
      -o "$pattern" .
)"; then
  echo "relative links to .md files; link by site route instead, e.g. [checks](/checks):" >&2
  printf '%s\n' "$found" | sed 's#^\./##' >&2
  exit 1
fi

# A "# " line outside fenced code blocks.
if found="$(
  cd "$docs" &&
    find . \( -name superpowers -o -name node_modules -o -name dist -o -name .blume \) -prune -o \
      \( -name '*.md' -o -name '*.mdx' \) -type f -print |
    sort |
    xargs awk '
      FNR == 1 { fence = "" }
      /^[[:space:]]*(```|~~~)/ {
        mark = substr($0, match($0, /(```|~~~)/), 3)
        if (fence == "") fence = mark; else if (fence == mark) fence = ""
        next
      }
      fence == "" && /^# / { print FILENAME ":" FNR ": " $0 }
    '
)" && [ -n "$found" ]; then
  echo "level-1 headings; put the page title into the front matter (title:) instead:" >&2
  printf '%s\n' "$found" | sed 's#^\./##' >&2
  exit 1
fi
