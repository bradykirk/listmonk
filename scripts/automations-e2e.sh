#!/usr/bin/env bash
# End-to-end API checks for the automations feature.
#
# Run against a LOCAL, fresh instance only. The script creates its own data
# (lists, subscribers, an automation and its step campaign, roles and API users)
# and overwrites the brands ri and ta with their seed values. Never run it
# against production.
#
# Environment (same as scripts/brands-e2e.sh):
#   LM_URL           base URL (default http://localhost:9000; host must be local)
#   LM_USER          super admin API user name
#   LM_PASS          super admin API token
#   LM_SMTP_CAPTURE  file written by the local sink (required here):
#                      bash scripts/brands-e2e.sh --smtp-sink PORT FILE
#
# Instance requirements:
#   - Every SMTP server points at the local sink (host 127.0.0.1, auth and TLS off),
#     so no real mail leaves the machine.
#   - An SMTP server named "email-ri" (also pointing at the sink), so that the
#     messenger email-ri exists. Brand ri allows only that messenger.
#   - Use @example.com addresses only (the script does).
#
# The worker ticks every 30 s, so the mail checks wait up to 60 s each. A full
# run takes about 5 minutes.
#
# Usage: LM_USER=... LM_PASS=... LM_SMTP_CAPTURE=/path/capture bash scripts/automations-e2e.sh

