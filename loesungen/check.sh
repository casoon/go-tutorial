#!/usr/bin/env bash
# Applies every solution patch on its own to a clean checkout of main and
# runs go vet and go test. Run it after main has changed to find solutions
# that no longer apply or no longer pass.
#
#   loesungen/check.sh                 all patches
#   loesungen/check.sh 06-concurrency  only the patches of one chapter
#
# A patch may name build tags in a header line before the first diff,
# e.g. "# test-tags: uebung". git apply ignores such lines.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
base="${BASE:-main}"
filter="${1:-}"
status=0

for patch in "$root"/loesungen/*"$filter"*/*.patch; do
  [[ -e "$patch" ]] || continue
  name="${patch#"$root"/}"
  tags="$(sed -n 's/^# test-tags: *//p' "$patch" | head -1)"
  tmp="$(mktemp -d)"
  git -C "$root" worktree add -q --detach "$tmp" "$base"
  if (
    cd "$tmp" &&
      git apply "$patch" &&
      go vet ${tags:+-tags "$tags"} ./... &&
      go test ${tags:+-tags "$tags"} ./... >/dev/null
  ) 2>"$tmp.err"; then
    echo "ok    $name"
  else
    echo "FAIL  $name"
    sed 's/^/      /' "$tmp.err" | tail -15
    status=1
  fi
  git -C "$root" worktree remove --force "$tmp"
  rm -f "$tmp.err"
done

exit "$status"
