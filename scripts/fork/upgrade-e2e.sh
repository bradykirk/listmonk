#!/usr/bin/env bash
# End-to-end test of the gunmade fork schema step through the real binaries.
#
# cmd/upgrade.go cannot be unit tested (cmd's init() loads config.toml), and the
# bug this guards against lives in its control flow: upgrade() and
# checkUpgrade() return early when no migList migration is pending, which is
# exactly the state of a production database at v6.2.1.
#
#   1. The old build (OLD_REF) installs a v6.2.1 database; subscribers are seeded.
#   2. namecheck reports the expected blank/split counts.
#   3. The new build's normal start must refuse.
#   4. The new build's --upgrade must apply the fork step.
#   5. The new build must start and serve /health.
#   6. A second --upgrade must change nothing.
#
# Usage: scripts/fork/upgrade-e2e.sh   (needs Go and Homebrew postgresql@16)
set -euo pipefail

OLD_REF="${OLD_REF:-f525ea3f}"
PGBIN="${PGBIN:-/opt/homebrew/opt/postgresql@16/bin}"
PORT="${PORT:-55433}"
APP_ADDR="127.0.0.1:19123"
ROOT="$(git rev-parse --show-toplevel)"
WORK="$(mktemp -d)"
APP_PID=""

cleanup() {
  if [ -n "$APP_PID" ]; then kill "$APP_PID" 2>/dev/null || true; fi
  "$PGBIN/pg_ctl" -D "$WORK/pg" -m immediate stop >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "ok:   $*"; }
sql() { "$PGBIN/psql" -h 127.0.0.1 -p "$PORT" -U lmtest -d listmonk -Atq -v ON_ERROR_STOP=1 -c "$1"; }

echo "== setting up Postgres and binaries in $WORK"
"$PGBIN/initdb" -D "$WORK/pg" -U lmtest --auth=trust >/dev/null
"$PGBIN/pg_ctl" -D "$WORK/pg" -o "-p $PORT -k $WORK" -l "$WORK/pg.log" -w start >/dev/null
"$PGBIN/createdb" -h 127.0.0.1 -p "$PORT" -U lmtest listmonk

export LISTMONK_db__host=127.0.0.1 LISTMONK_db__port="$PORT" LISTMONK_db__user=lmtest \
  LISTMONK_db__password=unused LISTMONK_db__database=listmonk LISTMONK_db__ssl_mode=disable \
  LISTMONK_app__address="$APP_ADDR"

mkdir "$WORK/old"
git -C "$ROOT" archive "$OLD_REF" | tar -x -C "$WORK/old"
mkdir -p "$WORK/old/frontend/dist" "$ROOT/frontend/dist"
(cd "$WORK/old" && go build -o "$WORK/listmonk-old" ./cmd)
(cd "$ROOT" && go build -o "$WORK/listmonk-new" ./cmd)

run_old() { (cd "$WORK/old" && exec "$WORK/listmonk-old" --config '' "$@"); }
run_new() { (cd "$ROOT" && exec "$WORK/listmonk-new" --config '' "$@"); }

echo "== 1. old build installs a v6.2.1 database"
run_old --install --idempotent --yes >"$WORK/install.log" 2>&1 || { cat "$WORK/install.log"; fail "old install"; }
[ "$(sql "SELECT value->>-1 FROM settings WHERE key='migrations'")" = "v6.2.1" ] || fail "last migration is not v6.2.1"
[ "$(sql "SELECT count(*) FROM information_schema.columns WHERE table_name='subscribers' AND column_name='first_name'")" = "0" ] \
  || fail "old schema already has first_name"
sql "INSERT INTO subscribers (uuid, email, name) VALUES
  (gen_random_uuid(), 'pop.up@example.com', 'pop.up'),
  (gen_random_uuid(), 'real@example.com', 'Real Person')"
pass "v6.2.1 database with 4 subscribers (2 from install, 2 seeded)"

