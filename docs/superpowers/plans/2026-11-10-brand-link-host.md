# Per-brand link host — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan
> task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** campaign emails of a brand that has a `link_host` carry every Listmonk-generated URL (unsubscribe,
manage, view in browser, tracking, opt-in, archive, `RootURL`, and the `List-Unsubscribe` header) on that host,
for example `https://news.tacticaladvisor.net`, instead of the instance root URL `https://news.rangeinsiders.com`.
Brands without a `link_host` (RI, CT for now) do not change.

**Architecture:** one instance, one DB. A `link_host` column on `ext.brands`. The manager calls an injected
resolver (`brands.Service.LinkHostFor`) once per campaign compile; it reads the brands fresh (no cache), takes the
brand from the campaign's lists exactly like the guard, checks the campaign's messenger is one of that brand's
messengers, and fails closed on any mismatch or read error. The manager builds a per-campaign URL set by
replacing the root prefix of the six startup format strings. `TemplateFuncs(c)` and `NewCampaignMessage` use that set. The TA hostname points
at the same container (Wix A record + a second Coolify domain on `ri-listmonk`); the public handlers work from
UUIDs only and read no Host.

**Tech stack:** Go (listmonk v6.2.0 fork, branch `hcm`), Postgres `ext` schema, Vue admin (brand form).

**Owner decision (2026-11-10):** per-brand link domain before the first TA send (rejects the shared-domain
option D8 of `~/email-infra/docs/plans/2026-11-06-ta-switch-from-emailoctopus.md` for TA).

## Global constraints

- `hcm` pushes auto-deploy RI production (`ri-listmonk`). Claude does not push `hcm`; the owner pushes.
- No upstream table changes. The column goes on the fork table `ext.brands` (`ALTER TABLE … ADD COLUMN IF NOT
  EXISTS` in `internal/brands/schema.sql`, run by `Bootstrap`).
- Changed upstream lines carry a `FORK(hcm)` comment; `PATCHES.md` gets a section with the modified-file table.
- With every `link_host` empty, output must be byte-identical to today (regression test).
- RI warmup is live. Nothing in this patch may change an RI email.

## Design decisions

- **D1 Brand by lists, verified by messenger, fail closed (Codex round 1 #2, round 2 #1).** `LinkHostFor(c)`
  fetches the campaign's list IDs by campaign ID (`q.ExtCampaignListIDs`, the query the guard's `g.lists(c.ID)`
  uses; `models.Campaign.Lists` is empty on the `NextCampaigns` path), loads brands and list tags with the same
  `loadAll` the guard uses, resolves `CampaignBrand(listIDs)`, and returns the
  brand's `link_host` only if `c.Messenger ∈ brand.messengers`. A list set with no brand → root URL. A brand with
  a `link_host` but a messenger outside its `messengers` (for example a fallback to `email`, or a messenger
  reassigned between the guard read and this read) → error: the campaign does not compile and nothing is sent
  (the pipe errors, the campaign pauses). A DB read error → error, same result. Never a silent fall back to RI
  URLs for a brand that has a `link_host`. `manager` cannot import `brands` (import cycle), so the resolver is a
  `func(*models.Campaign) (string, error)` field on `manager.Config`, wired in `cmd/init.go`.
- **D2 Format strings by prefix swap.** `urlsFor(root)` = the six `cmd/init.go:459-485` formats with the
  instance root replaced by `link_host`. One function builds both the default set (startup) and overrides, so a
  future upstream URL shape stays in one place.
- **D3 No cache.** One brands read per campaign compile (`newPipe`) and per test send / automation message
  (`TemplateFuncs(c)`), which is the guard's own cost today. No stale index exists to disagree with the guard.
