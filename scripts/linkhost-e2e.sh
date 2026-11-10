#!/usr/bin/env bash
# FORK(hcm): end-to-end checks for the per-brand link host
# (docs/superpowers/plans/2026-11-10-brand-link-host.md).
#
# Run against a LOCAL, fresh instance only (it inserts lists 901/902, subscriber
# 901, campaigns 901/902 and sets brand ta's link_host directly in the DB,
# because the save-time check needs a real HTTPS host). Never run it against
# production.
#
# Environment:
#   LM_URL    base URL of the local instance (default http://localhost:9123)
#   LM_PSQL   command that runs psql on the instance DB, reading SQL on stdin
#             (default: docker exec -i lm-brands-pg psql -U lmtest -d lm_linkhost -Atq)
set -u
LM_URL=${LM_URL:-http://localhost:9123}
LM_PSQL=${LM_PSQL:-docker exec -i lm-brands-pg psql -U lmtest -d lm_linkhost -Atq}
psql_() { $LM_PSQL "$@"; }
TA=https://news.tacticaladvisor.net
SUB=11111111-1111-1111-1111-111111111111
CTA=22222222-2222-2222-2222-222222222222
CRI=33333333-3333-3333-3333-333333333333
CBAD=44444444-4444-4444-4444-444444444444
fails=0
pass() { echo "PASS  $1"; }
fail() { echo "FAIL  $1: $2"; fails=$((fails + 1)); }

psql_ <<SQL
UPDATE ext.brands SET link_host = '$TA' WHERE slug = 'ta';
INSERT INTO lists (id, uuid, name, type, optin, tags) VALUES
  (901, gen_random_uuid(), 'TA test', 'public', 'single', '{ta}'),
  (902, gen_random_uuid(), 'RI test', 'public', 'single', '{ri}');
INSERT INTO subscribers (id, uuid, email, status) VALUES (901, '$SUB', 'ta@example.com', 'enabled');
INSERT INTO subscriber_lists (subscriber_id, list_id, status) VALUES (901, 901, 'confirmed'), (901, 902, 'confirmed');
INSERT INTO campaigns (id, uuid, name, subject, from_email, body, content_type, messenger, status, template_id) VALUES
  (901, '$CTA', 'ta-c', 'S', 'TA <news@news.tacticaladvisor.net>', '<p>U={{ UnsubscribeURL }} V={{ MessageURL }}</p>', 'html', 'email-ta', 'finished', (SELECT id FROM templates WHERE is_default LIMIT 1)),
  (902, '$CRI', 'ri-c', 'S', 'RI <news@rangeinsiders.com>', '<p>U={{ UnsubscribeURL }} V={{ MessageURL }}</p>', 'html', 'email-ri', 'finished', (SELECT id FROM templates WHERE is_default LIMIT 1)),
  (903, '$CBAD', 'ta-wrong-messenger', 'S', 'TA <news@news.tacticaladvisor.net>', '<p>U={{ UnsubscribeURL }}</p>', 'html', 'email', 'finished', (SELECT id FROM templates WHERE is_default LIMIT 1));
INSERT INTO campaign_lists (campaign_id, list_id, list_name) VALUES (901, 901, 'TA test'), (902, 902, 'RI test'), (903, 901, 'TA test');
SQL

body=$(curl -s -H 'Host: news.tacticaladvisor.net' "$LM_URL/campaign/$CTA/$SUB")
if echo "$body" | grep -qF "U=$TA/subscription/$CTA/$SUB V=$TA/campaign/$CTA/$SUB"; then pass "ta campaign links use the brand host"; else fail "ta campaign links use the brand host" "$(echo "$body" | grep -o 'U=[^<]*')"; fi

body=$(curl -s "$LM_URL/campaign/$CRI/$SUB")
if echo "$body" | grep -q "U=.*/subscription/$CRI/$SUB" && ! echo "$body" | grep -qF "$TA"; then pass "ri campaign keeps the root URL"; else fail "ri campaign keeps the root URL" "$(echo "$body" | grep -o 'U=[^<]*')"; fi

code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: news.tacticaladvisor.net' "$LM_URL/subscription/$CTA/$SUB")
[ "$code" = 200 ] && pass "unsubscribe page through the brand host" || fail "unsubscribe page through the brand host" "HTTP $code"

code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: news.tacticaladvisor.net' -d 'List-Unsubscribe=One-Click' "$LM_URL/subscription/$CTA/$SUB")
st=$(echo "SELECT string_agg(list_id || ':' || status, ',' ORDER BY list_id) FROM subscriber_lists WHERE subscriber_id = 901" | psql_)
[ "$code" = 200 ] && [ "$st" = "901:unsubscribed,902:confirmed" ] && pass "one-click POST through the brand host unsubscribes the ta list only" || fail "one-click unsubscribe" "HTTP $code, $st"

body=$(curl -s "$LM_URL/campaign/$CBAD/$SUB")
if ! echo "$body" | grep -q "U=http"; then pass "ta lists with a messenger outside the brand render no links (fail closed)"; else fail "fail closed" "$(echo "$body" | grep -o 'U=[^<]*')"; fi

# An old campaign whose list was deleted still renders its browser view.
echo "DELETE FROM lists WHERE id = 902" | psql_
code=$(curl -s -o /dev/null -w '%{http_code}' "$LM_URL/campaign/$CRI/$SUB")
[ "$code" = 200 ] && pass "view of a campaign whose list was deleted still works" || fail "view after list delete" "HTTP $code"

code=$(curl -s -o /dev/null -w '%{http_code}' "$LM_URL/ext/link-host-check/0000")
[ "$code" = 404 ] && pass "check route refuses an unknown token" || fail "check route refuses an unknown token" "HTTP $code"

echo "failures: $fails"
[ "$fails" = 0 ]
