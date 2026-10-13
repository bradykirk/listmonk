# Automations (welcome sequences) — Design

Status: draft for review. Branch: `hcm` (RI Listmonk fork). Builds on the Brands feature
(`docs/superpowers/specs/2026-10-11-brands-design.md`).

## 1. Goal

A new confirmed signup on a creator list gets a welcome email from Listmonk, with open, click,
unsubscribe and bounce stats per email. This replaces the welcome step in the email-infra gateway.

Version 1 needs one welcome email per creator. The data model supports more steps (email 1, wait
N days, email 2, ...) later without a redesign.

## 2. Decisions (from the user)

- D1. Only a real signup starts a sequence: a confirmed subscription that comes through the gateway
  or through the Listmonk public subscription form. Imports and manual admin adds never start one.
- D2. Each step is a hidden Listmonk campaign (approach A). Stats come from the normal campaign
  tables and pages.
- D3. The trigger is an explicit enroll call (`POST /api/ext/automations/enroll`). The gateway
  calls it where it now calls `sendWelcome`. The public form calls the same core code.
- D4. The TA postal address (`{{SenderInfo}}`) is filled in later. The email carries the marker
  `[[SENDER_INFO]]` until then, and the fork refuses to enable an automation that contains it.

## 3. Non-goals (version 1)

- No branching, conditions, tags, or triggers other than D1.
- No more than one step in the UI (the API and tables accept more; see 6.4).
- No SES Open/Click events. Listmonk tracking replaces them.
- No re-enrollment. One person gets a sequence at most once per automation, ever.
- No automation for unsubscribe, bounce, or other events.

## 4. Data model (schema `ext`)

Created by `brands.Bootstrap` style DDL at startup (`CREATE TABLE IF NOT EXISTS`), in a new
package `internal/automations`. `--install` drops schema `ext` as today.

```sql
CREATE TABLE IF NOT EXISTS ext.automations (
  id          SERIAL PRIMARY KEY,
  name        TEXT NOT NULL,
  list_id     INTEGER NOT NULL REFERENCES lists(id) ON DELETE RESTRICT,  -- a list with an automation cannot be deleted (no orphan step drafts)
  enabled     BOOLEAN NOT NULL DEFAULT false,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- At most one enabled automation per list.
CREATE UNIQUE INDEX IF NOT EXISTS automations_one_enabled_per_list
  ON ext.automations (list_id) WHERE enabled;

CREATE TABLE IF NOT EXISTS ext.automation_steps (
  automation_id INTEGER NOT NULL REFERENCES ext.automations(id) ON DELETE CASCADE,
  position      INTEGER NOT NULL CHECK (position >= 1),
  delay_minutes INTEGER NOT NULL DEFAULT 0 CHECK (delay_minutes >= 0),
  campaign_id   INTEGER NOT NULL UNIQUE REFERENCES campaigns(id) ON DELETE RESTRICT,
  PRIMARY KEY (automation_id, position)
);

CREATE TABLE IF NOT EXISTS ext.automation_enrollments (
  id            BIGSERIAL PRIMARY KEY,
  automation_id INTEGER NOT NULL REFERENCES ext.automations(id) ON DELETE CASCADE,
  subscriber_id INTEGER NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
  next_position INTEGER NOT NULL DEFAULT 1,
  next_at       TIMESTAMPTZ,            -- NULL when final
  status        TEXT NOT NULL,          -- active | sending | done | skipped | failed | unknown
  attempts      INTEGER NOT NULL DEFAULT 0,
  last_error    TEXT,
  sending_at    TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (automation_id, subscriber_id)
);
CREATE INDEX IF NOT EXISTS automation_enrollments_due
  ON ext.automation_enrollments (next_at) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS ext.automation_sends (
  enrollment_id BIGINT NOT NULL REFERENCES ext.automation_enrollments(id) ON DELETE CASCADE,
  position      INTEGER NOT NULL,
  campaign_id   INTEGER NOT NULL,
  sent_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (enrollment_id, position)
);
```

The brand of an automation is the brand of its list (`brands.BrandOfList`). It is not stored.

## 5. Hidden step campaigns

