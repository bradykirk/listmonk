# Subscriber first/last names and campaign preview text — design

Date: 2026-09-17
Status: Approved design, revised after Codex adversarial review rounds 1 and 2, amended at plan time
Target repo: `github.com/bradykirk/listmonk`, branch **`gunmade`** (branched from v6.2.0)

## Goals

1. Replace the single subscriber `name` with `first_name` and `last_name`.
2. Blank out names that listmonk invented from the email address when no name was supplied, and
   stop inventing them going forward.
3. Add a per-campaign **preview text** (inbox preheader) field that works for every HTML campaign,
   including existing visual drafts and their clones.

## Non-goals

- No change to lists, subscriptions, bounces, or analytics.
- No attempt to recover real names that the cleanup blanks (see "Accepted loss").
- No data patch of existing templates or campaign bodies.

## Decisions

| # | Question | Decision |
|---|----------|----------|
| 1 | Where do the fields live? | Real columns, not JSONB `attribs`. Accepts permanent fork divergence for a clean UI and a real CSV column. |
| 2 | Relationship of `name` to the new columns | `name` becomes a Postgres **generated column**. |
| 3 | Which names get blanked | Exact match against the fallback rule for that subscriber's own email. |
| 4 | How preview text reaches the email | **Compile-time injection** after the opening `<body>` tag, following the fork's `autounsubscribe.go` pattern. (Revised from "template block in default templates" after review; see Revision log.) |

## Revision log

**Round 1 (Codex adversarial review):**

- *[high] Wrong deployment source.* Confirmed. `listmonk-private/docker-compose.yml:23` builds
  `github.com/bradykirk/listmonk.git#gunmade`, and `gunmade:PATCHES.md` documents a Coolify app on
  that repo with `docker-compose.gunmade.yml`. The deployed code is the `gunmade` branch, which
  carries 13 fork commits (analytics, auto-tracking, guaranteed unsubscribe, fork migration
  `v6.2.1`) that `listmonk-private/production` does not. **Fix:** implementation targets
  `bradykirk/listmonk` `gunmade`; rollout verifies the live Coolify source and deployed revision.
- *[medium] Existing visual campaigns ignore preview text.* Confirmed. `CompileTemplate` replaces the
  base body with `{{ template "content" . }}` for visual campaigns, and the visual editor copies the
  template into each campaign's `body`/`body_source`, so patching templates cannot reach existing
  drafts or clones. **Fix:** decision 4 changed to compile-time injection into the document itself,
  the same approach `gunmade` already uses for the unsubscribe footer. The template patch script is
  removed.

**Round 2 (Codex adversarial review):**

- *[high] Fork migration skipped on the live database.* Confirmed against
  `gunmade:cmd/upgrade.go`. `upgrade()` returns before its loop when no versioned migrations are
  pending, which is the live state (`v6.2.1` recorded). A hook after the loop never runs, and a
  column check in `checkUpgrade` placed after its own early return is bypassed. **Fix:** both entry
  points run the fork step before their early returns; added an entry-point test against a
  `v6.2.1` database without the new columns.

**Plan-time amendments (2026-09-17, from reading `gunmade` at `f525ea3`).** Where these conflict
with a later section, this list wins.

1. **Second name fallback.** `cmd/public.go` `processSubForm` (public form and website pop-up) sets an
   empty name to the raw local part, `strings.Split(email, "@")[0]`, with no title case. The blank
   rule therefore matches **either** fallback: `name == FallbackName(email)` **or**
   `name == localPart(email)`. Emails are stored lowercased (`internal/utils` `SanitizeEmail`), so an
   exact comparison is correct. This fallback is also deleted.
2. **Helper location.** `FallbackName`, `IsFallbackName`, `SplitName` and `ResolveSubscriberNames`
   live in `models/names.go` (leaf package), not `internal/subimporter`, so the migration, the
   importer, `cmd`, and the dry-run tool share them without import cycles.
3. **Existing methods.** `models.Subscriber` already has `FirstName()` and `LastName()` methods that
   guess from `Name` (used by `subscriber-optin.html`, the install-time default campaign, and
   documented in `docs/docs/content/templating.md`). A field of the same name does not compile. The
   methods are **replaced** by the `FirstName`/`LastName` fields. Template syntax
   `{{ .Subscriber.FirstName }}` is unchanged; it now returns the stored value.