LM_URL=${LM_URL:-http://localhost:9000}
LM_URL=${LM_URL%/}

for bin in curl jq python3; do
  command -v "$bin" >/dev/null 2>&1 || { echo "missing required tool: $bin"; exit 2; }
done
[ -n "${LM_USER:-}" ] && [ -n "${LM_PASS:-}" ] || { echo "set LM_USER and LM_PASS"; exit 2; }
if ! printf '%s' "$LM_URL" | grep -Eq '^http://(localhost|127\.0\.0\.1)(:[0-9]+)?$'; then
  echo "refusing to run against a non-local URL: $LM_URL"; exit 2
fi
[ -n "${LM_SMTP_CAPTURE:-}" ] || { echo "set LM_SMTP_CAPTURE (see bash scripts/brands-e2e.sh --smtp-sink)"; exit 2; }
touch "$LM_SMTP_CAPTURE" || { echo "cannot write $LM_SMTP_CAPTURE"; exit 2; }

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
RUN="$(date +%s)$RANDOM"
ADMIN_USER=$LM_USER
ADMIN_PASS=$LM_PASS
U=$ADMIN_USER
P=$ADMIN_PASS
BRAND=""
CODE=""
BODY=""

PASS_N=0
FAIL_N=0
FAILED=""

# ---------- helpers ----------

pass() { PASS_N=$((PASS_N + 1)); echo "PASS  $1"; }
fail() {
  FAIL_N=$((FAIL_N + 1))
  FAILED="$FAILED
  $1"
  echo "FAIL  $1"
  [ -n "${2:-}" ] && echo "      $2" | cut -c1-400
}

# req METHOD PATH [JSON]. Sets CODE and BODY. Uses U, P and BRAND (header).
req() {
  local m=$1 p=$2 d=${3-}
  local args=(-sS -m 60 -X "$m" -o "$TMP/body" -w '%{http_code}' -H "Authorization: token ${U}:${P}")
  [ -n "$BRAND" ] && args+=(-H "X-Listmonk-Brand: $BRAND")
  if [ -n "$d" ]; then args+=(-H 'Content-Type: application/json' --data "$d"); fi
  CODE=$(curl "${args[@]}" "$LM_URL$p" 2>"$TMP/err") || CODE="000"
  BODY=$(cat "$TMP/body" 2>/dev/null)
  [ "$CODE" = "000" ] && BODY="curl error: $(cat "$TMP/err")"
  return 0
}

# Run req as the super admin, with no brand header, whatever the caller set.
admin() { U=$ADMIN_USER P=$ADMIN_PASS BRAND="" req "$@"; }

# form EMAIL LIST_UUID: the public subscription form (no auth). Sets CODE.
form() {
  CODE=$(curl -sS -m 60 -o "$TMP/body" -w '%{http_code}' -X POST \
    --data-urlencode "email=$1" --data-urlencode "l=$2" "$LM_URL/subscription/form" 2>"$TMP/err") || CODE="000"
  BODY=$(head -c 400 "$TMP/body" 2>/dev/null)
}

expect() {
  if [ "$CODE" = "$2" ]; then pass "$1"; else fail "$1" "want HTTP $2, got $CODE: $BODY"; fi
}

expect_msg() {
  local msg
  msg=$(printf '%s' "$BODY" | jq -r '.message // empty' 2>/dev/null)
  if [ "$CODE" = "$2" ] && printf '%s' "$msg" | grep -qF -- "$3"; then
    pass "$1"
  else
    fail "$1" "want HTTP $2 with message containing [$3], got $CODE: $BODY"
  fi
}

jq_ok() {
  local name=$1 filter=$2
  shift 2
  if printf '%s' "$BODY" | jq -e "$@" "$filter" >/dev/null 2>&1; then
    pass "$name"
  else
    fail "$name" "filter [$filter] false on: $BODY"
  fi
}

jget() { local f=$1; shift; printf '%s' "$BODY" | jq -r "$@" "$f"; }

# mail_count EMAIL [CAMPAIGN_UUID]: captured messages whose To header has EMAIL
# (and, when given, whose X-Listmonk-Campaign header is CAMPAIGN_UUID).
mail_count() {
  python3 -I - "$LM_SMTP_CAPTURE" "$1" "${2:-}" <<'PYEOF'
import sys
path, addr, uuid = sys.argv[1], sys.argv[2].lower(), sys.argv[3]
n = 0
for m in open(path, encoding="utf-8", errors="replace").read().split("=====MESSAGE=====\n")[1:]:
    lines = m.split("\n")
    if addr not in lines[0].lower():
        continue
    if uuid and not any(l == "X-Listmonk-Campaign: " + uuid for l in lines[1:3]):
        continue
    n += 1
print(n)
PYEOF
}

# wait_mail EMAIL WANT SECONDS [UUID]: poll until the count reaches WANT or time is up.
# Prints the last count.
wait_mail() {
  local i n=0
  for i in $(seq 1 "$3"); do
    n=$(mail_count "$1" "${4:-}")
    [ "$n" -ge "$2" ] && break
    sleep 1
  done
  echo "$n"
}

# enrolled_count: the enrolled count of automation $AID.
enrolled_count() {
  admin GET "/api/ext/automations/$AID"
  jget '.data.counts.enrolled'
}

list_json() { # NAME TAGS_JSON TYPE
  jq -nc --arg n "$1" --argjson t "$2" --arg ty "$3" \
    '{name:$n, type:$ty, optin:"single", tags:$t, description:""}'
}

# camp_put [key=value ...]: the step campaign PUT body. Defaults are a valid
# ri email; each key=value argument overrides one field.
camp_put() {
  local subject="Welcome $RUN" body="<p>Welcome</p>" alt="" preview="" messenger="email-ri" lists="[$L]" kv
  for kv in "$@"; do
    case $kv in
      subject=*) subject=${kv#*=} ;;
      body=*) body=${kv#*=} ;;
      alt=*) alt=${kv#*=} ;;
      preview=*) preview=${kv#*=} ;;
      messenger=*) messenger=${kv#*=} ;;
      lists=*) lists=${kv#*=} ;;
    esac
  done
  jq -nc --arg n "Automation: auto-e2e-$RUN" --arg s "$subject" --arg b "$body" --arg a "$alt" \
    --arg p "$preview" --arg m "$messenger" --argjson l "$lists" --argjson t "$TPL" \
    '{name:$n, subject:$s, lists:$l, from_email:"E2E RI <news@rangeinsiders.com>", type:"regular",
      content_type:"richtext", body:$b, altbody:$a, preview_text:$p, messenger:$m,
      template_id:$t, tags:[]}'
}

