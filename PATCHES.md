# gunmade fork — patch record

This fork adds a list analytics page to listmonk's admin. It exists so that
campaign statistics, audience growth and engagement live in the same interface
as the campaigns themselves, instead of a second tool.

**Branch:** `gunmade`, branched from tag **v6.2.0**.

## Rules for this fork

1. **Branch from release tags, never from `main`.** Rebasing onto a tag is
   predictable. Rebasing onto unreleased churn is not.
2. **Never change the database schema.** A schema patch turns every future
   upstream migration into a merge problem. This is the rule that keeps the
   fork cheap. If a feature needs a schema change, contribute it upstream
   instead.
3. **Put new code in new files.** New files never conflict during a rebase.
   Only five existing files are touched, by 20 lines in total.
4. **Keep this file current.** When a hunk conflicts during a rebase, the entry
   below tells you whether the patch is still needed.

## Upgrading to a new listmonk release

```sh
git fetch upstream --tags
git rebase v6.3.0          # the new tag
go build ./...             # backend still compiles
make dist                  # full build
```

Then rebuild and push the image, and update the tag in Coolify.

## New files (no rebase risk)

| File | Purpose |
| --- | --- |
| `queries/analytics.sql` | Nine named queries. Each returns one JSON column, matching the existing `get-dashboard-charts` pattern, so the Go layer needs no result structs. |
| `internal/core/analytics.go` | Core methods that run those queries. |
| `cmd/analytics.go` | HTTP handlers. Note: this directory is `package main`, not `package cmd`. |
| `frontend/src/views/Analytics.vue` | The page. Uses `chart.js`, which listmonk already bundles — no new dependency. |

## Modified files (rebase watch list)

| File | Change | If it conflicts |
| --- | --- | --- |
| `models/queries.go` | Nine `*sqlx.Stmt` fields with `query:` tags. | Re-add the fields. The tag names must match the `-- name:` headers in `queries/analytics.sql` exactly, or listmonk fails at startup rather than at build. |
| `cmd/handlers.go` | Nine `GET /api/analytics/*` routes. | Re-add them next to the `/api/dashboard/*` routes. They reuse the existing `campaigns:get_analytics` permission — do not invent a new one. |
| `frontend/src/api/index.js` | Nine client functions. | Re-add below `getDashboardCharts`. |
| `frontend/src/router/index.js` | Route `listAnalytics` at `/analytics`. | Re-add near `campaignAnalytics`. |
| `frontend/src/components/Navigation.vue` | Sidebar item in the campaigns group. | Re-add inside the campaigns `b-menu-item`. |

## What the page shows

Modelled on EmailOctopus's campaign report and home dashboard, after touring
that account in full:

- A KPI row: subscribers, 30-day joins and leaves, emails sent, open rate,
  click rate, and **bounce and complaint rates against the SES thresholds**
- Audience growth per week, with the running total
- Engagement cohorts, from "active < 30 days" to "cold 180 days+"
- Per campaign: sent, opened, didn't open, clicked, didn't click, unsubscribed,
  bounced (split hard/soft), complained
- A deliverability trend chart: open, click, bounce and complaint rate per
  campaign over time
- Per campaign: opens and clicks per hour for the first 48 hours
- Per campaign: most clicked links, with total and unique counts
- Engagement and bounces per mailbox provider
- Open rate by the weekday and hour of sending
- A recent activity feed of per-subscriber opens and clicks

Three of these go beyond what EmailOctopus reports. The KPI thresholds, the
deliverability trend and the mailbox-provider split all exist for the same
reason: SES suspends an account at 5% bounces or 0.1% complaints, and the
per-provider view is the only one that catches a single provider filtering to
spam before the overall rate moves.

## Numbers that are approximate, and why

- **Unsubscribes per campaign are attributed, not exact.** listmonk's
  `unsubscribe-by-campaign` query sets `subscriber_lists.status` and
  `updated_at` only, and stores no campaign reference. The query counts
  unsubscribes from a campaign's own lists in the 72 hours after it started.
  Two campaigns sent to one list inside that window will both claim the same
  unsubscribe. The page labels the column and says so beneath the table.