4. **PATCH semantics.** `PATCH /api/subscribers/:id` pre-fills the request from the database, so
   "was `first_name` supplied?" cannot be read from the struct. `ResolveSubscriberNames(sub, prev)`
   splits `Name` only when `Name` changed and `FirstName`/`LastName` did not. PUT and create pass
   `prev == nil`: split `Name` only when both parts are empty.
5. **Both update queries** (`update-subscriber` and `update-subscriber-with-lists`) carry the
   `CASE WHEN $3 != ''` guard. Both are changed.
6. **Preferences page.** `SubscriptionPrefs` rejects an empty name and
   `static/public/templates/subscription.html` marks the input `required`. With blank names allowed,
   a subscriber could not save preferences without inventing a name. Both restrictions are removed;
   the single `name` input is split with `SplitName`.
7. **Query parameters.** `$3` becomes `first_name`; `last_name` is appended as the **last** parameter
   of each query, so no other parameter is renumbered.
8. **Reads need no query change.** The app opens the database with `db.Unsafe()`
   (`cmd/init.go:368`), and campaign queries select `campaigns.*`, so `preview_text` reaches send,
   preview, archive and list responses once the struct field exists.
9. **Test send.** `TestCampaign` copies selected request fields over the stored campaign
   (`cmd/campaigns.go:580-588`); it must also copy `PreviewText`, and the frontend `sendTest`
   payload must send it.
10. **Fork step testability.** `cmd`'s `init()` loads `config.toml`, so `cmd` cannot host tests. The
    fork step's logic (`ForkPending`, `RunFork`) lives in `internal/migrations` with DB tests;
    `cmd/upgrade.go` only calls it. The `upgrade()`/`checkUpgrade()` entry points are verified by
    `scripts/fork/upgrade-e2e.sh`, which runs the real binaries: the `f525ea3` build installs a
    `v6.2.1` database, then the new build runs a normal start (must refuse), `--upgrade` (must
    migrate), a normal start (must serve), and `--upgrade` again (must no-op).
11. **Greeting audit.** The dry-run tool lists templates and non-finished campaigns containing
    `.Subscriber.Name`, `.Subscriber.FirstName` or `.Subscriber.LastName`.
12. **CSV import header map** (`internal/subimporter/importer.go:131`) gains `first_name` and
    `last_name`.
13. **Preheader target.** One rule for every HTML content type. The block goes into the **first**
    of these that has an opening `<body>` tag: the base template, then the campaign's own content.
    If neither has one, it is prepended to the base template. It is skipped when the base or the
    content already mentions `.Campaign.PreviewText`. For visual campaigns the base is already the
    bare content include, so the attached template never counts and the block lands in the visual
    document. This also covers an `html` campaign that carries a full document with no template,
    which the earlier "base template only" wording would have prefixed before `<!doctype>`.
14. **Admin toasts** use `d.name`; a blank name falls back to `d.email`.

## Fork context (from `gunmade`)

- **`PATCHES.md` rule 2: "Never change the database schema."** One recorded exception exists
  (`campaign_unsubs`, additive). This feature is a **second, larger exception**: it alters the
  upstream `subscribers` and `campaigns` tables. The owner accepted the rebase cost in decision 1.
  `PATCHES.md` must record the exception and its rebase procedure.
- **Existing fork migration** `v6.2.1` sits in `migList` (`cmd/upgrade.go`). `PATCHES.md` already
  notes the version-collision risk.
- **Existing compile-time injectors** in `models/campaigns.go` `CompileTemplate`: auto-tracking
  (`models/autotrack.go`) and guaranteed unsubscribe (`models/autounsubscribe.go`, with
  `reBodyClose` and a no-`</body>` fallback, tested in `models/autounsubscribe_test.go` and
  `models/visual_unsub_test.go`).
- **Container start command** (`docker-compose.gunmade.yml:35`):
  `--install --idempotent --yes`, then `--upgrade --yes`, then serve. A deploy runs migrations
  without a prompt.