auto_put() { # ENABLED(true|false)
  jq -nc --arg n "auto-e2e-$RUN" --argjson e "$1" '{name:$n, enabled:$e, steps:[{position:1, delay_minutes:0}]}'
}

mk_sub() { # EMAIL LISTS_JSON
  admin POST /api/subscribers "$(jq -nc --arg e "$1" --argjson l "$2" \
    '{email:$e, name:"E2E Sub", status:"enabled", lists:$l, preconfirm_subscriptions:true}')"
  jget .data.id
}

# mk_api_user NAME USER_PERMS_JSON LIST_PERMS_JSON: creates a role pair and an API
# user. Sets NEW_USER and NEW_TOKEN (never printed).
mk_api_user() {
  local ur lr
  admin POST /api/roles/users "$(jq -nc --arg n "$1-role-$RUN" --argjson p "$2" '{name:$n, permissions:$p}')"
  ur=$(jget .data.id)
  admin POST /api/roles/lists "$(jq -nc --arg n "$1-lrole-$RUN" --argjson l "$L" --argjson p "$3" \
    '{name:$n, lists:[{id:$l, permissions:$p}]}')"
  lr=$(jget .data.id)
  admin POST /api/users "$(jq -nc --arg u "$1$RUN" --argjson ur "${ur:-0}" --argjson lr "${lr:-0}" \
    '{username:$u, name:$u, type:"api", status:"enabled", user_role_id:$ur, list_role_id:$lr}')"
  NEW_USER=$(jget .data.username)
  NEW_TOKEN=$(jget .data.password)
  if [ -z "$NEW_TOKEN" ] || [ "$NEW_TOKEN" = "null" ]; then
    fail "create API user $1" "HTTP $CODE: $(printf '%s' "$BODY" | jq -c 'del(.data.password)' 2>/dev/null)"
    return 1
  fi
  pass "created API user $1"
}

# ---------- preflight ----------

echo "== preflight: $LM_URL (run $RUN)"
admin GET /api/health
[ "$CODE" = "200" ] || { echo "server not reachable or credentials rejected (HTTP $CODE)"; exit 2; }
admin GET /api/config
if ! printf '%s' "$BODY" | jq -e '.data.messengers | index("email-ri")' >/dev/null 2>&1; then
  echo "the instance has no messenger email-ri (add an SMTP server named email-ri that points at the sink)"; exit 2
fi
admin GET /api/settings
if [ "$(jget '.data["app.enable_public_subscription_page"]')" != "true" ]; then
  echo "app.enable_public_subscription_page must be true (public form checks)"; exit 2
fi

upsert_brand() { # SLUG NAME TAGS_JSON MESSENGERS_JSON DOMAINS_JSON SORT
  local j
  j=$(jq -nc --arg s "$1" --arg n "$2" --argjson t "$3" --argjson m "$4" --argjson d "$5" --argjson o "$6" \
    '{slug:$s, name:$n, list_tags:$t, messengers:$m, from_domains:$d, sort_order:$o}')
  admin POST /api/ext/brands "$j"
  if [ "$CODE" = "409" ]; then admin PUT "/api/ext/brands/$1" "$j"; fi
  expect "brand $1 has its seed values" 200
}
upsert_brand ri "RangeInsiders" '["ri","ri-cold"]' '["email-ri"]' '["rangeinsiders.com"]' 1
upsert_brand ta "Tactical Advisor" '["ta"]' '["email-ta"]' '["news.tacticaladvisor.net"]' 2

# ---------- 1: create ----------

