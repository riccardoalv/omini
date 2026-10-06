#!/bin/sh
# Validates a commit message against Conventional Commits (https://www.conventionalcommits.org).
# Used by the lefthook commit-msg hook; CI runs commitlint with the same rules.
set -eu

msg_file="$1"
header=$(sed -n '1p' "$msg_file")

# Let git-generated messages through.
case "$header" in
  Merge\ *|Revert\ *|fixup!\ *|squash!\ *) exit 0 ;;
esac

types="feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert"
pattern="^($types)(\([a-z0-9._/-]+\))?!?: .+"

if ! printf '%s' "$header" | grep -Eq "$pattern"; then
  cat >&2 <<MSG
✖ Commit message does not follow Conventional Commits:

    $header

Expected:  <type>(<optional scope>): <description>
Types:     feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert
Examples:  feat(snmp): read LLDP neighbors
           fix(topology): ignore uplink ports when placing clients
           docs: explain plugin manifest

See CONTRIBUTING.md.
MSG
  exit 1
fi

if [ "${#header}" -gt 100 ]; then
  echo "✖ Commit header is longer than 100 characters (${#header})." >&2
  exit 1
fi
