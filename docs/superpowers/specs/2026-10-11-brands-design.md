# Brands in Listmonk — design

**Date:** 2026-10-11
**Branch:** `hcm` (from `origin/gunmade` at `9ab53051`)
**Instance:** RangeInsiders Listmonk, `news.rangeinsiders.com` (Coolify project `rangeinsiders-email`)
**Sub-project:** 1 of 2. Sub-project 2 (automations / welcome sequences) has its own spec and depends on this one.
**Parent plan:** `tacticaladvisor/docs/superpowers/plans/2026-10-07-listmonk-multi-creator-migration.md`

## 1. Goal

One Listmonk instance holds several businesses ("brands"): RangeInsiders (RI), Tactical Advisor (TA),
CT, and later others. An admin selects a brand in the top bar, and the admin UI then shows only that
brand's lists, subscribers, campaigns, templates, media, bounces, and numbers. A server-side send guard
blocks any campaign that mixes brands.

### Success criteria

1. With a brand selected, every page in section 5 shows only that brand's items, with correct counts and
   pagination.
2. With "All brands" selected, or with no brand header (API clients), Listmonk behaves as it does today.
3. The send guard blocks every campaign in section 6, on every path that starts a send.
4. The Gun Made Listmonk (`lists.gunmade.com`, branch `gunmade`) gets no code from this work.
5. A rollback to the `gunmade` branch needs no data change.

## 2. Decisions (from the design discussion)

| # | Decision | Reason |
|---|---|---|
| D1 | Build in the Listmonk fork, not in email-infra. | The team works in Listmonk. |
| D2 | The switcher is a **view filter for admins only**. No brand-limited logins. | Nobody needs a brand-limited login now. Security boundary is out of scope. |
| D3 | The send guard **blocks**. No override. | A cross-brand send has no valid use and cannot be reversed. It also runs for scheduled and API sends, where nobody sees a warning. |
| D4 | A separate branch `hcm` for the RI instance. `gunmade` stays unchanged. | Both instances build from `gunmade` today. Gun Made must not get this code. |
| D5 | New tables go in a new Postgres schema `ext`. No upstream table changes. | Keeps the fork rule in `PATCHES.md` ("never change the database schema") in intent: upstream migrations never conflict. |
| D6 | The brand filter for lists and subscribers works through a **scoped copy of the user** (5.2). Campaigns get their own brand predicate (5.3). | Upstream filters lists and subscribers by the user's list permissions in several places, so a scoped user covers all of them. Upstream campaign checks skip list permissions for `campaigns:*_all` and accept a campaign when any one list matches, so campaigns need their own check (Codex adversarial review, round 1). |
| D8 | Subscriber delete and blocklist are **global actions**. With a brand selected, the API refuses them. | One subscriber record is shared by all brands. A delete or blocklist from the TA view would also remove the person from RI (Codex adversarial review, round 1). |
| D7 | The creator gateway stays in email-infra for intake and SES feedback only. Welcome sends move to Listmonk in sub-project 2. | The gateway is built and live-tested. The automation trigger does not depend on it. |

## 3. Out of scope

- Brand-limited users and per-brand permissions (D2).
- Automations / welcome sequences (sub-project 2).
- Per-brand settings, SMTP servers, users, roles, logs, and maintenance. These stay global.
- Separate subscriber records per brand. One email address stays one subscriber.
- The Listmonk v7 server-side admin UI. This design targets the v6 Vue UI. Section 10 lists the v7 cost.

## 4. Data model (schema `ext`)

The fork creates the schema and tables at startup with `CREATE SCHEMA IF NOT EXISTS` and
`CREATE TABLE IF NOT EXISTS`, inside one transaction. It does not use the upstream migration system.