echo "== 1: list and automation"
admin POST /api/lists "$(list_json "auto-e2e-$RUN" '["ri"]' public)";  L=$(jget .data.id); L_UUID=$(jget .data.uuid)
admin POST /api/lists "$(list_json "other-e2e-$RUN" '["ri"]' public)"; O=$(jget .data.id); O_UUID=$(jget .data.uuid)
admin POST /api/lists "$(list_json "ta-e2e-$RUN" '["ta"]' private)";   T=$(jget .data.id)
for v in "$L" "$O" "$T" "$L_UUID"; do
  [ -n "$v" ] && [ "$v" != "null" ] || { echo "could not create lists: $BODY"; exit 2; }
done
pass "created lists auto-e2e=$L other=$O ta=$T"

admin POST /api/ext/automations "$(jq -nc --arg n "auto-e2e-$RUN" --argjson l "$L" '{name:$n, list_id:$l}')"
expect "create automation -> 200" 200
AID=$(jget .data.id)
[ -n "$AID" ] && [ "$AID" != "null" ] || { echo "could not create the automation: $BODY"; exit 2; }
jq_ok "new automation is disabled" '.data.enabled == false'
admin GET "/api/ext/automations/$AID"
jq_ok "automation has one step with a campaign id" '(.data.steps | length) == 1 and .data.steps[0].campaign_id > 0'
CID=$(jget '.data.steps[0].campaign_id')
admin GET "/api/campaigns/$CID"
CUUID=$(jget .data.uuid)
TPL=$(jget '.data.template_id // 1')
jq_ok "step campaign default messenger is the brand's first (email-ri)" '.data.messenger == "email-ri"'

# ---------- 2: enable needs campaigns:send ----------

echo "== 2: enable without campaigns:send"
mk_api_user e2emanage '["campaigns:get","campaigns:manage"]' '["list:get","list:manage"]' && {
  MAN_USER=$NEW_USER MAN_TOKEN=$NEW_TOKEN
}
man() { U=$MAN_USER P=$MAN_TOKEN req "$@"; }
man PUT "/api/ext/automations/$AID" "$(auto_put true)"
expect_msg "enable by a user with campaigns:manage only -> 403" 403 "campaigns:send"
admin GET "/api/ext/automations/$AID"
jq_ok "automation is still disabled after the refused enable" '.data.automation.enabled == false'

# ---------- 3: marker and guard ----------

echo "== 3: marker and send guard"
admin PUT "/api/campaigns/$CID" "$(camp_put 'body=<p>[[SENDER_INFO]]</p>')"
expect "disabled: set step body with the marker -> 200" 200
admin PUT "/api/ext/automations/$AID" "$(auto_put true)"
expect_msg "enable with the marker in the body -> 400" 400 "still contains"

admin PUT "/api/campaigns/$CID" "$(camp_put messenger=email)"
expect "disabled: set body Welcome and messenger email -> 200" 200
admin PUT "/api/ext/automations/$AID" "$(auto_put true)"
expect_msg "enable with messenger email (not in brand ri) -> 400" 400 "Messenger email is not allowed"

admin PUT "/api/campaigns/$CID" "$(camp_put)"
expect "disabled: set messenger email-ri -> 200" 200
admin PUT "/api/ext/automations/$AID" "$(auto_put true)"
expect "enable -> 200" 200
jq_ok "automation is enabled" '.data.enabled == true'

echo "== 3b: edits of an enabled automation's email"
man PUT "/api/campaigns/$CID" "$(camp_put 'subject=Changed')"
expect_msg "enabled: campaigns:manage-only user edits the email -> 403" 403 "campaigns:send"
for f in subject body alt preview; do
  admin PUT "/api/campaigns/$CID" "$(camp_put "$f=x [[SENDER_INFO]] {{ UnsubscribeURL }}")"
  expect_msg "enabled: marker in $f -> 400" 400 "Replace [[SENDER_INFO]]"