- A step campaign is a normal `campaigns` row with status `draft`, type `regular`, archive off,
  and one list: the automation's list. The upstream runner only picks `running`/`scheduled`
  campaigns, so it never sends a step campaign as a broadcast.
- "Hidden" means: the id is in `ext.automation_steps.campaign_id`.
- The fork excludes hidden campaigns from: the campaign list and counts (`GET /api/campaigns`),
  the dashboard campaign counts, and the analytics campaign picker. The direct
  `GET /api/campaigns/:id`, the campaign editor, preview, test send, and
  `/admin/campaigns/:id/analytics`-style stats stay available, so the user edits and inspects the
  email with the normal tools.
- The fork refuses (400, "automation email") for a hidden campaign: status changes
  (`PUT /api/campaigns/:id/status`), delete, and list changes away from the automation list.
  `ON DELETE RESTRICT` on `campaign_id` backs this up in the database.
- Campaign stats counters: upstream `campaigns.sent` is only advanced by the runner. The fork
  shows the step send count from `ext.automation_sends`. Views, clicks, bounces and unsubscribes
  come from the normal tables by `campaign_id`.

## 6. Behaviour

### 6.0 Active subscription

Listmonk stores a public-form subscription as `unconfirmed` on every list, also on single opt-in
lists, and still sends campaigns to it on single opt-in lists. In this spec, a subscription is
**active** when:

- its status is `confirmed`; or
- its status is `unconfirmed` and the list's `optin` is `single`.

`unsubscribed` is never active. Every rule below that says "confirmed" means "active".

### 6.1 Enroll

`core`-level function `Enroll(subscriberID, listID int) (enrolled bool, reason string, err error)`:

1. Find the enabled automation for `listID`. None → `enrolled=false, reason="no-automation"`.
2. Load the subscriber and its subscription to `listID`. Not found, blocklisted, or subscription
   not active (6.0) → `enrolled=false` with the reason.
3. `INSERT ... ON CONFLICT (automation_id, subscriber_id) DO NOTHING` with `status='active'`,
   `next_position=1`, `next_at = now() + step1.delay_minutes`. Conflict →
   `enrolled=false, reason="already-enrolled"`.

HTTP: `POST /api/ext/automations/enroll` body `{"subscriber_id": n, "list_id": n}`, permission
`subscribers:manage` plus list permission on `list_id` (same check as list subscription changes).
Response 200 `{"data": {"enrolled": bool, "reason": string}}`. Unknown subscriber or list → 404.
The endpoint is Global in the brand route table (the gateway sends no brand header); a request
that carries a brand gets 404 when the list is outside that brand.

### 6.2 Public form

- `processSubForm` (`cmd/public.go`) has two success branches: `InsertSubscriber` (new email) and,
  on a 409 conflict, `UpdateSubscriberWithLists` (existing email, for example a person on the RI
  list who now joins a TA list). Both branches enroll.
  - Before the insert or update, read the person's subscription status for each submitted list
    (none for a new email).
  - After success, call `Enroll` only for lists where the subscription was absent or not active
    before and is active now (6.0; in practice single opt-in lists). A person who re-submits the
    form for a list they were already confirmed on does not get a welcome.
  - Enroll errors are logged and never fail the form. The intent rows below recover them.

#### 6.2.1 Durable public-form trigger (intent rows)

The upstream subscription change and `Enroll` are separate database operations. A crash or a
database error between them would lose the welcome, and a later form submission cannot recover
it (the subscription is then already confirmed). The fork does not change the upstream
subscription transaction. It writes an intent first:

```sql
CREATE TABLE IF NOT EXISTS ext.automation_intents (
  id          BIGSERIAL PRIMARY KEY,
  email       TEXT NOT NULL,         -- lower-cased; the subscriber id is unknown for a new email
  list_id     INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
  was_active  BOOLEAN NOT NULL,      -- 6.0 state before the change
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

1. Before `InsertSubscriber` / `UpdateSubscriberWithLists` / `ConfirmSubscriptionOptin`, insert
   one intent per submitted list that has an enabled automation, with the status before the
   change. A failed intent insert is logged and the form continues (the welcome is then
   best-effort for this one submission).
2. After the change succeeds, run the 6.2 rule (`Enroll` if not confirmed before and confirmed
   now) and delete the intent rows in the same transaction as the enrollment insert.
3. The automation worker sweeps intents older than 2 minutes on every tick: look up the
   subscriber by email and the subscription. Active now and `was_active=false` →
   `Enroll` (the unique key makes a repeat harmless). Then delete the intent. Not active (the
   upstream change failed or the person has not clicked the opt-in link yet) → delete intents
   older than 7 days only, so a late opt-in click within 7 days is still recovered.
4. Double opt-in: the form's intent stays unresolved (the subscription is not active until the
   click) and expires after 7 days; `confirmOptinSubscription` writes its own intent for the
   confirm step.

The gateway path needs no intent rows: the gateway keeps its own durable event table and retries
the enroll call until it gets an answer.
- `confirmOptinSubscription` (`cmd/public.go`): read the statuses before, and after
  `ConfirmSubscriptionOptin` succeeds, call `Enroll` for each list whose subscription changed from
  not active to active (same rule as above; in practice double opt-in lists). A second click on the confirm link enrolls
  nobody.
- Imports, `POST /api/subscribers`, `PUT /api/subscribers/lists`, and the admin UI never call
  `Enroll` (D1).

### 6.3 Worker

A goroutine in the Listmonk process, started after the manager, tick 30 s, batch 100.

```
SELECT ... FROM ext.automation_enrollments
 WHERE status='active' AND next_at <= now()
 ORDER BY next_at LIMIT 100 FOR UPDATE SKIP LOCKED
