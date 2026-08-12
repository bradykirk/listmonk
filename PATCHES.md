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
| `queries/analytics.sql` | Three named queries. Each returns one JSON column, matching the existing `get-dashboard-charts` pattern, so the Go layer needs no result structs. |
| `internal/core/analytics.go` | Core methods that run those queries. |
| `cmd/analytics.go` | HTTP handlers. Note: this directory is `package main`, not `package cmd`. |
| `frontend/src/views/Analytics.vue` | The page. Uses `chart.js`, which listmonk already bundles — no new dependency. |

## Modified files (rebase watch list)

| File | Change | If it conflicts |
| --- | --- | --- |
| `models/queries.go` | Three `*sqlx.Stmt` fields with `query:` tags. | Re-add the three fields. The tag names must match the `-- name:` headers in `queries/analytics.sql` exactly, or listmonk fails at startup rather than at build. |
| `cmd/handlers.go` | Three `GET /api/analytics/*` routes. | Re-add them next to the `/api/dashboard/*` routes. They reuse the existing `campaigns:get_analytics` permission — do not invent a new one. |
| `frontend/src/api/index.js` | Three client functions. | Re-add below `getDashboardCharts`. |
| `frontend/src/router/index.js` | Route `listAnalytics` at `/analytics`. | Re-add near `campaignAnalytics`. |
| `frontend/src/components/Navigation.vue` | Sidebar item in the campaigns group. | Re-add inside the campaigns `b-menu-item`. |

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

- `go build ./...` and `go vet` pass.
- The three queries were executed against a Postgres 17 container loaded with
  `schema.sql`, first empty and then seeded. Results were checked by hand:
  growth totals accumulate correctly, unsubscribed members are excluded from
  cohorts, and a subscriber who last opened 200 days ago falls into `cold`.
- One real bug was found this way: the growth query originally put a window
  function beside the `GROUP BY`, and Postgres rejected the `ORDER BY` as
  ungrouped. The running total is now computed in an outer query.

## Licence

listmonk is **AGPLv3**. This fork is published on GitHub, which satisfies the
source-availability obligation that applies when a modified version is served
to users over a network. Keep the fork public.