done
admin PUT "/api/campaigns/$CID" "$(camp_put "lists=[$O]")"
expect_msg "step campaign lists changed away from the automation list -> 400" 400 "automation list"
admin PUT "/api/campaigns/$CID" "$(camp_put "lists=[$L,$O]")"
expect_msg "step campaign lists with an extra list -> 400" 400 "automation list"
admin GET "/api/campaigns/$CID"
jq_ok "step campaign unchanged after the refused edits" \
  '.data.body == "<p>Welcome</p>" and .data.preview_text == "" and ([.data.lists[].id] == [$l])' --argjson l "$L"
admin DELETE "/api/ext/automations/$AID"
expect_msg "delete while enabled -> 400" 400 "Disable the automation first"

# ---------- 4: hidden step campaign ----------

echo "== 4: the step campaign is hidden and guarded"
admin GET "/api/campaigns?per_page=all"
jq_ok "GET /api/campaigns does not list the step campaign" '[.data.results[].id] | index($c) == null' --argjson c "$CID"
admin GET "/api/campaigns/$CID"
expect "GET /api/campaigns/<step> -> 200" 200
admin PUT "/api/campaigns/$CID/status" '{"status":"running"}'
expect_msg "PUT step campaign status running -> 400" 400 "automation email"
admin DELETE "/api/campaigns/$CID"
expect_msg "DELETE step campaign -> 400" 400 "automation email"

# ---------- 5: enroll ----------

echo "== 5: API enroll"
E1="e2e1-$RUN@example.com"
S1=$(mk_sub "$E1" "[$L]")
[ -n "$S1" ] && [ "$S1" != "null" ] || { echo "could not create subscriber: $BODY"; exit 2; }
ENR=$(jq -nc --argjson s "$S1" --argjson l "$L" '{subscriber_id:$s, list_id:$l}')

BRAND=ta req POST /api/ext/automations/enroll "$ENR"
expect "enroll with brand header ta to the ri list -> 404" 404
mk_api_user e2eenroll '["subscribers:get","subscribers:manage"]' '["list:get"]' && {
  U=$NEW_USER P=$NEW_TOKEN req POST /api/ext/automations/enroll "$ENR"
  expect "enroll by a user without manage on the list -> 403" 403
}
admin GET "/api/ext/automations/$AID"
jq_ok "refused enrolls wrote no enrollment" '.data.counts.enrolled == 0'

admin POST /api/ext/automations/enroll "$ENR"
expect "enroll -> 200" 200
jq_ok "enroll -> enrolled: true" '.data.enrolled == true'
admin POST /api/ext/automations/enroll "$ENR"
jq_ok "enroll again -> enrolled: false, reason already-enrolled" '.data.enrolled == false and .data.reason == "already-enrolled"'
n=$(wait_mail "$E1" 1 60 "$CUUID")
sleep 2
n=$(mail_count "$E1" "$CUUID"); all=$(mail_count "$E1")
if [ "$n" = "1" ] && [ "$all" = "1" ]; then pass "one mail to $E1 with X-Listmonk-Campaign: step uuid"
else fail "one mail to $E1 with X-Listmonk-Campaign: step uuid" "with header: $n, all mails to the address: $all"; fi
if grep -qF "/subscription/$CUUID/" "$LM_SMTP_CAPTURE"; then pass "the mail has the step campaign's unsubscribe link"
else fail "the mail has the step campaign's unsubscribe link" "/subscription/$CUUID/ not in captured mail"; fi

# ---------- 6: import does not enroll ----------

echo "== 6: import"
BEFORE=$(enrolled_count)
E2="e2e2-$RUN@example.com"
printf 'email,name\n%s,E2E Import\n' "$E2" > "$TMP/import.csv"
PARAMS=$(jq -nc --argjson l "$L" '{mode:"subscribe", subscription_status:"confirmed", delim:",", lists:[$l], overwrite:true}')
CODE=$(curl -sS -m 60 -o "$TMP/body" -w '%{http_code}' -X POST -H "Authorization: token ${ADMIN_USER}:${ADMIN_PASS}" \
  -F "params=$PARAMS" -F "file=@$TMP/import.csv;filename=import.csv;type=text/csv" "$LM_URL/api/import/subscribers" 2>"$TMP/err") || CODE="000"