```sql
CREATE TABLE ext.brands (
  slug          TEXT PRIMARY KEY CHECK (slug ~ '^[a-z][a-z0-9-]{1,30}$'),
  name          TEXT NOT NULL,
  list_tags     TEXT[] NOT NULL,  -- a list with any of these tags belongs to the brand
  messengers    TEXT[] NOT NULL,  -- messengers a campaign of this brand may use
  from_domains  TEXT[] NOT NULL,  -- from-address domain must equal one of these or be a subdomain of one
  sort_order    INT NOT NULL DEFAULT 0,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Ownership of items that have no tags in upstream. brand_slug NULL = shared (visible in every brand).
CREATE TABLE ext.template_brands (
  template_id INT PRIMARY KEY REFERENCES templates(id) ON DELETE CASCADE,
  brand_slug  TEXT REFERENCES ext.brands(slug) ON UPDATE CASCADE
);
CREATE TABLE ext.media_brands (
  media_id    INT PRIMARY KEY REFERENCES media(id) ON DELETE CASCADE,
  brand_slug  TEXT REFERENCES ext.brands(slug) ON UPDATE CASCADE
);
```

### 4.1 List ownership comes from list tags

- A list belongs to brand B when its tags contain one of `B.list_tags`.
- A list whose tags match two brands is **ambiguous**. It belongs to no brand, the UI shows it with an
  "Ambiguous" label in "All brands", and the send guard blocks it.
- A list with no matching tag is **unassigned**. It shows only in "All brands", with an "Unassigned" label.
- Reason for tags, not a table: email-infra creates a new `ri-cold` list each day through the API, and
  `creatorAdmin` already requires the slug tag on creator lists. Tags keep those lists owned with no
  extra call.
- The "Brand" field on the list form sets or replaces the brand tag. It does not use a separate table.

### 4.2 Campaign ownership comes from its lists

- A campaign's brand is the one brand of all its lists.
- A campaign with no lists, with lists of two brands, or with an unassigned or ambiguous list has no
  brand. It shows only in "All brands", and the send guard blocks it.

### 4.3 Templates and media

- A row in `ext.template_brands` / `ext.media_brands` with a brand: the item belongs to that brand.
- A row with `brand_slug NULL`: the item is **shared** and shows in every brand.
- No row: the item is **unassigned** and shows only in "All brands".
- A template or media file created with a brand selected gets a row for that brand in the same request.

### 4.4 First records (seed, run once, idempotent)

| Brand | list_tags | messengers | from_domains |
|---|---|---|---|
| `ri` | `ri`, `ri-cold` | `email-ri` | `rangeinsiders.com` |
| `ta` | `ta` | `email-ta` | `news.tacticaladvisor.net` |
| `ct` | `ct` | `email-ct` | (set when CT's sending domain exists) |

Notes:
- email-infra creates RI campaigns with messenger `email-ri` (its default for `LISTMONK_MESSENGER`) and a
  from address on `rangeinsiders.com`. The dry run (section 8) confirms this before the deploy.
- The `ta` messenger does not exist yet. The guard blocks TA sends until it exists. This is correct.
- Templates seen on 2026-10-11: 1 Default campaign, 2 Default archive, 3 Sample transactional,
  4 Sample visual, 5 RangeInsiders Issue (pass-through), 6 RI Preview. Proposed: 1–4 shared, 5–6 `ri`.
  The admin confirms before the seed runs.
- Media count on 2026-10-11: 0.

## 5. Brand scope per request

### 5.1 Transport

- Each browser tab has its own brand. The tab keeps it in memory and in `sessionStorage` (key `ext.brand`).
  `localStorage` holds the last choice and sets only the start brand of a new tab. A choice in one tab
  does not change another open tab (plan review round 2).
- The Vue app sends the tab's brand on every API call in the header `X-Listmonk-Brand: <slug>`. "All
  brands" sends no header. Requests that cannot carry a header (downloads, iframe `src`, form posts into
  an iframe) send the query parameter `ext_brand=<slug>`. A header and a parameter with different values
  → `400`.