## Findings that shape the design

- **Name fallback source.** `internal/subimporter/importer.go:654-665` (`ValidateFields`) sets an
  empty name to the email local part, `.` → space, each word title-cased with
  `cases.Title(language.Und)`. It runs on CSV import and on admin/API create
  (`cmd/subscribers.go:236`).
- **Empty name cannot be saved today.** `queries/subscribers.sql:154`:
  `name=(CASE WHEN $3 != '' THEN $3 ELSE name END)` keeps the old name when the new one is empty.
- **SQL cannot reproduce the fallback exactly.** Verified 2026-09-17:

  | local part | Go fallback | Postgres `initcap` |
  |------------|-------------|--------------------|
  | `john_doe` | `John_doe`  | `John_Doe`         |
  | `o'brien`  | `O'brien`   | `O'Brien`          |
  | `j.doe`    | `J Doe`     | `J Doe`            |
  | `info+news`| `Info+News` | `Info+News`        |

  Therefore the blank step runs in Go, using the same function that generated the names.
- **Migration version trap.** `cmd/upgrade.go` runs every `migList` entry whose semver is greater
  than the last recorded one. Another fork version such as `v6.2.2` would cause a future upstream
  migration of that number, or lower, to be skipped silently.
- **Visual campaigns never render their template** (`models/campaigns.go`, `CompileTemplate`).
- Existing names are rendered through `{{ .Subscriber.Name }}` in `default-visual.tpl`,
  `sample-tx.tpl`, the v2.2.0 migration's tx template, the postback messenger, CSV export,
  `queries/campaigns.sql` (subscriber and activity queries), and the fork's analytics activity
  lists. All of these are reads.

## 1. Subscriber names

### Schema

```sql
ALTER TABLE subscribers
    ADD COLUMN first_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN last_name  TEXT NOT NULL DEFAULT '';
-- (backfill, see below)
ALTER TABLE subscribers DROP COLUMN name;
ALTER TABLE subscribers ADD COLUMN name TEXT NOT NULL
    GENERATED ALWAYS AS (btrim(first_name || ' ' || last_name)) STORED;
```

`schema.sql` is updated to the same shape so fresh installs match.

Every read of `name` stays byte-identical to upstream. Any write to `name` fails loudly at runtime
(`cannot insert a non-DEFAULT value into column "name"`), which surfaces missed write paths instead
of letting `name` drift.

The implementation plan must confirm that no view, materialized view, index, or trigger depends on
`subscribers.name` before the `DROP COLUMN` (initial check of `schema.sql` found none).

### Fork migration mechanism

The migration is **not** added to `migList`. It is a separate idempotent function,
`migrations.ForkNamesPreviewText(db)`, in a new file.

**Control flow (both entry points must run the fork check before any early return):**

- `upgrade()` in `cmd/upgrade.go` returns early with "no upgrades to run" when
  `getPendingMigrations` finds nothing. The live database is already at `v6.2.1`, so it takes that
  path. A hook placed after the `migList` loop would **never run** there (Codex round 2).
- Required shape: after the prompt and after `getPendingMigrations`, run the versioned migrations
  if any are pending, then **always** call a new `runForkMigrations(db)` helper, then log and
  return. The existing `len(toRun) == 0` early return must not bypass `runForkMigrations`.
- `checkUpgrade()` also returns early when nothing versioned is pending. The fork column check must
  run **before** that return. If either column is missing, it exits with the standard
  "run --upgrade" message.
- If the upgrade prompt is cancelled, neither versioned nor fork migrations run.
- Fresh installs (`--install --idempotent`) create the columns from `schema.sql`; the following
  `--upgrade` finds no versioned migrations, calls `runForkMigrations`, which detects the columns
  and no-ops.

**Behaviour of `ForkNamesPreviewText`:**

- It detects whether it already ran by checking `information_schema.columns` for
  `subscribers.first_name` and `campaigns.preview_text`. Both present: no-op. Exactly one present:
  abort with an error (partial state must not exist, see transaction below).
- It runs inside one transaction. Any error rolls back all of it and aborts the upgrade.
- Rationale for not using `migList`: a new version such as `v6.2.2` extends the version-collision
  trap. The existing `v6.2.1` is left as is.