BODY=$(cat "$TMP/body")
expect "start import -> 200" 200
ST=""
for i in $(seq 1 30); do
  admin GET /api/import/subscribers
  ST=$(jget .data.status)
  [ "$ST" = "finished" ] || [ "$ST" = "failed" ] && break
  sleep 1
done
if [ "$ST" = "finished" ]; then pass "import finished"; else fail "import finished" "status: $ST"; fi
Q=$(printf "subscribers.email = '%s'" "$E2" | jq -sRr @uri)
admin GET "/api/subscribers?query=$Q"
jq_ok "imported subscriber is on the list" '.data.results[0].lists | map(.id) | index($l) != null' --argjson l "$L"
n=$(wait_mail "$E2" 1 60)
if [ "$n" = "0" ]; then pass "no mail to the imported $E2 within 60 s"; else fail "no mail to the imported $E2 within 60 s" "mails: $n"; fi
AFTER=$(enrolled_count)
if [ "$AFTER" = "$BEFORE" ]; then pass "import wrote no enrollment (enrolled $BEFORE)"; else fail "import wrote no enrollment" "before $BEFORE after $AFTER"; fi

# ---------- 7: public form ----------

echo "== 7: public form, new subscriber"
E3="e2e3-$RUN@example.com"
form "$E3" "$L_UUID"
expect "public form for $E3 -> 200" 200
n=$(wait_mail "$E3" 1 60 "$CUUID")
if [ "$n" = "1" ]; then pass "one mail to $E3 after the form"; else fail "one mail to $E3 after the form" "mails: $n"; fi
form "$E3" "$L_UUID"
expect "public form again for $E3 -> 200" 200
n=$(wait_mail "$E3" 2 45)
if [ "$n" = "1" ]; then pass "no second mail to $E3 after the repeated form"; else fail "no second mail to $E3 after the repeated form" "mails: $n"; fi

# ---------- 8: public form, existing subscriber ----------

echo "== 8: public form, existing subscriber on another list"
E4="e2e4-$RUN@example.com"
S4=$(mk_sub "$E4" "[$O]")
[ -n "$S4" ] && [ "$S4" != "null" ] || fail "create $E4 on the other list" "$BODY"
form "$E4" "$L_UUID"
expect "public form for existing $E4 -> 200" 200
n=$(wait_mail "$E4" 1 60 "$CUUID")
sleep 2
all=$(mail_count "$E4")
if [ "$n" = "1" ] && [ "$all" = "1" ]; then pass "one mail to existing $E4 after the form"; else fail "one mail to existing $E4 after the form" "with header: $n, all: $all"; fi

# ---------- 9: disable and delete ----------

echo "== 9: disable, edit, delete"
admin PUT "/api/ext/automations/$AID" "$(auto_put false)"
expect "disable -> 200" 200
man PUT "/api/campaigns/$CID" "$(camp_put 'subject=Edited while disabled')"
expect "disabled: campaigns:manage-only user edits the email -> 200" 200
admin DELETE "/api/ext/automations/$AID"
expect "delete the disabled automation -> 200" 200
admin GET "/api/campaigns/$CID"
# Upstream GET /api/campaigns/:id answers a missing campaign with 400 "Campaign not found".
if [ "$CODE" = "404" ] || { [ "$CODE" = "400" ] && printf '%s' "$BODY" | grep -qi 'not found'; }; then
  pass "the step campaign is gone (GET -> $CODE not found)"
else
  fail "the step campaign is gone" "want 404, or upstream 400 'not found'; got $CODE: $BODY"
fi
admin GET "/api/ext/automations/$AID"
expect "the automation is gone (GET -> 404)" 404

# ---------- summary ----------

echo
echo "== summary: $PASS_N passed, $FAIL_N failed"
if [ "$FAIL_N" -gt 0 ]; then
  echo "failed checks:$FAILED"
  exit 1
fi
exit 0