- **Didn't open / didn't click** are derived as `sent` minus the action count.
  listmonk keeps no per-campaign recipient list, so an exact audience segment is
  not available. EmailOctopus does keep one — it stores a per-recipient "Was
  sent" event, visible on its contact timeline — and that is the single
  structural difference behind every derived number here.
- **The mailbox-provider table counts blocklisted addresses; the KPI row does
  not.** This is deliberate. listmonk blocklists a subscriber when they bounce,
  so excluding them would empty the bounce column of the very table that exists
  to show it.
- **Send times are read in the database's timezone,** not the recipient's. With
  one sender and a mostly US audience that is the useful reading, but it is
  "when we press send", not "when they read".

## Deliberate omissions

- **Automation and transactional reports.** `campaign_views.campaign_id` is
  `NOT NULL`, so an open cannot be recorded without a campaign. A welcome email
  sent through `/api/tx` will never show an open rate here. EmailOctopus does
  report these, on its home dashboard and per automation. The only route to that
  data is an SES configuration set with open tracking, outside listmonk.
- **A per-recipient send log.** EmailOctopus's contact timeline shows "Was sent
  <campaign>" for every recipient. listmonk stores only an aggregate
  `campaigns.sent` counter, and adding a row per recipient per campaign would be
  a schema change — which rule 2 forbids. listmonk does ship a per-subscriber
  Activity tab covering opens and clicks (`SubscriberActivity.vue` on the
  subscriber form), so the rest of that timeline is already covered upstream.

## Behaviour notes

- **Permission.** All three endpoints require `campaigns:get_analytics`. The
  `Campaign Editor` role already has it, so a VA can view analytics without any
  further change.
- **List filter.** `list_id=0` means all lists. The page defaults to it.
- **Engagement data depends on a setting.** listmonk records opens and clicks
  against a subscriber only when Settings → Privacy → *individual subscriber
  tracking* is on. It ships **off** and it is **not retroactive**. With it off,
  the engagement chart shows everyone as "never engaged" and it is correct to do
  so.
- **Open rates overstate engagement.** Apple Mail Privacy Protection pre-loads
  images and registers opens nobody performed. Clicks are the reliable signal.
  The page says so next to the table.

## Verification performed

- `go build ./...`, `go vet`, `yarn lint` and `make dist` all pass.
- Every query was executed against a Postgres 17 container loaded with
  `schema.sql`, first empty and then seeded with 400 subscribers across four
  mailbox providers, three campaigns at different weekday/hour slots, all three
  bounce types, and unsubscribes placed deliberately inside and outside the
  72-hour attribution window. Results were checked by hand:
  - growth totals accumulate correctly
  - unsubscribed members are excluded from cohorts, and a subscriber who last
    opened 200 days ago falls into `cold`
  - the campaign attributed exactly the 5 unsubscribes inside its window and
    none of the 3 placed 30 days later
  - the 90-day summary excluded a 200-day-old campaign and its 9 bounces
- The binary was then booted against that database and all nine endpoints were
  called over HTTP. Startup is the real test of the `query:` tags: a name that
  does not match a `-- name:` header fails when statements are prepared, not at
  build time.
- Two real bugs were found this way, neither by inspection:
  - the growth query originally put a window function beside the `GROUP BY`, and
    Postgres rejected the `ORDER BY` as ungrouped. The running total is now
    computed in an outer query.
  - `TRUNCATE ... CASCADE` on `lists` silently deletes `roles` and `users`,
    because role rows reference lists. Use `DELETE` when clearing content from a
    test database, or the admin login goes with it.

## Licence

listmonk is **AGPLv3**. This fork is published on GitHub, which satisfies the
source-availability obligation that applies when a modified version is served
to users over a network. Keep the fork public.