echo "== 2. namecheck dry run"
(cd "$ROOT" && go run ./scripts/fork/namecheck -dsn "postgres://lmtest@127.0.0.1:$PORT/listmonk?sslmode=disable") >"$WORK/namecheck.log"
grep -q "names to blank (invented from the e-mail): 1$" "$WORK/namecheck.log" || { cat "$WORK/namecheck.log"; fail "namecheck blank count"; }
grep -q "names to split into first/last: 3$" "$WORK/namecheck.log" || { cat "$WORK/namecheck.log"; fail "namecheck split count"; }
pass "namecheck: 1 to blank, 3 to split"

echo "== 3. new build: normal start must refuse"
# Backgrounded directly (not through run_new) so $! is the app's own PID: a
# backgrounded function call forks an extra wrapper shell around the "( )"
# subshell, and killing the wrapper leaves the real process running.
(cd "$ROOT" && exec "$WORK/listmonk-new" --config '') >"$WORK/start1.log" 2>&1 &
APP_PID=$!
for _ in $(seq 1 40); do kill -0 "$APP_PID" 2>/dev/null || break; sleep 0.5; done
if kill -0 "$APP_PID" 2>/dev/null; then fail "normal start kept running on a pre-fork database"; fi
set +e; wait "$APP_PID"; code=$?; set -e; APP_PID=""
[ "$code" -ne 0 ] || fail "normal start exited 0"
grep -q "gunmade fork database upgrade" "$WORK/start1.log" || { cat "$WORK/start1.log"; fail "refusal message"; }
pass "normal start refused (exit $code)"

echo "== 4. new build: --upgrade applies the fork step with no versioned migration pending"
run_new --upgrade --yes >"$WORK/upgrade1.log" 2>&1 || { cat "$WORK/upgrade1.log"; fail "--upgrade"; }
grep -q "no upgrades to run" "$WORK/upgrade1.log" || { cat "$WORK/upgrade1.log"; fail "expected no versioned migrations"; }
grep -q "gunmade fork migration applied: 4 subscribers, 1 fallback names blanked, 3 names split into first/last" "$WORK/upgrade1.log" \
  || { cat "$WORK/upgrade1.log"; fail "fork step did not run"; }
[ "$(sql "SELECT first_name || '|' || last_name || '|' || name FROM subscribers WHERE email='pop.up@example.com'")" = "||" ] || fail "pop.up not blanked"
[ "$(sql "SELECT first_name || '|' || last_name || '|' || name FROM subscribers WHERE email='real@example.com'")" = "Real|Person|Real Person" ] || fail "real not split"
[ "$(sql "SELECT first_name || '|' || last_name || '|' || name FROM subscribers WHERE email='john@example.com'")" = "John|Doe|John Doe" ] || fail "john not split"
sql "SELECT preview_text FROM campaigns LIMIT 1" >/dev/null || fail "campaigns.preview_text missing"
pass "fork step applied"

echo "== 5. new build starts and serves"
(cd "$ROOT" && exec "$WORK/listmonk-new" --config '') >"$WORK/start2.log" 2>&1 &
APP_PID=$!
up=""
for _ in $(seq 1 120); do
  if curl -fsS "http://$APP_ADDR/health" >/dev/null 2>&1; then up=1; break; fi
  kill -0 "$APP_PID" 2>/dev/null || break
  sleep 0.5
done
[ -n "$up" ] || { cat "$WORK/start2.log"; fail "app did not serve /health"; }
kill "$APP_PID"; wait "$APP_PID" 2>/dev/null || true; APP_PID=""
pass "/health responded"

echo "== 6. second --upgrade is a no-op"
before="$(sql "SELECT string_agg(id || first_name || '|' || last_name || '|' || name, ',' ORDER BY id) FROM subscribers")"
run_new --upgrade --yes >"$WORK/upgrade2.log" 2>&1 || { cat "$WORK/upgrade2.log"; fail "second --upgrade"; }
if grep -q "gunmade fork migration applied" "$WORK/upgrade2.log"; then fail "fork step ran twice"; fi
after="$(sql "SELECT string_agg(id || first_name || '|' || last_name || '|' || name, ',' ORDER BY id) FROM subscribers")"
[ "$before" = "$after" ] || fail "second --upgrade changed subscriber names"
pass "second --upgrade changed nothing"

echo "ALL PASSED"