- A Go middleware reads the header after authentication:
  - no header → no scope (today's behavior)
  - a known slug → the request scope gets the brand and its list IDs
  - an unknown slug → `400 unknown brand`
- The middleware resolves list IDs once per request from `lists.tags` and `ext.brands.list_tags`
  (ambiguous lists excluded).

### 5.2 Scoped user (lists and subscribers)

Upstream checks list access in several places: `GetPermittedLists`, `GetPermittedListIDs`,
`FilterListsByPerm`, `HasListPerm`, `HasPerm(lists:*_all | subscribers:get_all)`, and the super-admin role
check. A hook in one function misses the others. So the middleware does not change any upstream function.
It replaces the user in the request context with a **scoped copy**:

- The role is no longer the super-admin role, and the copy has none of the blanket permissions
  `lists:get_all`, `lists:manage_all`, `subscribers:get_all`, `campaigns:get_all`, `campaigns:manage_all`.
- The copy's list permissions are the brand's list IDs **intersected** with the real user's own access,
  for read and manage separately:
  - read lists = brand lists ∩ (all lists if the real user is super admin or has `lists:get_all`, else
    the real user's read lists)
  - manage lists = brand lists ∩ (all lists if the real user is super admin or has `lists:manage_all`,
    else the real user's manage lists)
  - the list permission map holds exactly these two sets.
- **The copy only narrows.** It never adds a permission, a list, or a list right that the real user does
  not have. A brand header can hide data. It can never give access.
- All other permissions of the real user stay (for example `subscribers:manage`, `campaigns:manage`,
  `templates:get`).
- The copy is built per request from the real user. The cached user object is never changed.
- The campaign predicate (5.3) and the refusals (5.4) are extra conditions on top of the upstream
  permission checks. They never replace or skip an upstream check.

**Route allowlist.** The middleware applies the scope only to the scoped routes in 5.4 and 5.5. On every
other route (settings, users, roles, SMTP, logs, maintenance, import) it ignores the header, so the
scoped copy cannot block an admin from global pages.

### 5.3 Campaign brand predicate

Upstream campaign access does not depend on list permissions for `campaigns:*_all` users, and
`CampaignHasLists` accepts a campaign when **any** list matches. So campaigns get an independent check,
used on every scoped campaign route, whatever the user's permissions:

- **List and count** (`GET /api/campaigns`): only campaigns whose brand (4.2) is the selected brand.
  Totals and pagination must count the same set. A mixed-brand or list-less campaign never shows in a
  brand view.
- **By id** (get, preview, update, status, test send, content conversion, archive, analytics, delete):
  `404` when the campaign's brand is not the selected brand.
- **Delete by query and bulk actions:** only campaigns of the selected brand.
- **Create:** the request's lists must all belong to the selected brand, else `400`.

The plan chooses the hook (a wrapper handler in a new file, or an optional parameter on the upstream
campaign query). Each changed upstream line is recorded in `PATCHES.md`.

### 5.4 Subscriber actions

| Action | With a brand selected |
|---|---|
| List, query, get, export | Scoped user: only subscribers with a subscription on a brand list. The detail view shows only brand lists. |
| Add to lists, remove from lists, unsubscribe from lists (single and bulk) | Scoped user: only brand lists. Other brands' subscriptions do not change. |
| Update (`PUT` and `PATCH /api/subscribers/:id`) | List changes touch only brand lists; an empty manage set inside the brand deletes no subscription. Name, email, and attributes are shared, so the form shows the note "Shared across all brands". A status change is refused: `400 Status change is a global action. Select All brands.` (plan review round 1). |
| Activity tab, single export (`GET /api/subscribers/:id/activity`, `/export`) | Only the brand's subscriptions, and only views and clicks on brand campaigns. The profile is shared (plan review round 2). |
| Delete (single, bulk, by query) | Refused: `400 Delete is a global action. Select All brands.` |
| Blocklist (single, bulk, by query) | Refused: `400 Blocklist is a global action. Select All brands.` |
| Create | Allowed. The lists must all be brand lists, else `400`. If the email already exists, upstream rules apply, and only brand subscriptions change. |

The UI hides the Delete and Blocklist buttons in a brand view and shows a short note why.

### 5.5 Pages

| Page / API | With a brand selected |
|---|---|
| Lists | Scoped user (5.2) |
| Subscribers | Scoped user (5.2) and the rules in 5.4 |
| Campaigns | Campaign brand predicate (5.3) |
| Templates | New filter: brand's templates + shared |
| Media | New filter: brand's media + shared |
| Bounces | New filter: bounces whose campaign belongs to the brand, or whose subscriber is on a brand list when the bounce has no campaign |
| Dashboard (upstream counts and charts) | New filter: brand lists only |
| Fork pages: Analytics, Campaign report, Dashboard growth | New filter: brand lists and brand campaigns only |
| Settings, Users, Roles, SMTP, Logs, Maintenance, Import | No change (global) |

The plan lists every route in `cmd/handlers.go` and marks it scoped, global (header ignored), or refused in
a brand view. The integration test covers every scoped and refused route, because a missed route acts on
all brands.

### 5.6 Create and assign

- Create list / template / media with a brand selected → owned by that brand.
- Create in "All brands" → the form has a required "Brand" field (with "Shared" for templates and media).
- Edit list / template / media → a "Brand" field to move or assign the item.
- A campaign has no brand field. Its brand follows its lists (4.2).

## 6. Send guard

### 6.1 Rules

A campaign may send only when all of these are true:
1. It has at least one list, and every list belongs to the same brand B (no unassigned or ambiguous list).
2. Its messenger is in `B.messengers`.
3. The domain of its `from_email` equals a domain in `B.from_domains` or is a subdomain of one.
4. Its effective template is not owned by another brand. The effective template is its `template_id`, or
   the current default template when `template_id` is NULL (the manager falls back to the default at send
   time). A template owned by B, a shared template, and an unassigned template pass. The rule is skipped
   for the visual content type. Message: `The campaign template belongs to another brand.`

Otherwise the guard returns an error that names the first failed rule, for example
`List 6 (RI Cold) belongs to ri, but list 17 belongs to ta.` or
`Messenger email-ri is not allowed for brand ta.`

### 6.2 Where it runs

- On a status change to `running` or `scheduled` (API and UI).
- In the campaign manager, just before the campaign's first batch is sent. This catches a scheduled
  campaign whose lists or brand records changed after it was scheduled, and any path that bypasses the
  status handler. A campaign blocked here moves to `paused`, and the error goes to the Listmonk log.
- Test sends (`/api/campaigns/:id/test`) also run rules 2, 3 and 4.

The guard does not depend on the brand header. API clients without a header are guarded too.

### 6.3 Transactional sends

Sub-project 2 uses the same rules 2 and 3 for automation steps. The public `/api/tx` endpoint is not
guarded in this sub-project.

## 7. UI

- Top bar: a brand selector with "All brands" first, then brands by `sort_order`.
- A small brand label next to the page title when a brand is selected, so the scope is always visible.
- A new admin page "Brands" under Settings: list, add, and edit brand records (name, list tags,
  messengers, from domains, sort order). Delete is allowed only when no list, template, or media file
  belongs to the brand.
- Labels "Unassigned", "Ambiguous", and "Shared" in the list, template, and media tables in "All brands".

## 8. Rollout

1. Confirm the commit that the RI instance runs now (Coolify deployment record). Rebase `hcm` onto it if
   it differs from `9ab53051`.
2. Build and test locally (section 9).
3. **Dry run** against production, read-only: a script lists every draft, scheduled, running, and paused
   campaign and the guard result for each, plus every list and its brand. Fix data before the deploy if a
   campaign that must send would be blocked.
4. The admin confirms the template assignment (4.4).
5. The admin points the RI Coolify app at branch `hcm` and redeploys (a production deploy is the admin's
   step).
6. The seed runs at startup. Check the switcher, each page, and the guard on production with the RI test
   lists 4 and 5.
7. **Rollback:** point the app back at `gunmade`. Upstream code ignores schema `ext`.

## 9. Tests

- **Go unit tests:** brand resolution from tags (match, none, ambiguous); the scoped user copy (super
  admin, `lists:get_all`, `lists:manage_all`, `campaigns:get_all`, limited role; the real user stays
  unchanged); the copy never holds a permission or list right that the real user lacks (property test
  over random roles); the campaign
  brand predicate (one brand, mixed, no lists, unassigned list); the shared/unassigned rules; every guard rule and message;
  subdomain matching (`news.rangeinsiders.com` passes for `rangeinsiders.com`, `evilrangeinsiders.com`
  fails).
- **Integration tests:** a local Listmonk from `hcm` with its own Postgres, no production connection.
  Each API in 5.3 with each brand, with no header, and with an unknown brand. Each guard case on start,
  on schedule, at the manager check, and on a test send. The scoped user must not stay cached across
  requests. A super admin with a brand selected sees no campaign of another brand and no mixed-brand
  campaign, by list, by id, or by count. A subscriber on RI and TA lists: in the TA view, a remove,
  an unsubscribe, and an update change only TA subscriptions; a delete and a blocklist return 400 and
  change nothing.
  **Permission escalation:** a user with read access to RI list 4 only, and a user with no access to TA
  lists, each send `X-Listmonk-Brand: ri` and `ta`. They get no new read access, and list update, list
  delete, subscriber changes, and campaign changes on lists outside their own rights return the same 403
  as without the header.
- **Browser check:** the switcher and each page on the local instance.
- **Review gate:** `/codex:adversarial-review` on this spec and on the plan; `/codex:review` on the code.

## 10. Risks

- **A missed endpoint** acts on all brands while the UI shows one brand. Mitigation: the plan lists every
  handler in `cmd/` and marks it scoped, global, or not affected; the integration test covers each
  scoped one.
- **Cached user object:** if the scope leaks into the cached user, another request gets the wrong brand.
  Mitigation: request-context copy (5.2) and a test.
- **A new upstream permission path** (after an upstream sync) can skip the scoped copy. Mitigation: the
  route test in section 9 runs after every sync.
- **Upstream sync:** the `GetPermittedLists` hunk and the middleware hook can conflict. Both are small and
  recorded in `PATCHES.md`.
- **Two branches:** shared fixes (for example an upstream release) must go to `gunmade` and `hcm`.
- **Listmonk v7** replaces the Vue UI. The selector, the Brands page, and the form fields must be rebuilt
  then. The middleware, the scope, the guard, and the `ext` schema stay.
- **Tag edits change ownership.** Removing the `ri` tag from list 3 moves it out of RI. The guard then
  blocks RI campaigns to that list, which fails safe.

## 11. Review log

**Codex adversarial review, round 1 (spec):** needs-attention, 2 findings, both accepted.
1. [high] Campaign routes skip list permissions for `campaigns:*_all` users, and `CampaignHasLists`
   accepts a campaign when any list matches. Fix: an independent campaign brand predicate (5.3, D6).
   While I checked this, I found more list-permission paths that skip `GetPermittedLists`, so the single
   hook became a scoped user copy (5.2) with a route allowlist.
2. [high] Subscriber delete removes the shared record from every brand. Fix: delete and blocklist are
   global actions, refused in a brand view (5.4, D8). Other subscriber actions change only brand
   subscriptions.

**Codex adversarial review, round 2 (spec):** needs-attention, 1 finding, accepted.
1. [high] The scoped copy gave read and manage rights on every brand list, so a restricted user could
   gain rights through the header. Fix: intersect brand lists with the real user's read and manage rights
   separately; the copy only narrows (5.2). Escalation tests added (section 9).

Two review rounds are done. No finding is open.