### Backfill order (inside the migration transaction)

1. Add `first_name`, `last_name`.
2. **Blank step (Go).** Read `id, email, name` for all subscribers. For each row, compute
   `FallbackName(email)`. If `name == FallbackName(email)`, treat the name as empty.
3. **Split step (Go).** For non-blanked rows, `SplitName(name)`.
4. Write `first_name`/`last_name` in batches.
5. Drop `name`, re-add as generated.

Worked examples:

| email | stored name | result first / last |
|-------|-------------|---------------------|
| `bkirkpatrick00@x.com` | `Bkirkpatrick00` | `""` / `""` |
| `john.smith@x.com` | `John Smith` | `""` / `""` (accepted loss) |
| `john.smith@x.com` | `Johnny S.` | `Johnny` / `S.` |
| `bkirk00@x.com` | `Brady Kirkpatrick` | `Brady` / `Kirkpatrick` |
| `x@x.com` | `Mary Jo Van Der Berg` | `Mary` / `Jo Van Der Berg` |
| `x@x.com` | `Cher` | `Cher` / `""` |

**Accepted loss.** A real person whose submitted name equals the generated one (John Smith at
`john.smith@`) is blanked. No rule can distinguish the two. A later form submission or edit
restores the name.

### Shared name helpers

New file in `internal/subimporter` (exported, unit-tested):

- `FallbackName(email string) string` — the exact current rule, moved out of `ValidateFields`
  unchanged. Used only by the migration and the dry-run tool after this change.
- `SplitName(name string) (first, last string)` — first whitespace-separated token of the trimmed
  name is `first`; remaining tokens joined by one space are `last`.

### Write paths

All must write `first_name`/`last_name` and never `name`:

- `queries/subscribers.sql`: `insert-subscriber`, `upsert-subscriber`,
  `upsert-blocklist-subscriber`, `update-subscriber`, `update-subscriber-with-lists`, and any other
  query found by the plan that inserts or updates `subscribers.name`.
- `update-subscriber`: remove the `CASE WHEN $3 != ''` guard. Submitted values are saved as-is,
  including empty.
- `ValidateFields`: delete the email-prefix fallback. Trim both fields.
- Subimporter prepared statements (`importer.go:310,312`).
- `internal/core/dashboard_growth_db_test.go:66` (fork test fixture inserts `name`).
- Any other Go code or test fixture that writes `subscribers.name` (plan must grep all of
  `cmd/`, `internal/`, `models/`, `queries/`, and `frontend/cypress`).

### API and form compatibility

- `SubReq` and subscriber models gain `FirstName`, `LastName` (`json:"first_name"`,
  `json:"last_name"`). `Name` stays in JSON responses (read from the generated column).
- **Input rule:** if a request supplies `first_name` or `last_name`, those are used and `name` is
  ignored. If it supplies only `name`, it is split with `SplitName`. This keeps the public
  subscription form (`cmd/public.go`, `static/public/templates/subscription.html`) and any existing
  integrations working.
- **Known behaviour change:** an API update that omits all name fields now blanks the name. The
  admin UI always sends both fields. The owner confirmed the website pop-up does not collect names,
  so no known integration is affected.

### CSV import and export

- Import recognises `first_name`, `last_name`, and still `name`, with the same input rule.
- Export header gains `first_name`, `last_name` after `name` (`cmd/subscribers.go:196`). `name` is
  kept so re-importing an export works.

### Admin UI

- `frontend/src/views/SubscriberForm.vue`: replace the Name input with First name and Last name
  inputs (not required, max length 200 each). Send `first_name`, `last_name`.
- Subscriber list keeps displaying `name`.
- i18n: add `subscribers.firstName`, `subscribers.lastName` to `i18n/en.json`.

### Greetings

- Shipped static templates (`default-visual.tpl`, `default-visual.json`, `sample-tx.tpl`): greeting
  becomes `Hello{{ if .Subscriber.FirstName }} {{ .Subscriber.FirstName }}{{ end }},`.