- **D4 Validation proves this instance (Codex round 1 #1, round 2 #3).** `link_host` is `https://` + lowercase
  host, no path, no port, no trailing slash. Brand PUT/POST with a new `link_host` creates two independent random
  32-byte tokens, a lookup token and a response token, and keeps the pair in process memory for 30 s. It fetches
  `https://<host>/ext/link-host-check/<lookup>` (5 s timeout, redirects refused, TLS verified). The public route
  answers 200 with the paired response token only when the lookup token is live in this process; any other
  instance answers 404, and a server that reflects the request cannot know the response token. The save is refused unless the answer matches. GET and the one-click
  POST on real subscription UUIDs are proven in rollout step 5 with a seed subscriber of this DB. Clearing
  `link_host` needs no check.
- **D5 Scope of override.** Campaign runs, test sends, automations, previews and the public campaign view all go
  through `TemplateFuncs(c)`, so they all follow. Not overridden: transactional messages (no URL funcs), the
  opt-in notification (`makeOptinNotifyHook`, global `email` messenger; TA uses the gateway, not Listmonk opt-in),
  admin system mails, public page chrome (`.RootURL` in `static/public/templates/*` for archive/subscribe/altcha;
  cosmetic, kept on RI).
- **D6 Frozen per pipe; rollback never pauses a running campaign (Codex round 1 #4, round 2 #2).** The URL set
  is computed when `newPipe` compiles the campaign and kept for that pipe. A `link_host` change applies to
  campaigns compiled after it. Pause → resume is not a rollback tool: upstream `StopCampaign` returns before the
  pipe ends, and a quick resume can skip recipients whose batch was fetched. Because D7 keeps every used host
  routed, an active campaign's links stay valid, so the rollback is: let the active campaign finish (a TA send to
  ~2.3K takes minutes), then clear `link_host`. If the TA host itself fails (DNS/certificate), the fix is to
  restore the routing, not to change URLs; a campaign that must stop is cancelled, not resumed.
- **D7 A used host stays routed for good.** Every host that ever appeared in delivered mail must keep its DNS,
  its Coolify domain and a valid certificate as long as those emails can be clicked (unsubscribe links must keep
  working; treat it as permanent). Clearing `link_host` stops new use only. Recorded in `PATCHES.md` and the
  email-infra runbook.
- **D8 Deploys only with no campaign running (Codex round 1 #3).** Listmonk advances `last_subscriber_id` when it
  fetches a batch, before delivery, and `Close` does not drain the queue, so a restart mid-send can skip
  recipients. Every restart in this rollout (Coolify domain add, `hcm` push) happens outside the RI send window
  (weekdays 09:00–16:00 ET) and only after the admin Campaigns page shows no `running` campaign and the email-infra
  report shows no send in flight. Checkpoints (`sent`/`to_send` of the last campaigns) are written down before
  and compared after.

## Review focus (inputs no task test covers by default)

1. A campaign whose messenger fell back to `email` (`validateCampaignFields`) while its lists are TA (brand has
   a `link_host`): resolver error, nothing sent, never RI URLs. Test in Task 2.
2. `link_host` set while the host is not routed, or routed to another Listmonk: refused by D4 (nonce). Test in
   Task 3.
3. `List-Unsubscribe` header and the HTML/plain body unsubscribe link must be the same host. Test in Task 2.
4. Trailing slash or uppercase in the saved host: normalized or refused, never `//subscription`. Test in Task 3.
5. All brands empty: byte-identical RI output. Test in Task 2.
6. Messenger reassigned to another brand, or the brands read fails: resolver error, campaign not sent. Test in
   Task 1/2.
7. Active campaign, `link_host` changed: unchanged until pause/resume, then the new host. Test in Task 2.

---

### Task 1: `link_host` column and brand index

**Files:** Modify `internal/brands/schema.sql`, `internal/brands/store.go` (struct + queries), add
`internal/brands/linkhost.go` (+ `_test.go`).

**Interfaces — Produces:** `Brand.LinkHost string` (`db:"link_host" json:"link_host"`);
`func NormalizeLinkHost(s string) (string, error)`;
`func (s *Service) LinkHostFor(c *models.Campaign) (string, error)` ("" = no override; error = do not send).

- [ ] Test: `NormalizeLinkHost` accepts `https://News.TacticalAdvisor.net/` → `https://news.tacticaladvisor.net`;
  refuses `http://…`, a path, a port, empty host, `javascript:`; `""` → `""`.
- [ ] Test (`s.load` stub): TA lists + `email-ta` → TA host; RI lists + `email` → `""`; TA lists + `email` with
  TA `link_host` set → error; TA lists + `email` with TA `link_host` empty → `""`; mixed-brand lists → error;
  load error → error.
- [ ] Implement; `ALTER TABLE ext.brands ADD COLUMN IF NOT EXISTS link_host TEXT NOT NULL DEFAULT ''`.
- [ ] `go test ./internal/brands/...` passes (DB tests with `LISTMONK_TEST_DSN`). Commit.

### Task 2: manager uses per-campaign URLs

**Files:** Modify `internal/manager/manager.go` (Config + `TemplateFuncs` + `campaignOut`), `message.go`
(`NewCampaignMessage`), `pipe.go` (`newPipe` resolves once); `cmd/init.go` (`urlsFor`, pass the index provider).
Test `internal/manager/linkhost_fork_test.go`.

**Interfaces — Consumes:** `Config.LinkHostFor func(*models.Campaign) (string, error)` (nil = no override). **Produces:** `func (m *Manager) urlsFor(c
*models.Campaign) URLSet` with fields `Unsub, Optin, LinkTrack, Message, Archive, ViewTrack, Root`.

- [ ] Test: campaign with messenger `email-ta`, index TA → rendered body `UnsubscribeURL`, `ManageURL`,
  `MessageURL`, `TrackLink`, `TrackView`, `OptinURL`, `ArchiveURL`, `RootURL` and the `List-Unsubscribe`
  header all start with `https://news.tacticaladvisor.net/`; none contains `news.rangeinsiders.com`.
- [ ] Test: messenger `email` (RI) and messenger `email-ta` with an empty index → output byte-identical to the
  pre-patch golden render (golden captured before the change in this task's first step).
- [ ] Test: `SendCampaignMessage` (automation path) and a test send follow the override.
- [ ] Test: resolver error → `newPipe` returns an error and no message is pushed; `TemplateFuncs`/test send
  returns the error.
- [ ] Test: a pipe built before a host change keeps the old host; a new pipe gets the new one.
- [ ] Test through the real path: a campaign fetched by `NextCampaigns` (empty `Lists`) on TA lists resolves the
  TA host (DB test with `LISTMONK_TEST_DSN`).
- [ ] Implement with `FORK(hcm)` comments. Commit.

### Task 3: brand API and admin form

**Files:** Modify `cmd/brands.go` (PUT/POST validate + nonce check), `cmd/handlers.go` (public route
`GET /ext/link-host-check/:nonce`, `FORK(hcm)`), the brand form in
`frontend/src/views/` (one text field "Link host"), `i18n/en.json` key. Test `cmd/brands_linkhost_test.go`
(handler test with an `httptest` server standing in for the health check).

- [ ] Test: PUT with a host whose check route is served by this handler → saved; a server that reflects the
  lookup token, returns 200 with another body, 404, a 30x redirect, a timeout, or a bad certificate → 400 with a
  clear message; an expired lookup token → 404; normalization from Task 1 applied.
- [ ] Implement; `scripts/brands-frontend-check.sh` passes. Commit.

### Task 4: e2e and docs

- [ ] Extend `scripts/brands-e2e.sh`: set `link_host` for `ta` against a local second hostname, send a test to a
  sink, assert body and header host; unsubscribe through the TA host (GET page, one-click POST) marks the
  subscription unsubscribed.
- [ ] `PATCHES.md` section "Per-brand link host" (why, behaviour, new/modified file tables, tests).
- [ ] Commit. Codex review of the diff (max 2 rounds).

### Task 5: rollout (owner + Claude)

0. Before each restart below: D8 (outside 09:00–16:00 ET weekdays, no `running` campaign, checkpoints noted).
1. Owner (Wix DNS for `tacticaladvisor.net`): A record `news` → `204.168.237.186` (same IP as
   `news.rangeinsiders.com`). Pass: `dig +short A news.tacticaladvisor.net`.
2. Owner (Coolify `ri-listmonk`): add `https://news.tacticaladvisor.net` to the domains; Redeploy. Pass:
   `https://news.tacticaladvisor.net/health` 200 with a valid certificate; `news.rangeinsiders.com` unchanged.
3. Owner: push `hcm` (auto-deploys). Pass: RI report `GET /api/report/cold` healthy; an RI test send to the RI
   owner list still shows `news.rangeinsiders.com` links (byte check against a pre-deploy test send).
4. Claude (admin UI): set brand `ta` `link_host` = `https://news.tacticaladvisor.net`.
5. Claude: TA test send to seed list 66 (this becomes rollout step 6 of the TA plan). Pass: every link and the
   `List-Unsubscribe` header on the TA host; one-click unsubscribe from Gmail works; DKIM `d=news.tacticaladvisor.net`.
6. Rollback: let any active TA campaign finish, then clear `link_host` (D6). The TA host stays routed (D7). Code
   rollback: revert the `hcm` commits (an empty `link_host` column is ignored by old code) under D8.

## Not in scope

- Per-brand opt-in emails, public page chrome, archive per brand, CT's `link_host` (set later the same way after
  its own host is routed).

## Review log

- Codex adversarial review round 1 (2026-11-10): 4 high findings (health check not instance-specific; stale
  messenger cache; restart during live RI sends; rollback). All accepted: D1, D3, D4, D6–D8.
- Round 2: broadcast campaigns carry no `Lists` (D1 fetches list IDs by campaign ID); pause → resume can skip
  recipients (D6 drops it); a reflecting server passes an echoed nonce (D4 uses a separate response token). All
  accepted. Two-round limit reached; these round-2 fixes are not re-reviewed by Codex. The diff review after the
  build covers them.
