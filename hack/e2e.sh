#!/usr/bin/env bash
# End-to-end test of the implemented phases, using the small scenario against
# the ACTIVE cub context. It creates and deletes real entities, so it refuses
# to run unless CUB_DEMO_E2E_CONTEXT names the active context, e.g.
#
#   CUB_DEMO_E2E_CONTEXT=meridian-demo make e2e
set -euo pipefail
cd "$(dirname "$0")/.."

ctx=$(cub context get --jq .name 2>/dev/null || cub context get | awk '/Context Name/{print $3}')
if [ "${CUB_DEMO_E2E_CONTEXT:-}" != "$ctx" ]; then
  echo "active cub context is \"$ctx\"; set CUB_DEMO_E2E_CONTEXT=$ctx to confirm running against it" >&2
  exit 1
fi

demo="cub demo"
fail() { echo "e2e: FAIL: $*" >&2; exit 1; }

$demo plan e2e >/dev/null || fail "plan"
$demo install e2e
$demo up --demo e2e
out=$($demo status --demo e2e)
grep -q "Everything the implemented phases create exists." <<<"$out" || fail "status incomplete after up"
# Idempotency: a second run creates nothing. (Captured first: grep -q closes
# the pipe early, which pipefail would turn into a false failure.)
out=$($demo up --demo e2e)
grep -q "(0 created or completed)" <<<"$out" || fail "second up was not a no-op"
$demo down --demo e2e --yes
# down keeps the installed definition; only the e2e-scenario space remains
n=$(cub space list --where "Labels.DemoName = 'e2e'" --no-headers | wc -l | tr -d ' ')
[ "$n" = "1" ] || fail "down should leave only the definition space, left $n"
$demo uninstall --demo e2e --yes
n=$(cub space list --where "Labels.DemoName = 'e2e'" --no-headers | wc -l | tr -d ' ')
[ "$n" = "0" ] || fail "uninstall left $n spaces"
echo "e2e: OK"