```

Per enrollment, in order:

1. Automation disabled or deleted, or step at `next_position` missing → `skipped`
   ("automation disabled" / "no step").
2. Subscriber blocklisted, or subscription to the automation list
   not active (6.0: unsubscribed, removed, or still unconfirmed on a double opt-in list) →
   `skipped` with the reason.
3. Brand send guard (`brands.Check` rules 1–4) on the step campaign. Failure → `skipped` with the
   guard reason, and one log line `automations: guard blocked step <automation>/<pos>: <reason>`.
   (A guard failure is a configuration error. It does not retry.)
3a. Content check on the rendered message (subject, HTML body, alt body, after the effective
   template is applied): if it contains `[[SENDER_INFO]]`, do not send. The enrollment stays
   `active` with `next_at = now() + 10 min`, `last_error = "sender info marker"`, no attempt is
   counted, and the log line `automations: marker in step <automation>/<pos>` repeats at most once
   per hour per step. The email goes out after the user fixes the content.
4. Set `status='sending', sending_at=now(), attempts=attempts+1`, commit.
5. Load the campaign, compile its template, `manager.NewCampaignMessage(campaign, subscriber)`,
   then `manager.SendCampaignMessage(msg)` (new fork method, 6.3.1). The call returns only after
   the messenger's `Push` returns.
6. `Push` returned nil (the messenger accepted the email) → insert `ext.automation_sends`; then
   next step exists → `status='active', next_position+1, next_at=now()+next.delay_minutes,
   attempts=0`; else `status='done', next_at=NULL`.
7. Error in step 5 (render error or `Push` error) → `status='active'`, `last_error`,
   `next_at = now() + backoff(attempts)` (1 min, 5 min, 30 min, 2 h); after 5 attempts →
   `failed`.

Crash recovery: at worker start and every tick, enrollments in `sending` for more than 10 minutes
become `unknown` and are not retried. `sending` covers only the window between step 4 and the
step 6/7 update, so `unknown` means "the process stopped during a send; the email may or may not
have gone out". A lost email is better than a duplicate.

#### 6.3.1 Synchronous send (fork change in `internal/manager`)

`PushCampaignMessage` only puts the message on the manager queue. The manager worker logs a
messenger error and drops it for a message without a campaign pipe, so the caller never learns
the result. The automation worker therefore does not use it.

- Move the code in `worker()` that builds `models.Message` and its headers from a
  `CampaignMessage` into a helper `campaignOut(msg CampaignMessage) models.Message`. `worker()`
  calls the helper (no behaviour change for broadcasts).
- Add `func (m *Manager) SendCampaignMessage(msg CampaignMessage) error`: attach media, build the
  message with `campaignOut`, look up `m.messengers[msg.Campaign.Messenger]` (unknown messenger →
  error), and return the result of `Push`.
- The message carries the normal headers (`X-Listmonk-Campaign`, `X-Listmonk-Subscriber`,
  `List-Unsubscribe`), so SES bounces and complaints that the gateway reports map to the step
  campaign as for broadcasts.
- The automation worker sends one message at a time and does not use the manager's message rate
  limit. Welcome volume is low (signups per minute), far below the SES rate.
- Error class: every `Push` error is retried (step 7). With the SMTP messenger, an error after the
  server accepted the DATA (for example a timeout on the final reply) can cause a duplicate on
  retry. This case is rare and accepted; it is the same trade-off as upstream test sends.

### 6.4 Enable rules

`PUT /api/ext/automations/:id` with `enabled=true` is refused (400, with the reason) when:

- the automation has no steps;
- any step campaign body, subject, or preview text contains `[[SENDER_INFO]]` (D4);
- any step campaign fails the brand send guard for the automation list.

Steps: version 1 UI shows and edits only step 1. The API accepts positions 1..n.

### 6.5 Permissions for sending

Upstream broadcasts need `campaigns:send` to start. An automation sends real email, so:

- `enabled=true` on `PUT /api/ext/automations/:id` needs `campaigns:send` in addition to
  `campaigns:manage` (403 otherwise). Disabling needs only `campaigns:manage`.
- `PUT /api/campaigns/:id` on a hidden campaign whose automation is enabled needs
  `campaigns:send` (403 otherwise). The fork also refuses (400) such an update when the new
  subject, body, alt body, or preview text contains `[[SENDER_INFO]]`, or when the new template,
  messenger, or from address fails the brand send guard.
- A user without `campaigns:send` can still edit the email of a disabled automation.

## 7. API (`/api/ext/automations`, permission `campaigns:get` / `campaigns:manage`)

| Method | Path | Notes |
|---|---|---|
| GET | `/api/ext/automations` | list with `brand`, `list`, `enabled`, `enrolled`, `sent`, `active`, `failed`, `unknown` counts. Brand view: only automations whose list is in the brand. |
| POST | `/api/ext/automations` | `{name, list_id}`. Creates the automation (disabled) and step 1 with a new hidden campaign (subject "Welcome", body empty, template default, messenger/from from the request or the brand's first messenger and domain). |
| GET | `/api/ext/automations/:id` | automation, steps (with campaign summary and stats), counts. |
| PUT | `/api/ext/automations/:id` | `{name, enabled, steps:[{position, delay_minutes}]}`. List change refused once any enrollment exists. `enabled=true` also needs `campaigns:send` (6.5). |
| DELETE | `/api/ext/automations/:id` | refused while `enabled`. Deletes enrollments and steps, then the hidden campaigns. |
| POST | `/api/ext/automations/enroll` | see 6.1. |

Brand rules follow the Brands spec: in a brand view, an automation whose list is outside the
brand returns 404, and create requires the list to be in the brand.

## 8. UI

- Navigation: "Automations" between Campaigns and Users. Follows the brand switcher.
- List page: name, list, brand tag, enabled toggle (calls PUT; shows the refusal message),
  enrolled / sent / failed counts.
- Edit page: name, list (locked after the first enrollment), step 1 row with delay (minutes,
  shown as "send immediately" for 0), "Edit email" (opens `/admin/campaigns/:id`), "Stats"
  (opens the campaign analytics for the id), and the step send count.
- STE-style short help text: "Only new signups from the website form or the signup gateway start
  this automation. Imports do not."

## 9. TA welcome conversion (one-time, done by Claude)

Source: `/Users/brady/HCM/tacticaladvisor/emails/welcome.html`.

| EmailOctopus | Listmonk |
|---|---|
| `{{PreviewText}}` (if present) | campaign `preview_text` (the fork's auto preheader inserts it) |
| `{{UnsubscribeURL}}` | `{{ UnsubscribeURL }}` |
| `{{WebVersionURL}}` | `{{ MessageURL }}` |
| `{{SenderInfo}}` | `[[SENDER_INFO]]` (D4) |
| `<a href="{{RewardsURL}}">Powered by EmailOctopus</a>` | removed |

The converted HTML is the step campaign body (content type HTML, a pass-through template with
`{{ template "content" . }}` only, owned by brand `ta`). Messenger `email-ta`, from address on
`news.tacticaladvisor.net`. A check script fails if any `{{[A-Za-z]+}}` EmailOctopus tag or the
text "EmailOctopus" remains.

## 10. Gateway change (email-infra) and rollout

1. Deploy the fork (`hcm`, auto-deploys on push). No automation exists, so nothing changes.
2. On RI: create an automation on the "RI Test - Owner" list, enable it, sign up a test address
   through the gateway path, and confirm the send, the stats, and no second send on a repeat.
3. email-infra: replace `ports.sendWelcome(...)` with `ports.enrollAutomation(subscriberId,
   listId)` in `signups.ts`. Map `enrolled=true` → `welcomeStatus='done'`,
   `enrolled=false` → `'skipped'` with the reason, HTTP error → the current retry path. Remove
   `welcomeTemplateId` once all creators use automations. The gateway keeps its own dedupe,
   so a re-signup after unsubscribe never calls enroll for a person the gateway rejects.
4. TA goes live after AWS account B, the `email-ta` messenger, and the TA lists exist, and after
   `[[SENDER_INFO]]` is replaced.

## 11. Testing

- Go unit tests: enroll rules (no automation, not confirmed, blocklisted, duplicate), worker state
  machine (success, retry/backoff, max attempts, guard skip, unsubscribed skip, stale `sending` →
  `unknown`), enable rules (no steps, marker, guard).
- Integration against the local test Postgres (`lm-brands-pg`, throwaway DB): one enroll → one
  SMTP message in the capture sink with the tracked links; a second enroll → no message; an import
  of the same list → no enrollment; a public-form signup on a single opt-in list → one message;
  a double opt-in signup → message only after confirm; an existing subscriber (on another list)
  who joins the automation list through the public form → one message; a confirmed subscriber who
  re-submits the form for the same list → no message.
- Permissions: a user with `campaigns:manage` but no `campaigns:send` gets 403 on enable and on
  an edit of an enabled automation's email; the same user can edit a disabled one.
- Marker: an edit that adds `[[SENDER_INFO]]` to an enabled automation's email → 400; a marker in
  the template (not the body) → the worker does not send and the enrollment stays `active`.
- Intent recovery: an intent row with a confirmed subscription and no enrollment (simulated crash
  after the subscription change) → the sweep enrolls once; an intent for a subscription that is
  still unconfirmed → no enrollment; a later opt-in click within 7 days → one enrollment.
- Messenger failure: a test messenger whose `Push` fails → the enrollment stays `active` with
  `last_error` and no `automation_sends` row; after 5 failures → `failed`. A `Push` that succeeds
  → exactly one `automation_sends` row. An enrollment left in `sending` (simulated crash) →
  `unknown` after 10 minutes and no send.
- e2e script `scripts/automations-e2e.sh` in the style of `brands-e2e.sh`.
- The existing `brands-e2e.sh` still passes (hidden campaigns do not appear in campaign lists).

## 12. Risks

- R1. Hidden campaigns leak into an upstream view the fork did not filter. Mitigation: the route
  table check from Brands classifies every route; a hidden campaign in a list view is only
  cosmetic, because status changes are refused.
- R2. A slow SMTP server slows the automation worker (synchronous `Push`). The worker runs in its
  own goroutine and does not share the broadcast queue, so broadcasts are not affected; only
  welcome latency grows.
- R3. Crash between `Push` and the `done` update gives `unknown` with no retry (by design).
- R4. Upstream merges touch `processSubForm`, `confirmOptinSubscription`, and the manager
  `worker()` (the `campaignOut` extraction). The hunks are marked `// FORK(hcm):` and listed in
  `PATCHES.md`.