- **Live templates and campaign drafts are copies** and keep `{{ .Subscriber.Name }}`. They still
  render, but a blanked subscriber gets `Hello ,`. The dry-run tool (Rollout step 2) lists every
  template and non-finished campaign whose body contains `.Subscriber.Name`, so the owner can edit
  those greetings by hand before the first send. No automatic rewrite.

## 2. Campaign preview text

### Schema

```sql
ALTER TABLE campaigns ADD COLUMN preview_text TEXT NOT NULL DEFAULT '';
```

Added in the same fork migration and in `schema.sql`.

### Backend

- `models.Campaign`: `PreviewText string` with `db:"preview_text" json:"preview_text"`.
- `queries/campaigns.sql`: include in create, update, get, list (the clone reads the list row), and every query that loads a
  campaign for sending, preview, test send, and archive. Plan must grep each `SELECT` that feeds
  `models.Campaign`.
- `cmd/campaigns.go`: accept and validate (trimmed, max 500 characters).
- Template context: `{{ .Campaign.PreviewText }}` is available to authors who want manual placement.

### Injection (new file `models/autopreheader.go`)

Follows `models/autounsubscribe.go`.

- `preheaderHTML` constant:

  ```html
  <div style="display:none;font-size:1px;color:#ffffff;line-height:1px;max-height:0;max-width:0;opacity:0;overflow:hidden;mso-hide:all;">{{ .Campaign.PreviewText }}&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;</div>
  ```

- `reBodyOpen = regexp.MustCompile(`(?i)<body\b[^>]*>`)`.
- `hasPreheader(sources ...string) bool` — true if any source contains `.Campaign.PreviewText`
  (author placed it manually).
- `autoPreheaderHTML(s string) string` — inserts `preheaderHTML` immediately after the first
  opening `<body ...>` tag; if none exists, prepends it.

Calls in `CompileTemplate`:

- Only when `c.PreviewText != ""` and `tracksAsHTML(c.ContentType)` (plain-text campaigns are
  skipped).
- **Non-visual campaigns:** inject into the base template `body`, unless
  `hasPreheader(c.TemplateBody, c.Body)`.
- **Visual campaigns:** inject into the campaign's own document `body` (the one that becomes the
  `content` template), unless `hasPreheader(c.Body)`. The attached template does not count, for the
  same reason documented for the unsubscribe footer.
- Both calls run **before** the `regTplFuncs` loop, like the existing injectors.
- `html/template` escapes the value, so user input cannot inject markup (verified: campaign
  templates use `html/template`).
- Plain-text `altbody` is unchanged.
- Archive rendering: the block is `display:none` and harmless on a web page.

Because injection happens at compile time from the campaign's own column, existing drafts, clones
(once `cloneCampaign` in `frontend/src/views/Campaigns.vue` copies `preview_text` into its create payload), and later visual edits all work with no data patch.

### Admin UI

- `frontend/src/views/Campaign.vue`: "Preview text" input directly below Subject, help text "Shown
  after the subject line in most inboxes. Leave blank to omit." Included in create and update
  payloads and loaded on edit. Shown for all content types except plain text.
- `frontend/src/views/Campaigns.vue` `cloneCampaign`: copy `preview_text`. Cloning is built client-side from the list row, not by a SQL query.
- i18n: `campaigns.previewText`, `campaigns.previewTextHelp`.

## 3. Rollout and safety

1. **Live deployment source (confirmed in Coolify, 2026-09-17).** Project "Gun Made - Email
   Marketing", environment `production`, application `listmonk-private` (the name is historical):
   - Repository `bradykirk/listmonk`, branch `gunmade`, commit `HEAD`.
   - Build strategy Compose, compose location `/docker-compose.gunmade.yml`, base directory `/`.
   - Auto deploy: **Deploy on push (webhooks)**. A push to `gunmade` deploys immediately and runs
     the migration.
   - Currently deployed: `f525ea3` (feat(dashboard): audience growth section), webhook, success.
     This is the rollback target.
   - `listmonk-private/docker-compose.yml` (remote build context) is **not** used by the live app;
     it dates from the earlier deployments `fd7e261` and `63525f6`.
   Re-check the deployed commit immediately before merging, in case it changed.
2. **Dry-run tool.** New Go program under `scripts/fork/namecheck/`, read-only, takes a database
   URL. Uses `FallbackName` and `SplitName`. Prints total subscribers, count to blank, count to
   split, 20 random examples of each, and every template and non-finished campaign containing
   `.Subscriber.Name`. Run against a copy of production; the owner reviews.
3. **Backup.** `pg_dump` of the `listmonk-data` database **before merging to `gunmade`**, because a
   push can auto-deploy and the migration drops a column. Back up `listmonk-uploads` per the
   existing storage notes.
4. **Deploy.** Merge the feature branch into `gunmade`. The webhook deploys it automatically. Do not
   merge until steps 2 and 3 are complete. Work on a feature branch, never push directly to
   `gunmade`.
5. **Verify the deployed revision** matches the merge commit (build string in the container log or
   `/api/config`), then check the migration log line and both new columns.
6. **Edit greetings** listed by the dry-run tool.
7. **Docs.** `gunmade:PATCHES.md` gains: the second schema-rule exception, the migration mechanism,
   new files, modified files with "if it conflicts" guidance, and a rebase note to grep upstream
   queries for new writes to `subscribers.name`. `listmonk-private/CUSTOMIZATIONS.md` gets a one-line
   pointer only.

## Testing

- Unit: `FallbackName` against the current behaviour (table above plus unicode).
- Unit: `SplitName` for empty, single token, two tokens, many tokens, extra whitespace.
- Unit: API input rule (first/last only, name only, both, none).
- Unit (`models/autopreheader_test.go`): placement after `<body>` with attributes and mixed case;
  no `<body>` prepends; no injection when empty; no double block when the author placed
  `.Campaign.PreviewText`; value is HTML-escaped.
- End-to-end through `CompileTemplate`: richtext with template, html, markdown, visual with template
  attached (template must not count), plain (skipped). Coexists with the existing tracking and
  unsubscribe injectors.
- Migration entry point (not only the function): start from a database at `v6.2.1` **without** the
  new columns. Call `checkUpgrade` and confirm it exits with the "run --upgrade" message. Call
  `upgrade()` and confirm the fork migration runs although no versioned migration is pending.
  Start the app and confirm it serves. Call `upgrade()` again and confirm a no-op.
- Migration against a fresh `--install --idempotent` database: fork step no-ops.
- Migration data: run against a restored copy of production data twice; second run is a no-op; row
  count unchanged; spot-check the dry-run examples against results.
- Integration: create subscriber with blank names via API and admin UI; import CSV with
  `first_name`/`last_name`, with `name` only, and with neither; update a subscriber to blank;
  public subscribe; blocklist upsert.
- Visual: existing visual draft gains preview text; clone of that campaign keeps it; edit it in the
  visual builder and re-send a test; block still present.
- Existing Go tests (including fork tests) and Cypress subscriber and campaign specs pass.

## Risks

| Risk | Mitigation |
|------|------------|
| Implementation lands in the wrong repo or branch | Target is `bradykirk/listmonk` `gunmade`; rollout steps 1 and 5 verify the live source and revision. |
| Missed write path to `name` | Generated column makes it a loud runtime error; integration tests cover create, update, import, public subscribe, blocklist, and fork fixtures. |
| Upstream rebase adds a new `name` write or touches `CompileTemplate` | `PATCHES.md` rebase notes; errors are loud; injector tests fail on conflict. |
| Blanking a real name | Accepted loss; dry-run examples reviewed first; backup exists. |
| Live greetings render `Hello ,` | Dry-run tool lists affected templates and drafts for manual edit before first send. |
| Fork migration skipped because no versioned migration is pending | Fork step runs before the early returns in `upgrade()` and `checkUpgrade()`; entry-point test on a `v6.2.1` database. |
| Migration failure mid-way | Single transaction, so the DB stays on the old schema. The new image cannot start on the old schema, so rollback means redeploying the commit recorded in rollout step 1 (`f525ea3` as of 2026-09-17) and restoring the backup if the transaction committed. |
| Preheader injected in the wrong place | Same regex-and-fallback pattern as the tested unsubscribe injector; unit and end-to-end tests. |
| API caller omits names and blanks them | No known caller; documented behaviour change. |
