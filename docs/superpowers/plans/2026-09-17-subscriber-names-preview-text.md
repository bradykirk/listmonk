# Subscriber First/Last Names and Campaign Preview Text Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Store subscriber first and last names (blanking names listmonk invented from email addresses) and add a per-campaign inbox preview text that reaches every HTML campaign.

**Architecture:** `subscribers.first_name`/`last_name` become real columns and `subscribers.name` becomes a Postgres generated column, so every existing read of `name` keeps working and any missed write fails loudly. A fork schema step outside `migList` backfills names in Go using the exact rules that invented them. Preview text is a `campaigns.preview_text` column injected at compile time after the first opening `<body>` tag, following the fork's existing unsubscribe-footer injector.

**Tech Stack:** Go 1.26, sqlx + lib/pq, goyesql named queries, Postgres (16 locally, 17 in production), Vue 2 + Buefy admin, Homebrew `postgresql@16` for DB tests.

**Spec:** `docs/superpowers/specs/2026-09-17-subscriber-names-preview-text-design.md` — read the "Plan-time amendments" list first; it overrides later sections.

## Global Constraints

- Repository `bradykirk/listmonk`, branch `feat/names-preview-text`, based on `gunmade` at `f525ea3f`. **Never push to `gunmade`:** Coolify deploys every push to `gunmade` and the migration drops a column.
- Generated column expression, verbatim everywhere: `btrim(first_name || ' ' || last_name)`.
- The fork schema step is **not** added to `migList` in `cmd/upgrade.go`.
- No tests in package `cmd` (its `init()` loads `config.toml` and exits). Put tests in `models`, `internal/...`, or scripts.
- DB tests skip unless `LISTMONK_TEST_DSN` is set.
- No new Go or JS dependencies. `golang.org/x/text` is already in `go.mod`.
- Preview text: trimmed, maximum 500 characters.
- The admin frontend receives API responses with **camelCased** keys (`firstName`, `previewText`) and sends **snake_case** keys (`first_name`, `preview_text`).
- Every fork change carries a `gunmade fork:` comment, matching existing fork code.
- Commit messages end with: `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`

## Test database (used from Task 2 onward)

Start a throwaway Postgres once per session. It lives outside the repo.

```bash
export PGBIN=/opt/homebrew/opt/postgresql@16/bin
export LM_PG="${TMPDIR:-/tmp}/lm-test-pg"
[ -d "$LM_PG" ] || "$PGBIN/initdb" -D "$LM_PG" -U lmtest --auth=trust >/dev/null
"$PGBIN/pg_ctl" -D "$LM_PG" -o "-p 55432 -k $LM_PG" -l "$LM_PG/log" -w status >/dev/null 2>&1 \
  || "$PGBIN/pg_ctl" -D "$LM_PG" -o "-p 55432 -k $LM_PG" -l "$LM_PG/log" -w start
"$PGBIN/psql" -h 127.0.0.1 -p 55432 -U lmtest -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='lmtest'" | grep -q 1 \
  || "$PGBIN/createdb" -h 127.0.0.1 -p 55432 -U lmtest lmtest
export LISTMONK_TEST_DSN='postgres://lmtest@127.0.0.1:55432/lmtest?sslmode=disable'
```

Stop it at the end with `"$PGBIN/pg_ctl" -D "$LM_PG" stop`.

## File map

| File | Status | Responsibility |
|------|--------|----------------|
| `models/names.go` | create | Name rules: `FallbackName`, `IsFallbackName`, `SplitName`, `JoinName`, `ResolveSubscriberNames` |
| `models/names_test.go` | create | Unit tests for the name rules and template field access |
| `models/subscribers.go` | modify | `FirstName`/`LastName` fields replace the guessing methods; export struct fields |
| `internal/migrations/fork_names_preview.go` | create | `ForkPending`, `RunFork` |
| `internal/migrations/fork_names_preview_db_test.go` | create | DB tests for the fork step |
| `schema.sql` | modify | New columns for fresh installs |
| `queries/subscribers.sql` | modify | Five write queries, export query |
| `queries/campaigns.sql` | modify | `create-campaign`, `update-campaign` |
| `internal/core/subscribers.go` | modify | Pass first/last to queries |
| `internal/core/campaigns.go` | modify | Pass preview text to queries |
| `internal/core/dashboard_growth_db_test.go` | modify | Extract `newTestDB`; fixture writes `first_name` |
| `internal/core/subscriber_names_db_test.go` | create | Query-level tests for subscriber writes |
| `internal/core/campaign_preview_db_test.go` | create | Query-level test for preview text |
| `internal/subimporter/importer.go` | modify | CSV headers, statements, `ValidateFields` |
| `internal/subimporter/names_test.go` | create | `ValidateFields` and CSV header tests |
| `cmd/install.go` | modify | Sample subscribers, default campaign greeting |
| `cmd/upgrade.go` | modify | Call the fork step before both early returns |
| `cmd/subscribers.go` | modify | PUT/PATCH name resolution, CSV export columns |
| `cmd/public.go` | modify | Remove public-form fallback; preferences page allows blank |
| `cmd/campaigns.go` | modify | Validate preview text; copy it on test send |
| `models/campaigns.go` | modify | `PreviewText` field; two calls in `CompileTemplate` |
| `models/autopreheader.go` | create | Preheader block, target choice, injection |
| `models/autopreheader_test.go` | create | Unit and end-to-end tests |
| `scripts/fork/namecheck/main.go` | create | Read-only dry run |
| `scripts/fork/upgrade-e2e.sh` | create | Real-binary test of `upgrade()`/`checkUpgrade()` |
| `static/public/templates/subscription.html` | modify | Name input no longer `required` |
| `static/email-templates/default-visual.tpl`, `default-visual.json`, `sample-tx.tpl`, `subscriber-optin.html` | modify | Greetings tolerate blank first names |
| `docs/docs/content/templating.md` | modify | Describe stored first/last names |
| `frontend/src/views/SubscriberForm.vue` | modify | First/Last inputs |
| `frontend/src/views/Campaign.vue` | modify | Preview text input and payloads |
| `frontend/src/views/Campaigns.vue` | modify | Clone copies preview text |
| `i18n/en.json` | modify | Five new keys |
| `PATCHES.md` | modify | Record the change and the second schema exception |

---

### Task 1: Name rules and subscriber name fields

**Files:**
- Create: `models/names.go`
- Create: `models/names_test.go`
- Modify: `models/subscribers.go:28-37` (struct), `models/subscribers.go:76-102` (delete methods)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func FallbackName(email string) string`
  - `func IsFallbackName(name, email string) bool`
  - `func SplitName(name string) (first, last string)`
  - `func JoinName(first, last string) string`
  - `func ResolveSubscriberNames(s *Subscriber, prev *Subscriber)`
  - `models.Subscriber` fields `FirstName string` (`db:"first_name" json:"first_name"`), `LastName string` (`db:"last_name" json:"last_name"`)

- [ ] **Step 1: Write the failing tests**

Create `models/names_test.go`:

```go
package models

import (
	"bytes"
	"html/template"
	"testing"
)

// The exact outputs of the rule that subimporter.ValidateFields used to apply.
// Verified against the removed code on 2026-09-17. Postgres initcap differs on
// the first three rows, which is why the migration runs this rule in Go.
func TestFallbackName(t *testing.T) {
	cases := []struct{ email, want string }{
		{"john_doe@example.com", "John_doe"},
		{"o'brien@example.com", "O'brien"},
		{"a1b2@example.com", "A1b2"},
		{"j.doe@example.com", "J Doe"},
		{"info+news@example.com", "Info+News"},
		{"bkirk00@example.com", "Bkirk00"},
		{"ÉMILE@example.com", "Émile"},
		{"JOHN.SMITH@example.com", "John Smith"},
	}
	for _, c := range cases {
		if got := FallbackName(c.email); got != c.want {
			t.Errorf("FallbackName(%q) = %q, want %q", c.email, got, c.want)
		}
	}
}

func TestIsFallbackName(t *testing.T) {
	cases := []struct {
		name, email string
		want        bool
	}{
		{"Bkirkpatrick00", "bkirkpatrick00@example.com", true},
		{"John Smith", "john.smith@example.com", true},
		{"  John Smith ", "john.smith@example.com", true},
		{"John_doe", "john_doe@example.com", true},
		// initcap-style output was never produced by listmonk.
		{"John_Doe", "john_doe@example.com", false},
		// Public subscription form rule: the raw local part.
		{"pop.up", "pop.up@example.com", true},
		{"Johnny S.", "john.smith@example.com", false},
		{"Brady Kirkpatrick", "bkirk00@example.com", false},
		{"", "john.smith@example.com", false},
	}
	for _, c := range cases {
		if got := IsFallbackName(c.name, c.email); got != c.want {
			t.Errorf("IsFallbackName(%q, %q) = %v, want %v", c.name, c.email, got, c.want)
		}
	}
}

func TestSplitName(t *testing.T) {
	cases := []struct{ name, first, last string }{
		{"", "", ""},
		{"   ", "", ""},
		{"Cher", "Cher", ""},
		{"Brady Kirkpatrick", "Brady", "Kirkpatrick"},
		{"Mary Jo Van Der Berg", "Mary", "Jo Van Der Berg"},
		{"  Pat   O'Brien  ", "Pat", "O'Brien"},
	}
	for _, c := range cases {
		first, last := SplitName(c.name)
		if first != c.first || last != c.last {
			t.Errorf("SplitName(%q) = %q, %q; want %q, %q", c.name, first, last, c.first, c.last)
		}
	}
}

func TestJoinName(t *testing.T) {
	cases := []struct{ first, last, want string }{
		{"", "", ""},
		{"Cher", "", "Cher"},
		{"", "Doe", "Doe"},
		{"Mary", "Jo Smith", "Mary Jo Smith"},
	}
	for _, c := range cases {
		if got := JoinName(c.first, c.last); got != c.want {
			t.Errorf("JoinName(%q, %q) = %q, want %q", c.first, c.last, got, c.want)
		}
	}
}

func TestResolveSubscriberNames(t *testing.T) {
	stored := Subscriber{FirstName: "Mary", LastName: "Smith", Name: "Mary Smith"}

	cases := []struct {
		label string
		in    Subscriber
		prev  *Subscriber
		want  Subscriber
	}{
		{"create: legacy name only is split", Subscriber{Name: " Mary Jo Smith "}, nil,
			Subscriber{FirstName: "Mary", LastName: "Jo Smith", Name: "Mary Jo Smith"}},
		{"create: first/last win over a stale name", Subscriber{Name: "Ignored", FirstName: " Ann ", LastName: "Lee"}, nil,
			Subscriber{FirstName: "Ann", LastName: "Lee", Name: "Ann Lee"}},
		{"create: no name stays blank", Subscriber{}, nil, Subscriber{}},
		{"patch: legacy name changed", Subscriber{FirstName: "Mary", LastName: "Smith", Name: "Jane Doe"}, &stored,
			Subscriber{FirstName: "Jane", LastName: "Doe", Name: "Jane Doe"}},
		{"patch: first name changed", Subscriber{FirstName: "Janet", LastName: "Smith", Name: "Mary Smith"}, &stored,
			Subscriber{FirstName: "Janet", LastName: "Smith", Name: "Janet Smith"}},
		{"patch: nothing changed", stored, &stored, stored},
		{"patch: name cleared", Subscriber{FirstName: "Mary", LastName: "Smith", Name: ""}, &stored, Subscriber{}},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			got := c.in
			ResolveSubscriberNames(&got, c.prev)
			if got.FirstName != c.want.FirstName || got.LastName != c.want.LastName || got.Name != c.want.Name {
				t.Errorf("got %q / %q / %q, want %q / %q / %q",
					got.FirstName, got.LastName, got.Name, c.want.FirstName, c.want.LastName, c.want.Name)
			}
		})
	}
}

// Templates written as {{ .Subscriber.FirstName }} against the old methods must
// keep working against the fields, including the blank-name greeting idiom.
func TestSubscriberNameFieldsInTemplates(t *testing.T) {
	tpl := template.Must(template.New("t").Parse(
		`Hello{{ if .Subscriber.FirstName }} {{ .Subscriber.FirstName }}{{ end }}, {{ .Subscriber.LastName }}`))

	cases := []struct {
		sub  Subscriber
		want string
	}{
		{Subscriber{FirstName: "Mary", LastName: "Smith"}, "Hello Mary, Smith"},
		{Subscriber{}, "Hello, "},
	}
	for _, c := range cases {
		var b bytes.Buffer
		if err := tpl.Execute(&b, map[string]any{"Subscriber": c.sub}); err != nil {
			t.Fatalf("execute: %v", err)
		}
		if b.String() != c.want {
			t.Errorf("got %q, want %q", b.String(), c.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./models/ -run 'FallbackName|IsFallbackName|SplitName|JoinName|ResolveSubscriberNames|NameFields' -v`
Expected: FAIL to compile — `undefined: FallbackName`, and `unknown field FirstName in struct literal`.

- [ ] **Step 3: Replace the guessing methods with fields**

In `models/subscribers.go`, change the `Subscriber` struct to:

```go
// Subscriber represents an e-mail subscriber.
type Subscriber struct {
	Base

	UUID  string `db:"uuid" json:"uuid"`
	Email string `db:"email" json:"email" form:"email"`

	// gunmade fork: first_name and last_name are stored; name is a generated
	// column, btrim(first_name || ' ' || last_name). See models/names.go.
	Name      string         `db:"name" json:"name" form:"name"`
	FirstName string         `db:"first_name" json:"first_name" form:"first_name"`
	LastName  string         `db:"last_name" json:"last_name" form:"last_name"`
	Attribs   JSON           `db:"attribs" json:"attribs"`
	Status    string         `db:"status" json:"status"`
	Lists     types.JSONText `db:"lists" json:"lists"`
}
```

Delete this whole block from `models/subscribers.go` (the two methods and their comments, currently lines 76-102):

```go
// FirstName splits the name by spaces and returns the first chunk
// of the name that's greater than 2 characters in length, assuming
// that it is the subscriber's first name.
func (s Subscriber) FirstName() string {
	for _, s := range strings.Split(s.Name, " ") {
		if len(s) > 2 {
			return s
		}
	}

	return s.Name
}

// LastName splits the name by spaces and returns the last chunk
// of the name that's greater than 2 characters in length, assuming
// that it is the subscriber's last name.
func (s Subscriber) LastName() string {
	chunks := strings.Split(s.Name, " ")
	for i := len(chunks) - 1; i >= 0; i-- {
		chunk := chunks[i]
		if len(chunk) > 2 {
			return chunk
		}
	}

	return s.Name
}
```

If `strings` is now unused in `models/subscribers.go`, remove it from the imports (`go build` tells you).

- [ ] **Step 4: Write the name rules**

Create `models/names.go`:

```go
package models

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Subscriber name rules (gunmade fork).
//
// Subscribers store first_name and last_name. subscribers.name is a Postgres
// generated column, btrim(first_name || ' ' || last_name), so every existing
// read of name keeps working and any write to it fails loudly.
//
// These rules are shared by the fork migration, the importer, the HTTP
// handlers and scripts/fork/namecheck, so each one exists exactly once.

// FallbackName reproduces the name that the importer and the admin API used to
// invent for a subscriber created without one: the e-mail's local part, dots
// turned into spaces, each word title-cased. It must stay byte-identical to
// that removed rule, or the migration stops recognising the names it made.
func FallbackName(email string) string {
	name := strings.ToLower(strings.Split(email, "@")[0])

	parts := strings.Fields(strings.ReplaceAll(name, ".", " "))
	for n, p := range parts {
		parts[n] = cases.Title(language.Und).String(p)
	}

	return strings.Join(parts, " ")
}

// IsFallbackName reports whether name was invented by listmonk from email
// rather than supplied by a person. Two rules invented names: FallbackName
// (importer and admin API) and the raw local part (public subscription form).
func IsFallbackName(name, email string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}

	return name == FallbackName(email) || name == strings.Split(email, "@")[0]
}

// SplitName splits a full name into the first word and the remaining words.
func SplitName(name string) (first, last string) {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "", ""
	}

	return parts[0], strings.Join(parts[1:], " ")
}

// JoinName mirrors the subscribers.name generated column.
func JoinName(first, last string) string {
	return strings.TrimSpace(first + " " + last)
}

// ResolveSubscriberNames trims the name fields of s and derives FirstName and
// LastName from the legacy single Name when that is what the caller sent. Name
// is then rewritten to match the generated column.
//
// prev is the stored subscriber when the request was pre-filled from the
// database (PATCH), and nil otherwise (create, PUT, public forms):
//   - prev == nil: Name is split only when FirstName and LastName are both empty.
//   - prev != nil: Name is split only when Name changed and neither FirstName
//     nor LastName did.
func ResolveSubscriberNames(s *Subscriber, prev *Subscriber) {
	s.Name = strings.TrimSpace(s.Name)
	s.FirstName = strings.TrimSpace(s.FirstName)
	s.LastName = strings.TrimSpace(s.LastName)

	legacy := s.FirstName == "" && s.LastName == "" && s.Name != ""
	if prev != nil {
		legacy = s.Name != prev.Name && s.FirstName == prev.FirstName && s.LastName == prev.LastName
	}
	if legacy {
		s.FirstName, s.LastName = SplitName(s.Name)
	}

	s.Name = JoinName(s.FirstName, s.LastName)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./models/ -v -run 'FallbackName|IsFallbackName|SplitName|JoinName|ResolveSubscriberNames|NameFields' && go build ./... && go test ./models/`
Expected: PASS for all listed tests; build succeeds; the existing `models` tests still pass.

- [ ] **Step 6: Commit**

```bash
git add models/names.go models/names_test.go models/subscribers.go
git commit -m "feat(subscribers): first/last name fields and shared name rules

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Fork schema step

**Files:**
- Create: `internal/migrations/fork_names_preview.go`
- Create: `internal/migrations/fork_names_preview_db_test.go`

**Interfaces:**
- Consumes: `models.IsFallbackName`, `models.SplitName` (Task 1).
- Produces:
  - `func ForkPending(db *sqlx.DB) (bool, error)`
  - `func RunFork(db *sqlx.DB, lo *log.Logger) error` — logs exactly `gunmade fork migration applied: %d subscribers, %d fallback names blanked, %d names split into first/last` when it applies the step, and logs nothing when it no-ops.
  - `func forkColumns(q sqlx.Queryer) (names, preview bool, err error)` (package-private, used by tests)

- [ ] **Step 1: Start the test database**

Run the commands in "Test database" above. Expected: `LISTMONK_TEST_DSN` is exported and `"$PGBIN/psql" "$LISTMONK_TEST_DSN" -c 'select 1'` prints `1`.

- [ ] **Step 2: Write the failing tests**

Create `internal/migrations/fork_names_preview_db_test.go`:

```go
package migrations

import (
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// These tests need a Postgres server where the user may create databases:
//
//	LISTMONK_TEST_DSN='postgres://lmtest@127.0.0.1:55432/lmtest?sslmode=disable' \
//	  go test ./internal/migrations/ -run Fork -v

var discard = log.New(io.Discard, "", 0)

func forkTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	dsn := os.Getenv("LISTMONK_TEST_DSN")
	if dsn == "" {
		t.Skip("LISTMONK_TEST_DSN not set")
	}

	admin, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close() })

	name := fmt.Sprintf("lm_fork_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create database: %v", err)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + name

	db, err := sqlx.Connect("postgres", u.String())
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(func() {
		db.Close()
		admin.Exec("DROP DATABASE IF EXISTS " + name)
	})

	schema, err := os.ReadFile("../../schema.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("install schema: %v", err)
	}

	return db
}

func mustExec(t *testing.T, db *sqlx.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// toPreForkSchema reshapes a schema.sql install into the shape production has
// before the fork step (gunmade f525ea3), whether or not schema.sql already
// carries the fork columns.
func toPreForkSchema(t *testing.T, db *sqlx.DB) {
	t.Helper()

	names, preview, err := forkColumns(db)
	if err != nil {
		t.Fatalf("fork columns: %v", err)
	}
	if names {
		mustExec(t, db, `ALTER TABLE subscribers DROP COLUMN name`)
		mustExec(t, db, `ALTER TABLE subscribers DROP COLUMN first_name, DROP COLUMN last_name`)
		mustExec(t, db, `ALTER TABLE subscribers ADD COLUMN name TEXT NOT NULL DEFAULT ''`)
	}
	if preview {
		mustExec(t, db, `ALTER TABLE campaigns DROP COLUMN preview_text`)
	}
}

type forkNameCase struct {
	email, stored          string
	first, last, generated string
}

var forkNameCases = []forkNameCase{
	// Invented by the importer / admin API rule (models.FallbackName).
	{"bkirkpatrick00@example.com", "Bkirkpatrick00", "", "", ""},
	{"john.smith@example.com", "John Smith", "", "", ""},
	{"john_doe@example.com", "John_doe", "", "", ""},
	// Invented by the public subscription form rule (raw local part).
	{"pop.up@example.com", "pop.up", "", "", ""},
	// Supplied by a person.
	{"jsmith@example.com", "Johnny S.", "Johnny", "S.", "Johnny S."},
	{"bkirk00@example.com", "Brady Kirkpatrick", "Brady", "Kirkpatrick", "Brady Kirkpatrick"},
	{"mary@example.com", "Mary Jo Van Der Berg", "Mary", "Jo Van Der Berg", "Mary Jo Van Der Berg"},
	{"c@example.com", "Cher", "Cher", "", "Cher"},
	{"pat@example.com", "  Pat   O'Brien ", "Pat", "O'Brien", "Pat O'Brien"},
	{"empty@example.com", "", "", "", ""},
}

func seedForkNames(t *testing.T, db *sqlx.DB) {
	t.Helper()
	for _, c := range forkNameCases {
		mustExec(t, db, `INSERT INTO subscribers (uuid, email, name) VALUES (gen_random_uuid(), $1, $2)`, c.email, c.stored)
	}
}

func snapshotNames(t *testing.T, db *sqlx.DB) string {
	t.Helper()
	var s string
	if err := db.Get(&s, `SELECT COALESCE(string_agg(id || ':' || first_name || '|' || last_name || '|' || name, ',' ORDER BY id), '') FROM subscribers`); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return s
}

func TestRunForkMigratesPreForkDatabase(t *testing.T) {
	// Exercise several write batches with only a handful of rows.
	orig := forkBatchSize
	forkBatchSize = 3
	t.Cleanup(func() { forkBatchSize = orig })

	db := forkTestDB(t)
	toPreForkSchema(t, db)
	seedForkNames(t, db)

	if pending, err := ForkPending(db); err != nil || !pending {
		t.Fatalf("ForkPending before = %v, %v; want true, nil", pending, err)
	}
	if err := RunFork(db, discard); err != nil {
		t.Fatalf("RunFork: %v", err)
	}
	if pending, err := ForkPending(db); err != nil || pending {
		t.Fatalf("ForkPending after = %v, %v; want false, nil", pending, err)
	}

	for _, c := range forkNameCases {
		var first, last, name string
		if err := db.QueryRow(`SELECT first_name, last_name, name FROM subscribers WHERE email = $1`, c.email).Scan(&first, &last, &name); err != nil {
			t.Fatalf("%s: %v", c.email, err)
		}
		if first != c.first || last != c.last || name != c.generated {
			t.Errorf("%s (%q): got %q / %q / %q, want %q / %q / %q",
				c.email, c.stored, first, last, name, c.first, c.last, c.generated)
		}
	}

	// name is derived now: a direct write must fail loudly.
	if _, err := db.Exec(`UPDATE subscribers SET name = 'x' WHERE email = 'mary@example.com'`); err == nil {
		t.Error("writing subscribers.name succeeded; want an error from the generated column")
	}

	// Campaigns gained preview_text with an empty default.
	var preview string
	if err := db.QueryRow(`INSERT INTO campaigns (uuid, name, subject, from_email, body, messenger)
		VALUES (gen_random_uuid(), 'c', 's', 'from@example.com', 'b', 'email') RETURNING preview_text`).Scan(&preview); err != nil {
		t.Fatalf("insert campaign: %v", err)
	}
	if preview != "" {
		t.Errorf("preview_text default = %q, want empty", preview)
	}
}

func TestRunForkIsIdempotent(t *testing.T) {
	db := forkTestDB(t)
	toPreForkSchema(t, db)
	seedForkNames(t, db)

	if err := RunFork(db, discard); err != nil {
		t.Fatalf("first RunFork: %v", err)
	}
	before := snapshotNames(t, db)

	if err := RunFork(db, discard); err != nil {
		t.Fatalf("second RunFork: %v", err)
	}
	if after := snapshotNames(t, db); after != before {
		t.Errorf("second RunFork changed data:\nbefore %s\nafter  %s", before, after)
	}
}

func TestRunForkRefusesPartialSchema(t *testing.T) {
	db := forkTestDB(t)
	toPreForkSchema(t, db)
	mustExec(t, db, `ALTER TABLE subscribers ADD COLUMN first_name TEXT NOT NULL DEFAULT ''`)

	if err := RunFork(db, discard); err == nil {
		t.Fatal("RunFork on a partial schema succeeded; want an error")
	}
	if pending, err := ForkPending(db); err != nil || !pending {
		t.Errorf("ForkPending on a partial schema = %v, %v; want true, nil", pending, err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/migrations/ -run Fork -v`
Expected: FAIL to compile — `undefined: forkColumns`, `undefined: forkBatchSize`, `undefined: ForkPending`, `undefined: RunFork`.

- [ ] **Step 4: Write the fork step**

Create `internal/migrations/fork_names_preview.go`:

```go
package migrations

import (
	"fmt"
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

// Gunmade fork schema step: subscriber first/last names and campaign preview
// text. Spec: docs/superpowers/specs/2026-09-17-subscriber-names-preview-text-design.md.
//
// This step is deliberately NOT an entry in cmd/upgrade.go's migList. migList
// runs every entry whose semver is above the last recorded version, so a fork
// version number would make a later upstream migration with the same or a
// lower number skip silently. Instead, cmd/upgrade.go calls RunFork on every
// --upgrade and ForkPending on every normal start. Both calls must stay ahead
// of the "nothing pending" early returns in that file: a database already at
// the last migList version takes those paths.

// forkBatchSize is the number of subscribers written per UPDATE. A var so the
// tests can exercise several batches with a few rows.
var forkBatchSize = 5000

// forkColumns reports which of the step's two marker columns exist.
func forkColumns(q sqlx.Queryer) (names, preview bool, err error) {
	const exists = `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2)`

	if err = sqlx.Get(q, &names, exists, "subscribers", "first_name"); err != nil {
		return false, false, err
	}
	if err = sqlx.Get(q, &preview, exists, "campaigns", "preview_text"); err != nil {
		return false, false, err
	}

	return names, preview, nil
}

// ForkPending reports whether the fork step has not been applied.
func ForkPending(db *sqlx.DB) (bool, error) {
	names, preview, err := forkColumns(db)
	if err != nil {
		return false, err
	}

	return !names || !preview, nil
}

// RunFork applies the fork step in one transaction. It is a no-op when both
// marker columns exist, and refuses to guess when only one does.
func RunFork(db *sqlx.DB, lo *log.Logger) error {
	names, preview, err := forkColumns(db)
	if err != nil {
		return fmt.Errorf("checking fork columns: %w", err)
	}
	if names && preview {
		return nil
	}
	if names != preview {
		return fmt.Errorf("partial fork schema (subscribers.first_name=%v, campaigns.preview_text=%v); restore the database from backup",
			names, preview)
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`ALTER TABLE subscribers
		ADD COLUMN first_name TEXT NOT NULL DEFAULT '',
		ADD COLUMN last_name  TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("adding name columns: %w", err)
	}

	// Read everything first: lib/pq cannot run an UPDATE on the connection
	// while a result set is still open.
	var subs []struct {
		ID    int    `db:"id"`
		Email string `db:"email"`
		Name  string `db:"name"`
	}
	if err := tx.Select(&subs, `SELECT id, email, name FROM subscribers ORDER BY id`); err != nil {
		return fmt.Errorf("reading subscribers: %w", err)
	}

	var (
		ids     []int64
		firsts  []string
		lasts   []string
		blanked int
		split   int
	)
	flush := func() error {
		if len(ids) == 0 {
			return nil
		}
		if _, err := tx.Exec(`UPDATE subscribers s SET first_name = u.first_name, last_name = u.last_name
			FROM UNNEST($1::INT[], $2::TEXT[], $3::TEXT[]) AS u(id, first_name, last_name)
			WHERE s.id = u.id`, pq.Array(ids), pq.Array(firsts), pq.Array(lasts)); err != nil {
			return fmt.Errorf("writing names: %w", err)
		}
		ids, firsts, lasts = ids[:0], firsts[:0], lasts[:0]
		return nil
	}

	for _, s := range subs {
		if models.IsFallbackName(s.Name, s.Email) {
			// The new columns already default to ''.
			blanked++
			continue
		}

		first, last := models.SplitName(s.Name)
		if first == "" && last == "" {
			continue
		}

		split++
		ids = append(ids, int64(s.ID))
		firsts = append(firsts, first)
		lasts = append(lasts, last)
		if len(ids) >= forkBatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}

	for _, q := range []string{
		`ALTER TABLE subscribers DROP COLUMN name`,
		// Keep this expression identical to schema.sql.
		`ALTER TABLE subscribers ADD COLUMN name TEXT NOT NULL GENERATED ALWAYS AS (btrim(first_name || ' ' || last_name)) STORED`,
		`ALTER TABLE campaigns ADD COLUMN preview_text TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	lo.Printf("gunmade fork migration applied: %d subscribers, %d fallback names blanked, %d names split into first/last",
		len(subs), blanked, split)

	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/migrations/ -run Fork -v && go build ./... && go vet ./internal/migrations/`
Expected: `TestRunForkMigratesPreForkDatabase`, `TestRunForkIsIdempotent`, `TestRunForkRefusesPartialSchema` PASS (not SKIP — if they skip, `LISTMONK_TEST_DSN` is not exported).

If `DROP COLUMN name` fails with "other objects depend on it", a view or index references `subscribers.name`. Stop and report it; the spec assumes none exists.

- [ ] **Step 6: Commit**

```bash
git add internal/migrations/fork_names_preview.go internal/migrations/fork_names_preview_db_test.go
git commit -m "feat(migrations): fork step for subscriber names and campaign preview text

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Schema and subscriber write paths

**Files:**
- Modify: `schema.sql:20-30` (subscribers), `schema.sql:129` (campaigns)
- Modify: `queries/subscribers.sql` — `insert-subscriber` (~line 86), `upsert-subscriber` (~114), `upsert-blocklist-subscriber` (~137), `update-subscriber` (~151), `update-subscriber-with-lists` (~160)
- Modify: `internal/core/subscribers.go:319-328`, `:373-378`, `:414-425`
- Modify: `internal/subimporter/importer.go:309-313`
- Modify: `cmd/install.go:166-188`
- Modify: `internal/core/dashboard_growth_db_test.go:66` and `:288-369`
- Create: `internal/core/subscriber_names_db_test.go`
- Modify: `internal/migrations/fork_names_preview_db_test.go` (append one test)

**Interfaces:**
- Consumes: `models.Subscriber.FirstName/LastName` (Task 1).
- Produces:
  - Query parameters: `insert-subscriber` `$3`=first_name, `$9`=last_name; `upsert-subscriber` `$3`=first_name, `$9`=last_name; `upsert-blocklist-subscriber` `$3`=first_name, `$5`=last_name; `update-subscriber` `$3`=first_name, `$6`=last_name; `update-subscriber-with-lists` `$3`=first_name, `$12`=last_name. All other parameters keep their numbers.
  - `func newTestDB(t *testing.T, dsn, prefix, fixtures string) (*sqlx.DB, *models.Queries)` in package `core` tests.

- [ ] **Step 1: Extract the shared test database helper**

In `internal/core/dashboard_growth_db_test.go`, replace the whole `growthTestDB` function with:

```go
func growthTestDB(t *testing.T, dsn string) *models.Queries {
	t.Helper()

	_, q := newTestDB(t, dsn, "lm_growth_test", growthFixtures)
	return q
}

// newTestDB creates a throwaway database, installs schema.sql and fixtures
// (which may be empty), and prepares every query in queries/*.sql into
// models.Queries as the app does at startup.
func newTestDB(t *testing.T, dsn, prefix, fixtures string) (*sqlx.DB, *models.Queries) {
	t.Helper()

	admin, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close() })

	name := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create database: %v", err)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + name

	db, err := sqlx.Connect("postgres", u.String())
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(func() {
		db.Close()
		admin.Exec("DROP DATABASE IF EXISTS " + name)
	})

	schema, err := os.ReadFile("../../schema.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("install schema: %v", err)
	}
	if fixtures != "" {
		if _, err := db.Exec(fixtures); err != nil {
			t.Fatalf("fixtures: %v", err)
		}
	}

	files, err := filepath.Glob("../../queries/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("find query files: %v (%d found)", err, len(files))
	}

	qMap := goyesql.Queries{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		mp, err := goyesql.ParseBytes(b)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		maps.Copy(qMap, mp)
	}

	// Mirror the three derived queries that cmd/init.go prepareQueries adds
	// before scanning, with privacy.individual_tracking off. If that function
	// changes, ScanToStruct below fails loudly on the missing query.
	countQuery := qMap["get-campaign-analytics-counts"].Query
	qMap["get-campaign-view-counts"] = &goyesql.Query{
		Query: fmt.Sprintf(countQuery, "campaign_views"),
		Tags:  map[string]string{"name": "get-campaign-view-counts"},
	}
	qMap["get-campaign-click-counts"] = &goyesql.Query{
		Query: fmt.Sprintf(countQuery, "link_clicks"),
		Tags:  map[string]string{"name": "get-campaign-click-counts"},
	}
	qMap["get-campaign-link-counts"].Query = fmt.Sprintf(qMap["get-campaign-link-counts"].Query, "*")

	var q models.Queries
	if err := goyesqlx.ScanToStruct(&q, qMap, db); err != nil {
		t.Fatalf("prepare queries (the app would fail to start): %v", err)
	}

	return db, &q
}
```

Run: `go test ./internal/core/ -run Growth -v`
Expected: PASS (behaviour unchanged; schema is still the old shape).

- [ ] **Step 2: Write the failing query tests**

Create `internal/core/subscriber_names_db_test.go`:

```go
package core

import (
	"os"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

// TestSubscriberNameQueries checks every query that writes subscriber names
// against a real Postgres (see TestDashboardGrowthSQL for LISTMONK_TEST_DSN).
// subscribers.name is a generated column, so these queries must write
// first_name and last_name only, and must save blanks as blanks.
func TestSubscriberNameQueries(t *testing.T) {
	dsn := os.Getenv("LISTMONK_TEST_DSN")
	if dsn == "" {
		t.Skip("LISTMONK_TEST_DSN not set")
	}

	db, q := newTestDB(t, dsn, "lm_names_test", "")

	var (
		noInts  = pq.Array([]int{})
		noUUIDs = pq.Array([]string{})
	)

	insert := func(t *testing.T, email, first, last string) int {
		t.Helper()
		var id int
		if err := q.InsertSubscriber.Get(&id, uuid.Must(uuid.NewV4()).String(), email, first,
			models.SubscriberStatusEnabled, models.JSON{}, noInts, noUUIDs,
			models.SubscriptionStatusUnconfirmed, last); err != nil {
			t.Fatalf("insert-subscriber %s: %v", email, err)
		}
		return id
	}

	t.Run("insert-subscriber", func(t *testing.T) {
		insert(t, "ins@names.test", "Mary", "Jo Smith")
		wantNames(t, db, "ins@names.test", "Mary", "Jo Smith", "Mary Jo Smith")

		insert(t, "blank@names.test", "", "")
		wantNames(t, db, "blank@names.test", "", "", "")
	})

	t.Run("update-subscriber saves blanks", func(t *testing.T) {
		id := insert(t, "upd@names.test", "Ann", "Lee")

		if _, err := q.UpdateSubscriber.Exec(id, "", "", "", "", ""); err != nil {
			t.Fatalf("update-subscriber: %v", err)
		}
		wantNames(t, db, "upd@names.test", "", "", "")

		if _, err := q.UpdateSubscriber.Exec(id, "", "Bo", "", "", ""); err != nil {
			t.Fatalf("update-subscriber: %v", err)
		}
		wantNames(t, db, "upd@names.test", "Bo", "", "Bo")
	})

	t.Run("update-subscriber-with-lists", func(t *testing.T) {
		id := insert(t, "updl@names.test", "Ann", "Lee")

		if _, err := q.UpdateSubscriberWithLists.Exec(id, "", "Cy", "", "", noInts, noUUIDs,
			models.SubscriptionStatusUnconfirmed, false, noInts, false, "Young"); err != nil {
			t.Fatalf("update-subscriber-with-lists: %v", err)
		}
		wantNames(t, db, "updl@names.test", "Cy", "Young", "Cy Young")

		if _, err := q.UpdateSubscriberWithLists.Exec(id, "", "", "", "", noInts, noUUIDs,
			models.SubscriptionStatusUnconfirmed, false, noInts, false, ""); err != nil {
			t.Fatalf("update-subscriber-with-lists: %v", err)
		}
		wantNames(t, db, "updl@names.test", "", "", "")
	})

	t.Run("upsert-subscriber", func(t *testing.T) {
		upsert := func(first, last string, overwrite bool) {
			t.Helper()
			if _, err := q.UpsertSubscriber.Exec(uuid.Must(uuid.NewV4()), "ups@names.test", first, models.JSON{},
				pq.Int64Array{}, models.SubscriptionStatusUnconfirmed, overwrite, true, last); err != nil {
				t.Fatalf("upsert-subscriber: %v", err)
			}
		}

		upsert("Dee", "Dunn", true)
		wantNames(t, db, "ups@names.test", "Dee", "Dunn", "Dee Dunn")

		upsert("Eve", "Ell", true)
		wantNames(t, db, "ups@names.test", "Eve", "Ell", "Eve Ell")

		upsert("Zed", "Zz", false)
		wantNames(t, db, "ups@names.test", "Eve", "Ell", "Eve Ell")
	})

	t.Run("upsert-blocklist-subscriber", func(t *testing.T) {
		if _, err := q.UpsertBlocklistSubscriber.Exec(uuid.Must(uuid.NewV4()), "blk@names.test", "Fay",
			models.JSON{}, "Fox"); err != nil {
			t.Fatalf("upsert-blocklist-subscriber: %v", err)
		}
		wantNames(t, db, "blk@names.test", "Fay", "Fox", "Fay Fox")
	})

	t.Run("name is generated", func(t *testing.T) {
		if _, err := db.Exec(`UPDATE subscribers SET name = 'x' WHERE email = 'ins@names.test'`); err == nil {
			t.Error("writing subscribers.name succeeded; want an error from the generated column")
		}
	})
}

func wantNames(t *testing.T, db *sqlx.DB, email, first, last, name string) {
	t.Helper()

	var gotFirst, gotLast, gotName string
	if err := db.QueryRow(`SELECT first_name, last_name, name FROM subscribers WHERE email = $1`, email).
		Scan(&gotFirst, &gotLast, &gotName); err != nil {
		t.Fatalf("read %s: %v", email, err)
	}
	if gotFirst != first || gotLast != last || gotName != name {
		t.Errorf("%s: got %q / %q / %q, want %q / %q / %q", email, gotFirst, gotLast, gotName, first, last, name)
	}
}
```

Append to `internal/migrations/fork_names_preview_db_test.go`:

```go
// A fresh install gets the fork columns from schema.sql; the step must no-op.
func TestForkNoOpOnFreshInstall(t *testing.T) {
	db := forkTestDB(t)

	if pending, err := ForkPending(db); err != nil || pending {
		t.Fatalf("ForkPending on schema.sql = %v, %v; want false, nil", pending, err)
	}
	if err := RunFork(db, discard); err != nil {
		t.Fatalf("RunFork on schema.sql: %v", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/core/ -run SubscriberNameQueries -v; go test ./internal/migrations/ -run FreshInstall -v`
Expected: `TestSubscriberNameQueries` FAIL (`column "first_name" ... does not exist` or wrong parameter count); `TestForkNoOpOnFreshInstall` FAIL (`ForkPending on schema.sql = true`).

- [ ] **Step 4: Update schema.sql**

In `schema.sql`, replace the subscribers columns:

```sql
    email           TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    attribs         JSONB NOT NULL DEFAULT '{}',
```

with:

```sql
    email           TEXT NOT NULL UNIQUE,
    -- gunmade fork: first/last names are stored and name is derived. Keep the
    -- expression identical to internal/migrations/fork_names_preview.go.
    first_name      TEXT NOT NULL DEFAULT '',
    last_name       TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL GENERATED ALWAYS AS (btrim(first_name || ' ' || last_name)) STORED,
    attribs         JSONB NOT NULL DEFAULT '{}',
```

In the campaigns table, replace:

```sql
    archive_meta        JSONB NOT NULL DEFAULT '{}',
```

with:

```sql
    archive_meta        JSONB NOT NULL DEFAULT '{}',

    -- gunmade fork: inbox preview text. See models/autopreheader.go.
    preview_text        TEXT NOT NULL DEFAULT '',
```

- [ ] **Step 5: Update the five write queries**

In `queries/subscribers.sql`, replace the `insert-subscriber` header and `sub` CTE:

```sql
-- name: insert-subscriber
WITH sub AS (
    INSERT INTO subscribers (uuid, email, name, status, attribs)
    VALUES($1, $2, $3, $4, $5)
    RETURNING id, status
),
```

with:

```sql
-- name: insert-subscriber
-- gunmade fork: $3 is first_name and $9 is last_name. subscribers.name is generated.
WITH sub AS (
    INSERT INTO subscribers (uuid, email, first_name, last_name, status, attribs)
    VALUES($1, $2, $3, $9, $4, $5)
    RETURNING id, status
),
```

Replace the `upsert-subscriber` header and `sub` CTE:

```sql
-- name: upsert-subscriber
-- Upserts a subscriber where existing subscribers get their names and attributes overwritten.
-- If $7 = true, update name/attribs. If $8 = true, update subscription status.
WITH sub AS (
    INSERT INTO subscribers as s (uuid, email, name, attribs, status)
    VALUES($1, $2, $3, $4, 'enabled')
    ON CONFLICT (email)
    DO UPDATE SET
        name=(CASE WHEN $7 THEN $3 ELSE s.name END),
        attribs=(CASE WHEN $7 THEN $4 ELSE s.attribs END),
        updated_at=NOW()
    RETURNING uuid, id, status
),
```

with:

```sql
-- name: upsert-subscriber
-- Upserts a subscriber where existing subscribers get their names and attributes overwritten.
-- If $7 = true, update name/attribs. If $8 = true, update subscription status.
-- gunmade fork: $3 is first_name and $9 is last_name. subscribers.name is generated.
WITH sub AS (
    INSERT INTO subscribers as s (uuid, email, first_name, last_name, attribs, status)
    VALUES($1, $2, $3, $9, $4, 'enabled')
    ON CONFLICT (email)
    DO UPDATE SET
        first_name=(CASE WHEN $7 THEN $3 ELSE s.first_name END),
        last_name=(CASE WHEN $7 THEN $9 ELSE s.last_name END),
        attribs=(CASE WHEN $7 THEN $4 ELSE s.attribs END),
        updated_at=NOW()
    RETURNING uuid, id, status
),
```

Replace the `upsert-blocklist-subscriber` `sub` CTE:

```sql
WITH sub AS (
    INSERT INTO subscribers (uuid, email, name, attribs, status)
    VALUES($1, $2, $3, $4, 'blocklisted')
```

with:

```sql
-- gunmade fork: $3 is first_name and $5 is last_name. subscribers.name is generated.
WITH sub AS (
    INSERT INTO subscribers (uuid, email, first_name, last_name, attribs, status)
    VALUES($1, $2, $3, $5, $4, 'blocklisted')
```

Replace `update-subscriber`:

```sql
-- name: update-subscriber
UPDATE subscribers SET
    email=(CASE WHEN $2 != '' THEN $2 ELSE email END),
    name=(CASE WHEN $3 != '' THEN $3 ELSE name END),
```

with:

```sql
-- name: update-subscriber
-- gunmade fork: $3 is first_name and $6 is last_name, saved as sent (including
-- empty, so a name can be cleared). subscribers.name is generated.
UPDATE subscribers SET
    email=(CASE WHEN $2 != '' THEN $2 ELSE email END),
    first_name=$3,
    last_name=$6,
```

In `update-subscriber-with-lists`, replace:

```sql
        email=(CASE WHEN $2 != '' THEN $2 ELSE email END),
        name=(CASE WHEN $3 != '' THEN $3 ELSE name END),
```

with:

```sql
        email=(CASE WHEN $2 != '' THEN $2 ELSE email END),
        -- gunmade fork: $3 is first_name and $12 is last_name, saved as sent.
        first_name=$3,
        last_name=$12,
```

Then confirm nothing else writes the column:

Run: `grep -rnE "name\s*=|INSERT INTO subscribers" queries/subscribers.sql | grep -v "first_name\|last_name\|list_name\|-- name:"`
Expected: no line that assigns or inserts `subscribers.name`.

- [ ] **Step 6: Pass first and last names from Go**

In `internal/core/subscribers.go` `InsertSubscriber`, replace the `Get` arguments:

```go
	if err = c.q.InsertSubscriber.Get(&sub.ID,
		sub.UUID,
		sub.Email,
		strings.TrimSpace(sub.Name),
		sub.Status,
		sub.Attribs,
		pq.Array(listIDs),
		pq.Array(listUUIDs),
		subStatus); err != nil {
```

with:

```go
	if err = c.q.InsertSubscriber.Get(&sub.ID,
		sub.UUID,
		sub.Email,
		strings.TrimSpace(sub.FirstName),
		sub.Status,
		sub.Attribs,
		pq.Array(listIDs),
		pq.Array(listUUIDs),
		subStatus,
		strings.TrimSpace(sub.LastName)); err != nil {
```

In `UpdateSubscriber`, replace:

```go
	_, err := c.q.UpdateSubscriber.Exec(id,
		sub.Email,
		strings.TrimSpace(sub.Name),
		sub.Status,
		json.RawMessage(attribs),
	)
```

with:

```go
	_, err := c.q.UpdateSubscriber.Exec(id,
		sub.Email,
		strings.TrimSpace(sub.FirstName),
		sub.Status,
		json.RawMessage(attribs),
		strings.TrimSpace(sub.LastName),
	)
```

In `UpdateSubscriberWithLists`, replace:

```go
	_, err := c.q.UpdateSubscriberWithLists.Exec(id,
		sub.Email,
		strings.TrimSpace(sub.Name),
```

with:

```go
	_, err := c.q.UpdateSubscriberWithLists.Exec(id,
		sub.Email,
		strings.TrimSpace(sub.FirstName),
```

and replace its last argument line `		allowResubscribe)` with:

```go
		allowResubscribe,
		strings.TrimSpace(sub.LastName))
```

In `internal/subimporter/importer.go`, replace:

```go
		if s.opt.Mode == ModeSubscribe {
			_, err = stmt.Exec(uu, sub.Email, sub.Name, sub.Attribs, pq.Array(listIDs), s.opt.SubStatus, s.opt.OverwriteUserInfo, s.opt.OverwriteSubStatus)
		} else if s.opt.Mode == ModeBlocklist {
			_, err = stmt.Exec(uu, sub.Email, sub.Name, sub.Attribs)
		}
```

with:

```go
		if s.opt.Mode == ModeSubscribe {
			_, err = stmt.Exec(uu, sub.Email, sub.FirstName, sub.Attribs, pq.Array(listIDs), s.opt.SubStatus, s.opt.OverwriteUserInfo, s.opt.OverwriteSubStatus, sub.LastName)
		} else if s.opt.Mode == ModeBlocklist {
			_, err = stmt.Exec(uu, sub.Email, sub.FirstName, sub.Attribs, sub.LastName)
		}
```

In `cmd/install.go` `installSubs`, replace both `UpsertSubscriber.Exec` calls with:

```go
	if _, err := q.UpsertSubscriber.Exec(
		uuid.Must(uuid.NewV4()),
		"john@example.com",
		"John",
		`{"type": "known", "good": true, "city": "Bengaluru"}`,
		pq.Int64Array{int64(defListID)},
		models.SubscriptionStatusUnconfirmed,
		true, true,
		"Doe"); err != nil {
		lo.Fatalf("Error creating subscriber: %v", err)
	}
	if _, err := q.UpsertSubscriber.Exec(
		uuid.Must(uuid.NewV4()),
		"anon@example.com",
		"Anon",
		`{"type": "unknown", "good": true, "city": "Bengaluru"}`,
		pq.Int64Array{int64(optinListID)},
		models.SubscriptionStatusUnconfirmed,
		true, true,
		"Doe"); err != nil {
		lo.Fatalf("error creating subscriber: %v", err)
	}
```

In `internal/core/dashboard_growth_db_test.go`, change line 66 from:

```sql
INSERT INTO subscribers (id, uuid, email, name, status, created_at, updated_at) VALUES
```

to:

```sql
INSERT INTO subscribers (id, uuid, email, first_name, status, created_at, updated_at) VALUES
```

- [ ] **Step 7: Run the tests to verify they pass**

Run:
```bash
go build ./... && go vet ./internal/... ./models/ \
  && go test ./internal/core/ -run 'SubscriberNameQueries|Growth' -v \
  && go test ./internal/migrations/ -run Fork -v \
  && go test ./...
```
Expected: all PASS, none SKIP for the DB tests.

- [ ] **Step 8: Commit**

```bash
git add schema.sql queries/subscribers.sql internal/core/subscribers.go internal/subimporter/importer.go cmd/install.go internal/core/dashboard_growth_db_test.go internal/core/subscriber_names_db_test.go internal/migrations/fork_names_preview_db_test.go
git commit -m "feat(subscribers): store first/last names; name becomes a generated column

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Name migration dry-run tool

**Files:**
- Create: `scripts/fork/namecheck/main.go`

**Interfaces:**
- Consumes: `models.IsFallbackName`, `models.SplitName`.
- Produces: `go run ./scripts/fork/namecheck -dsn URL`. Output lines that Task 5's script greps for, exactly:
  - `  names to blank (invented from the e-mail): N`
  - `  names to split into first/last: N`
  - `subscribers.first_name already exists: the migration has run. Skipping the name report.`

- [ ] **Step 1: Write the tool**

Create `scripts/fork/namecheck/main.go`:

```go
// Command namecheck is a read-only dry run of the gunmade fork's subscriber
// name migration (internal/migrations/fork_names_preview.go). Run it against a
// copy of the production database before deploying:
//
//	go run ./scripts/fork/namecheck -dsn 'postgres://user:pass@host:5432/listmonk?sslmode=disable'
//
// It reports how many names the migration would blank and split, prints random
// examples of each, and lists every template and unsent campaign that uses the
// subscriber's name, so greetings can be checked before the first send.
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	_ "github.com/lib/pq"
)

const samples = 20

type subscriber struct {
	ID    int    `db:"id"`
	Email string `db:"email"`
	Name  string `db:"name"`
}

type change struct {
	subscriber
	first, last string
}

func main() {
	dsn := flag.String("dsn", os.Getenv("LISTMONK_DSN"), "Postgres connection URL (default $LISTMONK_DSN)")
	flag.Parse()
	if *dsn == "" {
		log.Fatal("-dsn or LISTMONK_DSN is required")
	}

	db, err := sqlx.Connect("postgres", *dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer db.Close()

	tx, err := db.Beginx()
	if err != nil {
		log.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	// Guarantee the tool cannot change anything.
	if _, err := tx.Exec(`SET TRANSACTION READ ONLY`); err != nil {
		log.Fatalf("read-only transaction: %v", err)
	}

	var migrated bool
	if err := tx.Get(&migrated, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'subscribers' AND column_name = 'first_name')`); err != nil {
		log.Fatalf("check schema: %v", err)
	}

	if migrated {
		fmt.Println("subscribers.first_name already exists: the migration has run. Skipping the name report.")
	} else {
		reportNames(tx)
	}

	reportGreetings(tx)
}

func reportNames(tx *sqlx.Tx) {
	var subs []subscriber
	if err := tx.Select(&subs, `SELECT id, email, name FROM subscribers ORDER BY id`); err != nil {
		log.Fatalf("read subscribers: %v", err)
	}

	var (
		blank []change
		split []change
		empty int
	)
	for _, s := range subs {
		switch {
		case models.IsFallbackName(s.Name, s.Email):
			blank = append(blank, change{subscriber: s})
		case strings.TrimSpace(s.Name) == "":
			empty++
		default:
			first, last := models.SplitName(s.Name)
			split = append(split, change{subscriber: s, first: first, last: last})
		}
	}

	fmt.Printf("Subscribers: %d\n", len(subs))
	fmt.Printf("  names to blank (invented from the e-mail): %d\n", len(blank))
	fmt.Printf("  names to split into first/last: %d\n", len(split))
	fmt.Printf("  names already empty: %d\n\n", empty)

	printSample("Names that will be BLANKED", blank)
	printSample("Names that will be SPLIT", split)
}

func printSample(title string, cs []change) {
	n := min(samples, len(cs))
	fmt.Printf("%s (%d random of %d):\n", title, n, len(cs))

	rand.Shuffle(len(cs), func(i, j int) { cs[i], cs[j] = cs[j], cs[i] })
	for _, c := range cs[:n] {
		fmt.Printf("  #%-7d %-40s %-32q -> first=%q last=%q\n", c.ID, c.Email, c.Name, c.first, c.last)
	}
	fmt.Println()
}

func reportGreetings(tx *sqlx.Tx) {
	const pattern = `\.Subscriber\.(Name|FirstName|LastName)`

	var tpls []struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
		Type string `db:"type"`
	}
	if err := tx.Select(&tpls, `SELECT id, name, type FROM templates WHERE body ~ $1 ORDER BY id`, pattern); err != nil {
		log.Fatalf("read templates: %v", err)
	}

	var camps []struct {
		ID     int    `db:"id"`
		Name   string `db:"name"`
		Status string `db:"status"`
	}
	if err := tx.Select(&camps, `SELECT id, name, status FROM campaigns
		WHERE status NOT IN ('finished', 'cancelled')
		AND (body ~ $1 OR subject ~ $1 OR COALESCE(altbody, '') ~ $1)
		ORDER BY id`, pattern); err != nil {
		log.Fatalf("read campaigns: %v", err)
	}

	fmt.Printf("Templates using the subscriber's name: %d\n", len(tpls))
	for _, t := range tpls {
		fmt.Printf("  template #%d %q (%s)\n", t.ID, t.Name, t.Type)
	}
	fmt.Printf("Unsent campaigns using the subscriber's name: %d\n", len(camps))
	for _, c := range camps {
		fmt.Printf("  campaign #%d %q (%s)\n", c.ID, c.Name, c.Status)
	}
	fmt.Println(`
Check that each greeting reads well for a subscriber with no name, for example:
  Hello{{ if .Subscriber.FirstName }} {{ .Subscriber.FirstName }}{{ end }},`)
}
```

- [ ] **Step 2: Verify it builds and runs against a pre-fork database**

Run:
```bash
go vet ./scripts/fork/namecheck/ && go build -o /dev/null ./scripts/fork/namecheck/
"$PGBIN/createdb" -h 127.0.0.1 -p 55432 -U lmtest lm_namecheck
git show f525ea3f:schema.sql | "$PGBIN/psql" -q -h 127.0.0.1 -p 55432 -U lmtest -d lm_namecheck -v ON_ERROR_STOP=1 >/dev/null
"$PGBIN/psql" -q -h 127.0.0.1 -p 55432 -U lmtest -d lm_namecheck -c "INSERT INTO subscribers (uuid, email, name) VALUES
  (gen_random_uuid(), 'pop.up@example.com', 'pop.up'),
  (gen_random_uuid(), 'bkirkpatrick00@example.com', 'Bkirkpatrick00'),
  (gen_random_uuid(), 'real@example.com', 'Real Person')"
go run ./scripts/fork/namecheck -dsn 'postgres://lmtest@127.0.0.1:55432/lm_namecheck?sslmode=disable'
"$PGBIN/dropdb" -h 127.0.0.1 -p 55432 -U lmtest lm_namecheck
```
Expected output contains `names to blank (invented from the e-mail): 2`, `names to split into first/last: 1`, both blanked emails under BLANKED, and `real@example.com ... first="Real" last="Person"` under SPLIT.

- [ ] **Step 3: Commit**

```bash
git add scripts/fork/namecheck/main.go
git commit -m "feat(scripts): read-only dry run of the subscriber name migration

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Run the fork step from the upgrade entry points

**Files:**
- Modify: `cmd/upgrade.go` (`upgrade`, `checkUpgrade`, new `runForkStep`)
- Create: `scripts/fork/upgrade-e2e.sh`

**Interfaces:**
- Consumes: `migrations.RunFork`, `migrations.ForkPending` (Task 2); namecheck output lines (Task 4).
- Produces: normal start refuses with a message containing `gunmade fork database upgrade`; `--upgrade` applies the step even when it logs `no upgrades to run`.

- [ ] **Step 1: Write the failing end-to-end script**

Create `scripts/fork/upgrade-e2e.sh`:

```bash
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

run_old() { (cd "$WORK/old" && "$WORK/listmonk-old" --config '' "$@"); }
run_new() { (cd "$ROOT" && "$WORK/listmonk-new" --config '' "$@"); }

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
run_new >"$WORK/start1.log" 2>&1 &
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
run_new >"$WORK/start2.log" 2>&1 &
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
```

Run: `chmod +x scripts/fork/upgrade-e2e.sh && scripts/fork/upgrade-e2e.sh`
Expected: FAIL at step 3 — `normal start kept running on a pre-fork database` or a failed `refusal message` check, because `cmd/upgrade.go` does not call the fork step yet.

- [ ] **Step 2: Call the fork step before both early returns**

In `cmd/upgrade.go` `upgrade()`, replace:

```go
	// No migrations to run.
	if len(toRun) == 0 {
		lo.Printf("no upgrades to run. Database is up to date.")
		return
	}
```

with:

```go
	// No migrations to run.
	if len(toRun) == 0 {
		lo.Printf("no upgrades to run. Database is up to date.")

		// gunmade fork: the fork step must still run. See runForkStep.
		runForkStep(db)
		return
	}
```

and replace the final line of `upgrade()`:

```go
	lo.Printf("upgrade complete")
}
```

with:

```go
	// gunmade fork: after upstream migrations, which expect the pre-fork schema.
	runForkStep(db)

	lo.Printf("upgrade complete")
}

// runForkStep applies the gunmade fork schema step (subscriber first/last
// names, campaign preview text). It is not a migList entry, so it runs on
// every --upgrade, including when no versioned migration is pending: that is
// the normal state of a production database. See
// internal/migrations/fork_names_preview.go.
func runForkStep(db *sqlx.DB) {
	if err := migrations.RunFork(db, lo); err != nil {
		lo.Fatalf("error running gunmade fork migration: %v", err)
	}
}
```

In `checkUpgrade()`, replace:

```go
	lastVer, toRun, err := getPendingMigrations(db)
	if err != nil {
		lo.Fatalf("error checking migrations: %v", err)
	}

	// No migrations to run.
	if len(toRun) == 0 {
		return
	}
```

with:

```go
	lastVer, toRun, err := getPendingMigrations(db)
	if err != nil {
		lo.Fatalf("error checking migrations: %v", err)
	}

	// gunmade fork: checked before the early return below, which a database at
	// the last migList version always takes.
	if pending, err := migrations.ForkPending(db); err != nil {
		lo.Fatalf("error checking gunmade fork migration: %v", err)
	} else if pending {
		lo.Fatalf("the gunmade fork database upgrade (subscriber first/last names, campaign preview text) is pending. Backup the database and run listmonk --upgrade")
	}

	// No migrations to run.
	if len(toRun) == 0 {
		return
	}
```

- [ ] **Step 3: Run the script to verify it passes**

Run: `go build ./... && scripts/fork/upgrade-e2e.sh`
Expected: six `ok:` lines and `ALL PASSED`.

- [ ] **Step 4: Commit**

```bash
git add cmd/upgrade.go scripts/fork/upgrade-e2e.sh
git commit -m "feat(upgrade): run the fork schema step before the no-pending early returns

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Name input rules in the API, import, public pages and export

**Files:**
- Modify: `internal/subimporter/importer.go:131-134` (headers), `:557-560` (CSV row), `:654-665` (`ValidateFields`)
- Create: `internal/subimporter/names_test.go`
- Modify: `cmd/subscribers.go` (`UpdateSubscriber`, `PatchSubscriber`, export at ~196-208)
- Modify: `models/subscribers.go` (`SubscriberExport`)
- Modify: `queries/subscribers.sql` (`query-subscribers-for-export`)
- Modify: `cmd/public.go` (`SubscriptionPrefs`, `processSubForm`)
- Modify: `static/public/templates/subscription.html:32`

**Interfaces:**
- Consumes: `models.ResolveSubscriberNames`, `models.SplitName` (Task 1); write queries (Task 3).
- Produces: CSV import columns `first_name`, `last_name`; CSV export header `uuid,email,name,first_name,last_name,attributes,status,created_at,updated_at`.

- [ ] **Step 1: Write the failing tests**

Create `internal/subimporter/names_test.go`:

```go
package subimporter

import (
	"io"
	"log"
	"testing"

	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/models"
)

func testImporter(t *testing.T) *Importer {
	t.Helper()

	i, err := i18n.New([]byte(`{"_.code": "en", "_.name": "English", "subscribers.invalidEmail": "Invalid email."}`))
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}

	return New(Options{}, nil, i)
}

// gunmade fork: a subscriber without a name keeps no name. This used to be
// "John Smith", invented from the address.
func TestValidateFieldsKeepsMissingNameBlank(t *testing.T) {
	s, err := testImporter(t).ValidateFields(SubReq{Subscriber: models.Subscriber{Email: "John.Smith@Example.com"}})
	if err != nil {
		t.Fatalf("ValidateFields: %v", err)
	}
	if s.Name != "" || s.FirstName != "" || s.LastName != "" {
		t.Errorf("got %q / %q / %q, want all empty", s.FirstName, s.LastName, s.Name)
	}
	if s.Email != "john.smith@example.com" {
		t.Errorf("email = %q", s.Email)
	}
}

func TestValidateFieldsNames(t *testing.T) {
	cases := []struct {
		label                  string
		in                     models.Subscriber
		first, last, wantName  string
	}{
		{"legacy name is split", models.Subscriber{Name: "Mary Jo Smith"}, "Mary", "Jo Smith", "Mary Jo Smith"},
		{"first/last win", models.Subscriber{Name: "Ignored", FirstName: "Ann", LastName: "Lee"}, "Ann", "Lee", "Ann Lee"},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			c.in.Email = "person@example.com"
			s, err := testImporter(t).ValidateFields(SubReq{Subscriber: c.in})
			if err != nil {
				t.Fatalf("ValidateFields: %v", err)
			}
			if s.FirstName != c.first || s.LastName != c.last || s.Name != c.wantName {
				t.Errorf("got %q / %q / %q, want %q / %q / %q", s.FirstName, s.LastName, s.Name, c.first, c.last, c.wantName)
			}
		})
	}
}

func TestCSVHeadersIncludeFirstAndLastName(t *testing.T) {
	s := &Session{log: log.New(io.Discard, "", 0)}

	got := s.mapCSVHeaders([]string{"email", "first_name", "last_name", "name", "attributes"}, csvHeaders)
	for _, h := range []string{"email", "first_name", "last_name", "name", "attributes"} {
		if _, ok := got[h]; !ok {
			t.Errorf("header %q not recognised", h)
		}
	}
}
```

Run: `go test ./internal/subimporter/ -v`
Expected: FAIL — `TestValidateFieldsKeepsMissingNameBlank` gets `"John Smith"`, `first/last win` gets `"" / "" / "Ignored"`, and `header "first_name" not recognised`.

- [ ] **Step 2: Update the importer**

In `internal/subimporter/importer.go`, replace:

```go
	csvHeaders = map[string]bool{
		"email":      true,
		"name":       true,
		"attributes": true}
```

with:

```go
	csvHeaders = map[string]bool{
		"email":      true,
		"name":       true,
		"first_name": true, // gunmade fork
		"last_name":  true, // gunmade fork
		"attributes": true}
```

Replace:

```go
		if v, ok := row["name"]; ok {
			sub.Name = v
		}
```

with:

```go
		if v, ok := row["name"]; ok {
			sub.Name = v
		}
		// gunmade fork: first_name/last_name win over name in ValidateFields.
		if v, ok := row["first_name"]; ok {
			sub.FirstName = v
		}
		if v, ok := row["last_name"]; ok {
			sub.LastName = v
		}
```

In `ValidateFields`, replace:

```go
	// If there's no name, use the name part of the e-mail.
	s.Name = strings.TrimSpace(s.Name)
	if len(s.Name) == 0 {
		name := strings.ToLower(strings.Split(s.Email, "@")[0])

		parts := strings.Fields(strings.ReplaceAll(name, ".", " "))
		for n, p := range parts {
			parts[n] = cases.Title(language.Und).String(p)
		}

		s.Name = strings.Join(parts, " ")
	}

	return s, nil
```

with:

```go
	// gunmade fork: a subscriber without a name keeps no name. This used to
	// invent one from the e-mail's local part; models.FallbackName keeps that
	// rule only so the fork migration can recognise and blank those names.
	models.ResolveSubscriberNames(&s.Subscriber, nil)

	return s, nil
```

Keep the `cases`/`language` imports: `importer.go` still uses them for the import status subject.

- [ ] **Step 3: Update the admin handlers and export**

In `cmd/subscribers.go` `UpdateSubscriber`, replace:

```go
	if req.Name != "" && !strHasLen(req.Name, 1, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("subscribers.invalidName"))
	}

	// Filter lists against the current user's permitted lists.
	listIDs := user.FilterListsByPerm(auth.PermTypeManage, req.Lists)
```

with:

```go
	// gunmade fork: accept first_name/last_name, or split a legacy name.
	models.ResolveSubscriberNames(&req.Subscriber, nil)
	if req.Name != "" && !strHasLen(req.Name, 1, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("subscribers.invalidName"))
	}

	// Filter lists against the current user's permitted lists.
	listIDs := user.FilterListsByPerm(auth.PermTypeManage, req.Lists)
```

In `PatchSubscriber`, replace:

```go
	if req.Name != "" && !strHasLen(req.Name, 1, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("subscribers.invalidName"))
	}

	// If lists were explicitly sent, replace the existing subscriptions.
```

with:

```go
	// gunmade fork: the request was pre-filled from sub, so compare against it
	// to tell whether the caller sent a legacy name or first/last names.
	models.ResolveSubscriberNames(&req.Subscriber, &sub)
	if req.Name != "" && !strHasLen(req.Name, 1, stdInputMaxLen) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("subscribers.invalidName"))
	}

	// If lists were explicitly sent, replace the existing subscriptions.
```

In the CSV export, replace:

```go
	wr.Write([]string{"uuid", "email", "name", "attributes", "status", "created_at", "updated_at"})
```

with:

```go
	// gunmade fork: first_name and last_name follow name, so an export re-imports as-is.
	wr.Write([]string{"uuid", "email", "name", "first_name", "last_name", "attributes", "status", "created_at", "updated_at"})
```

and:

```go
			if err = wr.Write([]string{r.UUID, r.Email, r.Name, r.Attribs, r.Status,
```

with:

```go
			if err = wr.Write([]string{r.UUID, r.Email, r.Name, r.FirstName, r.LastName, r.Attribs, r.Status,
```

In `models/subscribers.go` `SubscriberExport`, replace:

```go
	Name    string `db:"name" json:"name"`
```

with:

```go
	Name      string `db:"name" json:"name"`
	FirstName string `db:"first_name" json:"first_name"`
	LastName  string `db:"last_name" json:"last_name"`
```

(run `gofmt -w models/subscribers.go` afterwards to realign the struct).

In `queries/subscribers.sql` `query-subscribers-for-export`, replace:

```sql
       subscribers.name,
       subscribers.status,
```

with:

```sql
       subscribers.name,
       subscribers.first_name,
       subscribers.last_name,
       subscribers.status,
```

- [ ] **Step 4: Update the public pages**

In `cmd/public.go` `SubscriptionPrefs`, replace:

```go
	// Manage preferences.
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 256 {
```

with:

```go
	// Manage preferences.
	// gunmade fork: an empty name is valid. Subscribers may have no name, and
	// requiring one would block them from saving their preferences.
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) > 256 {
```

and replace:

```go
	sub.Name = req.Name
```

with:

```go
	// gunmade fork: the page has a single name input.
	sub.Name = req.Name
	sub.FirstName, sub.LastName = "", ""
	models.ResolveSubscriberNames(&sub, nil)
```

In `processSubForm`, replace:

```go
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) == 0 {
		// If there's no name, use the name bit from the e-mail.
		req.Name = strings.Split(req.Email, "@")[0]
	} else if len(req.Name) > stdInputMaxLen {
		return false, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("subscribers.invalidName"))
	}
```

with:

```go
	// gunmade fork: a signup without a name stays nameless. This used to store
	// the e-mail's local part, which greeted people as "Hello jsmith42".
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) > stdInputMaxLen {
		return false, echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("subscribers.invalidName"))
	}
```

and replace:

```go
	// Insert the subscriber into the DB.
	_, hasOptin, err := a.core.InsertSubscriber(models.Subscriber{
		Name:   req.Name,
		Email:  req.Email,
		Status: models.SubscriberStatusEnabled,
	}, nil, listUUIDs, false, true)
```

with:

```go
	// Insert the subscriber into the DB.
	newSub := models.Subscriber{
		Name:   req.Name,
		Email:  req.Email,
		Status: models.SubscriberStatusEnabled,
	}
	models.ResolveSubscriberNames(&newSub, nil)

	_, hasOptin, err := a.core.InsertSubscriber(newSub, nil, listUUIDs, false, true)
```

In `static/public/templates/subscription.html`, replace:

```html
                <input type="text" name="name" value="{{ .Data.Subscriber.Name }}" maxlength="256" required />
```

with:

```html
                <input type="text" name="name" value="{{ .Data.Subscriber.Name }}" maxlength="256" />
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go build ./... && go vet ./cmd/ ./internal/subimporter/ && go test ./internal/subimporter/ ./models/ -v && go test ./... && scripts/fork/upgrade-e2e.sh`
Expected: all PASS; e2e prints `ALL PASSED`.

- [ ] **Step 6: Commit**

```bash
git add internal/subimporter/importer.go internal/subimporter/names_test.go cmd/subscribers.go cmd/public.go models/subscribers.go queries/subscribers.sql static/public/templates/subscription.html
git commit -m "feat(subscribers): first/last name input, no invented names, blank-friendly preferences

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Preheader injection

**Files:**
- Create: `models/autopreheader.go`
- Create: `models/autopreheader_test.go`
- Modify: `models/campaigns.go` (struct field; two calls in `CompileTemplate`)

**Interfaces:**
- Consumes: `tracksAsHTML` (`models/autotrack.go`), `stubFuncs` (`models/autotrack_test.go`).
- Produces: `models.Campaign.PreviewText string` (`db:"preview_text" json:"preview_text"`); `choosePreheaderTarget(c *Campaign, base string) preheaderTarget`; `autoPreheaderHTML(s string) string`.

- [ ] **Step 1: Write the failing tests**

Create `models/autopreheader_test.go`:

```go
package models

import (
	"bytes"
	"strings"
	"testing"
)

const preheaderMarker = `mso-hide:all;">`

func TestAutoPreheaderHTML(t *testing.T) {
	cases := []struct {
		label, in, wantPrefix string
	}{
		{"after body with attributes", `<html><BODY class="x" style="y"><p>Hi</p></body></html>`, `<html><BODY class="x" style="y"><div style=`},
		{"no body tag prepends", `<p>Hi</p>`, `<div style=`},
		{"tbody is not body", `<table><tbody><tr><td>x</td></tr></tbody></table>`, `<div style=`},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			out := autoPreheaderHTML(c.in)
			if !strings.HasPrefix(out, c.wantPrefix) {
				t.Errorf("got %q, want prefix %q", out, c.wantPrefix)
			}
			if n := strings.Count(out, preheaderMarker); n != 1 {
				t.Errorf("preheader blocks = %d, want 1", n)
			}
		})
	}
}

func TestChoosePreheaderTarget(t *testing.T) {
	const (
		docBody  = `<html><body><p>x</p></body></html>`
		fragment = `<p>x</p>`
		include  = `{{ template "content" . }}`
	)
	cases := []struct {
		label string
		c     Campaign
		base  string
		want  preheaderTarget
	}{
		{"empty preview text", Campaign{ContentType: CampaignContentTypeHTML, Body: fragment}, docBody, preheaderNone},
		{"whitespace preview text", Campaign{PreviewText: "  ", ContentType: CampaignContentTypeHTML, Body: fragment}, docBody, preheaderNone},
		{"plain text campaign", Campaign{PreviewText: "p", ContentType: CampaignContentTypePlain, Body: fragment}, docBody, preheaderNone},
		{"template has body", Campaign{PreviewText: "p", ContentType: CampaignContentTypeRichtext, Body: fragment}, docBody, preheaderBase},
		{"content has body, base does not", Campaign{PreviewText: "p", ContentType: CampaignContentTypeHTML, Body: docBody}, include, preheaderContent},
		{"visual document", Campaign{PreviewText: "p", ContentType: CampaignContentTypeVisual, Body: docBody}, include, preheaderContent},
		{"neither has body", Campaign{PreviewText: "p", ContentType: CampaignContentTypeMarkdown, Body: "# hi"}, include, preheaderBase},
		{"author placed it in the template", Campaign{PreviewText: "p", ContentType: CampaignContentTypeRichtext, Body: fragment},
			`<body>{{ .Campaign.PreviewText }}` + include + `</body>`, preheaderNone},
		{"author placed it in the content", Campaign{PreviewText: "p", ContentType: CampaignContentTypeVisual,
			Body: `<body><span>{{ .Campaign.PreviewText }}</span></body>`}, include, preheaderNone},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			if got := choosePreheaderTarget(&c.c, c.base); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// renderWithCampaign compiles c and renders it with the data shape the manager
// uses, so {{ .Campaign.PreviewText }} resolves.
func renderWithCampaign(t *testing.T, c *Campaign) string {
	t.Helper()
	if err := c.CompileTemplate(stubFuncs()); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	var b bytes.Buffer
	if err := c.Tpl.ExecuteTemplate(&b, BaseTpl, map[string]any{"Campaign": c}); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	return b.String()
}

func TestCompileTemplatePreheaderInTemplate(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeRichtext,
		PreviewText:  "Big news & <deals>",
		TemplateBody: `<html><body style="margin:0"><div>{{ template "content" . }}</div></body></html>`,
		Body:         `<p>Hello</p>`,
	}
	out := renderWithCampaign(t, c)

	if n := strings.Count(out, preheaderMarker); n != 1 {
		t.Fatalf("preheader blocks = %d, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, `<body style="margin:0"><div style="display:none;`) {
		t.Errorf("preheader not directly after <body>:\n%s", out)
	}
	if !strings.Contains(out, "Big news &amp; &lt;deals&gt;") {
		t.Errorf("preview text not HTML-escaped:\n%s", out)
	}
	if strings.Index(out, "Big news") > strings.Index(out, "Hello") {
		t.Errorf("preview text is not before the content:\n%s", out)
	}
}

// A visual campaign never renders its template: the block must land inside the
// visual document, and a template that mentions PreviewText must not count.
func TestCompileTemplatePreheaderVisual(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeVisual,
		PreviewText:  "Visual preview",
		TemplateBody: `<html><body>{{ .Campaign.PreviewText }}{{ template "content" . }}</body></html>`,
		Body:         `<!doctype html><html><body><p>Newsletter</p></body></html>`,
	}
	out := renderWithCampaign(t, c)

	if n := strings.Count(out, "Visual preview"); n != 1 {
		t.Fatalf("preview text occurrences = %d, want 1:\n%s", n, out)
	}
	if strings.Index(out, "Visual preview") < strings.Index(out, "<body>") {
		t.Errorf("preheader placed before the visual document's <body>:\n%s", out)
	}
}

// An html campaign carrying a full document with no template must not get the
// block in front of its doctype.
func TestCompileTemplatePreheaderHTMLDocumentWithoutTemplate(t *testing.T) {
	c := &Campaign{
		ContentType: CampaignContentTypeHTML,
		PreviewText: "Doc preview",
		Body:        `<!doctype html><html><body><p>Doc</p></body></html>`,
	}
	out := renderWithCampaign(t, c)

	if !strings.HasPrefix(out, "<!doctype html>") {
		t.Errorf("output does not start with the doctype:\n%s", out)
	}
	if n := strings.Count(out, "Doc preview"); n != 1 {
		t.Errorf("preview text occurrences = %d, want 1:\n%s", n, out)
	}
}

func TestCompileTemplateNoPreheaderWhenEmpty(t *testing.T) {
	c := &Campaign{
		ContentType:  CampaignContentTypeRichtext,
		TemplateBody: `<html><body>{{ template "content" . }}</body></html>`,
		Body:         `<p>Hello</p>`,
	}
	if out := renderWithCampaign(t, c); strings.Contains(out, preheaderMarker) {
		t.Errorf("preheader injected with empty preview text:\n%s", out)
	}
}

func TestCompileTemplateNoPreheaderForPlain(t *testing.T) {
	c := &Campaign{
		ContentType: CampaignContentTypePlain,
		PreviewText: "ignored",
		Body:        "Hello",
	}
	if out := renderWithCampaign(t, c); strings.Contains(out, "ignored") {
		t.Errorf("preheader injected into a plain text campaign:\n%s", out)
	}
}
```

Run: `go test ./models/ -run 'Preheader' -v`
Expected: FAIL to compile — `unknown field PreviewText`, `undefined: autoPreheaderHTML`, `undefined: choosePreheaderTarget`.

- [ ] **Step 2: Add the field**

In `models/campaigns.go` `Campaign`, after the `Subject` field, add:

```go
	// gunmade fork: inbox preview text. See models/autopreheader.go.
	PreviewText string `db:"preview_text" json:"preview_text"`
```

(run `gofmt -w models/campaigns.go`).

- [ ] **Step 3: Write the injector**

Create `models/autopreheader.go`:

```go
package models

import (
	"regexp"
	"strings"
)

// Campaign preview text (gunmade fork).
//
// Inboxes show a short line after the subject, taken from the first text in
// the message. preheaderHTML puts the campaign's preview_text there and hides
// it in the opened email. The zero-width padding stops the inbox from running
// on into the start of the body copy.
//
// Injection happens at compile time from the campaign's own column, so it
// reaches every existing draft, clone and visual campaign without editing
// stored templates or bodies. models/autounsubscribe.go uses the same approach
// for the unsubscribe footer.

const preheaderPad = `&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;` +
	`&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;`

// html/template escapes the value, so preview text cannot inject markup.
const preheaderHTML = `<div style="display:none;font-size:1px;color:#ffffff;line-height:1px;` +
	`max-height:0;max-width:0;opacity:0;overflow:hidden;mso-hide:all;">` +
	`{{ .Campaign.PreviewText }}` + preheaderPad + `</div>`

var reBodyOpen = regexp.MustCompile(`(?i)<body\b[^>]*>`)

type preheaderTarget int

const (
	preheaderNone preheaderTarget = iota
	preheaderBase
	preheaderContent
)

// hasPreheader reports whether an author already placed the preview text.
func hasPreheader(sources ...string) bool {
	for _, s := range sources {
		if strings.Contains(s, ".Campaign.PreviewText") {
			return true
		}
	}

	return false
}

// choosePreheaderTarget decides where the preview text goes. base is the base
// template body as CompileTemplate will parse it. The block belongs right after
// the first opening <body> tag the email contains: the base template's, else
// the campaign content's. With neither, it leads the base template. Visual
// campaigns arrive with base already replaced by the bare content include, so
// their attached template never counts.
func choosePreheaderTarget(c *Campaign, base string) preheaderTarget {
	if strings.TrimSpace(c.PreviewText) == "" || !tracksAsHTML(c.ContentType) {
		return preheaderNone
	}
	if hasPreheader(base, c.Body) {
		return preheaderNone
	}
	if reBodyOpen.MatchString(base) {
		return preheaderBase
	}
	if reBodyOpen.MatchString(c.Body) {
		return preheaderContent
	}

	return preheaderBase
}

// autoPreheaderHTML inserts the block immediately after the first opening
// <body> tag, or at the start when there is none.
func autoPreheaderHTML(s string) string {
	if loc := reBodyOpen.FindStringIndex(s); loc != nil {
		return s[:loc[1]] + preheaderHTML + s[loc[1]:]
	}

	return preheaderHTML + s
}
```

- [ ] **Step 4: Call it from CompileTemplate**

In `models/campaigns.go` `CompileTemplate`, replace:

```go
	if body == "" || c.ContentType == CampaignContentTypeVisual {
		body = `{{ template "content" . }}`
	}

	// gunmade fork: add the open pixel and track the template's own links unless
```

with:

```go
	if body == "" || c.ContentType == CampaignContentTypeVisual {
		body = `{{ template "content" . }}`
	}

	// gunmade fork: preview text goes after the first <body> the email will
	// contain. Decided here, before other injectors add markup. See
	// models/autopreheader.go.
	preheaderIn := choosePreheaderTarget(c, body)
	if preheaderIn == preheaderBase {
		body = autoPreheaderHTML(body)
	}

	// gunmade fork: add the open pixel and track the template's own links unless
```

and replace:

```go
	// gunmade fork: track the links in the campaign's own content. The pixel is
	// not added here — it belongs once, in the base template above.
	if tracksAsHTML(c.ContentType) {
		body = autoTrackLinks(body)
	}
```

with:

```go
	// gunmade fork: track the links in the campaign's own content. The pixel is
	// not added here — it belongs once, in the base template above.
	if tracksAsHTML(c.ContentType) {
		body = autoTrackLinks(body)
	}

	// gunmade fork: preview text inside the campaign's own document.
	if preheaderIn == preheaderContent {
		body = autoPreheaderHTML(body)
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./models/ -v && go build ./...`
Expected: all `Preheader` tests PASS, and the existing `autotrack`, `autounsubscribe` and `visual_unsub` tests still PASS.

- [ ] **Step 6: Commit**

```bash
git add models/autopreheader.go models/autopreheader_test.go models/campaigns.go
git commit -m "feat(campaigns): inject preview text after the first body tag at compile time

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Save preview text and edit it in the admin

**Files:**
- Modify: `queries/campaigns.sql` (`create-campaign` lines 2-44, `update-campaign` ~507-534)
- Modify: `internal/core/campaigns.go:173-195`, `:215-234`
- Modify: `cmd/campaigns.go` (`validateCampaignFields` ~809-812, `TestCampaign` ~580-582)
- Create: `internal/core/campaign_preview_db_test.go`
- Modify: `frontend/src/views/Campaign.vue` (field ~75-78, form data ~375, `sendTest`, `createCampaign`, `updateCampaign`)
- Modify: `frontend/src/views/Campaigns.vue` (`cloneCampaign`)
- Modify: `i18n/en.json`

**Interfaces:**
- Consumes: `models.Campaign.PreviewText` (Task 7); `newTestDB` (Task 3).
- Produces: `create-campaign` `$22` = preview_text; `update-campaign` `$21` = preview_text. i18n keys `campaigns.previewText`, `campaigns.previewTextHelp`, `campaigns.fieldInvalidPreviewText`.

- [ ] **Step 1: Write the failing query test**

Create `internal/core/campaign_preview_db_test.go`:

```go
package core

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// TestCampaignPreviewTextQueries checks that create-campaign and
// update-campaign persist preview_text, and that SELECT campaigns.* maps it
// onto models.Campaign (see TestDashboardGrowthSQL for LISTMONK_TEST_DSN).
func TestCampaignPreviewTextQueries(t *testing.T) {
	dsn := os.Getenv("LISTMONK_TEST_DSN")
	if dsn == "" {
		t.Skip("LISTMONK_TEST_DSN not set")
	}

	db, q := newTestDB(t, dsn, "lm_preview_test", "")

	var id int
	if err := q.CreateCampaign.Get(&id,
		uuid.Must(uuid.NewV4()), models.CampaignTypeRegular, "Preview test", "Subject", "from@example.com",
		"<p>Body</p>", null.String{}, models.CampaignContentTypeHTML, null.Time{},
		models.Headers{}, models.JSON{}, pq.StringArray{}, "email", null.Int{},
		pq.Array([]int{}), false, null.String{}, null.Int{}, json.RawMessage("{}"),
		pq.Array([]int{}), null.String{},
		"First look inside"); err != nil {
		t.Fatalf("create-campaign: %v", err)
	}

	read := func() string {
		t.Helper()
		var c models.Campaign
		if err := db.Unsafe().Get(&c, `SELECT campaigns.* FROM campaigns WHERE id = $1`, id); err != nil {
			t.Fatalf("read campaign: %v", err)
		}
		return c.PreviewText
	}

	if got := read(); got != "First look inside" {
		t.Errorf("after create: preview_text = %q", got)
	}

	if _, err := q.UpdateCampaign.Exec(id,
		"Preview test", "Subject", "from@example.com", "<p>Body</p>", null.String{},
		models.CampaignContentTypeHTML, null.Time{}, models.Headers{}, models.JSON{}, pq.StringArray{},
		"email", null.Int{}, pq.Array([]int{}), false, null.String{}, null.Int{}, json.RawMessage("{}"),
		pq.Array([]int{}), null.String{},
		"Updated preview"); err != nil {
		t.Fatalf("update-campaign: %v", err)
	}

	if got := read(); got != "Updated preview" {
		t.Errorf("after update: preview_text = %q", got)
	}
}
```

Run: `go test ./internal/core/ -run CampaignPreviewText -v`
Expected: FAIL — `got 22 parameters but the statement requires 21` (or `21 ... requires 20`).

- [ ] **Step 2: Update the queries**

In `queries/campaigns.sql` `create-campaign`, replace:

```sql
    INSERT INTO campaigns (uuid, type, name, subject, from_email, body, altbody,
        content_type, send_at, headers, attribs, tags, messenger, template_id, to_send,
        max_subscriber_id, archive, archive_slug, archive_template_id, archive_meta, body_source)
```

with:

```sql
    -- gunmade fork: $22 is preview_text.
    INSERT INTO campaigns (uuid, type, name, subject, from_email, body, altbody,
        content_type, send_at, headers, attribs, tags, messenger, template_id, to_send,
        max_subscriber_id, archive, archive_slug, archive_template_id, archive_meta, body_source,
        preview_text)
```

and replace:

```sql
            -- body_source
            COALESCE($21, (SELECT body_source FROM tpl))
        RETURNING id
```

with:

```sql
            -- body_source
            COALESCE($21, (SELECT body_source FROM tpl)),
            -- preview_text
            $22
        RETURNING id
```

In `update-campaign`, replace:

```sql
        body_source=$20,
        updated_at=NOW()
```

with:

```sql
        body_source=$20,
        -- gunmade fork
        preview_text=$21,
        updated_at=NOW()
```

- [ ] **Step 3: Pass and validate the value**

In `internal/core/campaigns.go` `CreateCampaign`, replace:

```go
		pq.Array(mediaIDs),
		o.BodySource,
	); err != nil {
```

with:

```go
		pq.Array(mediaIDs),
		o.BodySource,
		o.PreviewText,
	); err != nil {
```

In `UpdateCampaign`, replace:

```go
		pq.Array(mediaIDs),
		o.BodySource)
```

with:

```go
		pq.Array(mediaIDs),
		o.BodySource,
		o.PreviewText)
```

In `cmd/campaigns.go` `validateCampaignFields`, replace:

```go
	// Larger char limit for subject as it can contain {{ go templating }} logic.
	if !strHasLen(c.Subject, 1, 5000) {
		return c, errors.New(a.i18n.T("campaigns.fieldInvalidSubject"))
	}
```

with:

```go
	// Larger char limit for subject as it can contain {{ go templating }} logic.
	if !strHasLen(c.Subject, 1, 5000) {
		return c, errors.New(a.i18n.T("campaigns.fieldInvalidSubject"))
	}

	// gunmade fork: inbox preview text. See models/autopreheader.go.
	c.PreviewText = strings.TrimSpace(c.PreviewText)
	if utf8.RuneCountInString(c.PreviewText) > 500 {
		return c, errors.New(a.i18n.T("campaigns.fieldInvalidPreviewText"))
	}
```

Add `"unicode/utf8"` to the imports of `cmd/campaigns.go`.

In `TestCampaign`, replace:

```go
	camp.Name = req.Name
	camp.Subject = req.Subject
```

with:

```go
	camp.Name = req.Name
	camp.Subject = req.Subject
	camp.PreviewText = req.PreviewText // gunmade fork
```

In `i18n/en.json`, insert (keeping keys in alphabetical order):

- after `"campaigns.fieldInvalidName": "Invalid length for name.",`:
  ```json
      "campaigns.fieldInvalidPreviewText": "Preview text must be 500 characters or fewer.",
  ```
- after `"campaigns.preview": "Preview",`:
  ```json
      "campaigns.previewText": "Preview text",
      "campaigns.previewTextHelp": "Shown after the subject line in most inboxes. Leave blank to omit.",
  ```

Run: `jq -e . i18n/en.json >/dev/null && go build ./... && go test ./internal/core/ -run 'CampaignPreviewText|SubscriberNameQueries|Growth' -v`
Expected: valid JSON; all three tests PASS.

- [ ] **Step 4: Add the admin field**

In `frontend/src/views/Campaign.vue`, after the subject `b-field`:

```vue
                <b-field :label="$t('campaigns.subject')" label-position="on-border">
                  <b-input :maxlength="5000" v-model="form.subject" name="subject" :disabled="!canEdit"
                    :placeholder="$t('campaigns.subject')" required />
                </b-field>
```

add:

```vue
                <!-- gunmade fork: inbox preview text. Ignored for plain text campaigns. -->
                <b-field v-if="form.content.contentType !== 'plain'" :label="$t('campaigns.previewText')"
                  label-position="on-border" :message="$t('campaigns.previewTextHelp')">
                  <b-input :maxlength="500" v-model="form.previewText" name="preview_text" :disabled="!canEdit"
                    :placeholder="$t('campaigns.previewText')" />
                </b-field>
```

In the `form` data object, after `subject: '',` add `previewText: '',`.

In each of `sendTest()`, `createCampaign()` and `updateCampaign()`, after the line `subject: this.form.subject,` add:

```js
        preview_text: this.form.previewText,
```

In `frontend/src/views/Campaigns.vue` `cloneCampaign`, after `subject: c.subject,` add:

```js
        preview_text: c.previewText,
```

Run: `cd frontend && yarn install --frozen-lockfile && yarn lint && cd ..`
Expected: lint passes with no errors.

- [ ] **Step 5: Commit**

```bash
git add queries/campaigns.sql internal/core/campaigns.go cmd/campaigns.go internal/core/campaign_preview_db_test.go frontend/src/views/Campaign.vue frontend/src/views/Campaigns.vue i18n/en.json
git commit -m "feat(campaigns): save and edit preview text; clone and test send carry it

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Subscriber form first and last name inputs

**Files:**
- Modify: `frontend/src/views/SubscriberForm.vue` (title ~9, columns ~26-46, form data ~197-202, payloads ~260-302)
- Modify: `i18n/en.json`

**Interfaces:**
- Consumes: API fields `first_name`/`last_name` (Task 6); response keys `firstName`/`lastName`.
- Produces: i18n keys `subscribers.firstName`, `subscribers.lastName`.

- [ ] **Step 1: Add the i18n keys**

In `i18n/en.json`:

- after `"subscribers.export": "Export",` add:
  ```json
      "subscribers.firstName": "First name",
  ```
- after `"subscribers.invalidName": "Invalid name.",` add:
  ```json
      "subscribers.lastName": "Last name",
  ```

Run: `jq -e . i18n/en.json >/dev/null`
Expected: exit 0.

- [ ] **Step 2: Replace the name input**

In `frontend/src/views/SubscriberForm.vue`, replace:

```vue
        <div class="columns">
          <div class="column is-8">
            <b-field :label="$t('globals.fields.name')" label-position="on-border">
              <b-input :maxlength="200" v-model="form.name" name="name" :placeholder="$t('globals.fields.name')" />
            </b-field>
          </div>
          <div class="column is-4">
```

with:

```vue
        <!-- gunmade fork: names are stored as first and last name; both are optional. -->
        <div class="columns">
          <div class="column is-4">
            <b-field :label="$t('subscribers.firstName')" label-position="on-border">
              <b-input :maxlength="200" v-model="form.firstName" name="first_name"
                :placeholder="$t('subscribers.firstName')" />
            </b-field>
          </div>
          <div class="column is-4">
            <b-field :label="$t('subscribers.lastName')" label-position="on-border">
              <b-input :maxlength="200" v-model="form.lastName" name="last_name"
                :placeholder="$t('subscribers.lastName')" />
            </b-field>
          </div>
          <div class="column is-4">
```

Replace the modal title line `{{ data.name }}` with `{{ data.name || data.email }}`.

In `data()`, change the `form` object to:

```js
      form: {
        firstName: '',
        lastName: '',
        lists: [],
        strAttribs: '{}',
        status: 'enabled',
        preconfirm: false,
      },
```

In both `createSubscriber()` and `updateSubscriber()`, replace:

```js
        name: this.form.name,
```

with:

```js
        first_name: this.form.firstName,
        last_name: this.form.lastName,
```

Replace both toasts:

```js
        this.$utils.toast(this.$t('globals.messages.created', { name: d.name }));
```
```js
        this.$utils.toast(this.$t('globals.messages.updated', { name: d.name }));
```

with:

```js
        this.$utils.toast(this.$t('globals.messages.created', { name: d.name || d.email }));
```
```js
        this.$utils.toast(this.$t('globals.messages.updated', { name: d.name || d.email }));
```

- [ ] **Step 3: Lint**

Run: `cd frontend && yarn lint && cd ..`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/views/SubscriberForm.vue i18n/en.json
git commit -m "feat(admin): first and last name inputs on the subscriber form

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Greetings and template docs

**Files:**
- Modify: `static/email-templates/default-visual.tpl:15`, `static/email-templates/default-visual.json:22`, `static/email-templates/sample-tx.tpl:91`, `static/email-templates/subscriber-optin.html:4`
- Modify: `cmd/install.go:250`
- Modify: `docs/docs/content/templating.md:33-34`

**Interfaces:**
- Consumes: `Subscriber.FirstName` field (Task 1).
- Produces: nothing new.

- [ ] **Step 1: Update the greetings**

`static/email-templates/default-visual.tpl` — replace `Hello {{ .Subscriber.Name }}` with:

```
Hello{{ if .Subscriber.FirstName }} {{ .Subscriber.FirstName }}{{ end }}
```

`static/email-templates/default-visual.json` — replace `"text": "Hello {{ .Subscriber.Name }}",` with:

```json
        "text": "Hello{{ if .Subscriber.FirstName }} {{ .Subscriber.FirstName }}{{ end }}",
```

`static/email-templates/sample-tx.tpl` — replace `<p>Hello {{ .Subscriber.Name }}</p>` with:

```html
<p>Hello{{ if .Subscriber.FirstName }} {{ .Subscriber.FirstName }}{{ end }}</p>
```

`static/email-templates/subscriber-optin.html` — replace:

```html
<p>{{ L.Ts "email.optin.confirmSubWelcome" }} {{ .Subscriber.FirstName }}</p>
```

with:

```html
<p>{{ L.Ts "email.optin.confirmSubWelcome" }}{{ if .Subscriber.FirstName }} {{ .Subscriber.FirstName }}{{ end }}</p>
```

`cmd/install.go` — replace `<h3>Hi {{ .Subscriber.FirstName }}!</h3>` with:

```html
<h3>Hi{{ if .Subscriber.FirstName }} {{ .Subscriber.FirstName }}{{ end }}!</h3>
```

`docs/docs/content/templating.md` — replace the two table rows:

```
| `{{ .Subscriber.FirstName }}` | First name of the subscriber (automatically extracted from the name)                         |
| `{{ .Subscriber.LastName }}`  | Last name of the subscriber (automatically extracted from the name)                          |
```

with:

```
| `{{ .Subscriber.FirstName }}` | First name of the subscriber. Empty when unknown; use `{{ if .Subscriber.FirstName }}`       |
| `{{ .Subscriber.LastName }}`  | Last name of the subscriber. Empty when unknown                                              |
```

- [ ] **Step 2: Verify**

Run:
```bash
jq -e . static/email-templates/default-visual.json >/dev/null
grep -rn "Subscriber.Name }}" static/email-templates/ cmd/install.go || echo "no bare Name greetings left"
go build ./... && go test ./models/ -run NameFields -v
```
Expected: valid JSON; `no bare Name greetings left`; `TestSubscriberNameFieldsInTemplates` PASS (it covers this exact idiom with blank and set names).

- [ ] **Step 3: Commit**

```bash
git add static/email-templates/default-visual.tpl static/email-templates/default-visual.json static/email-templates/sample-tx.tpl static/email-templates/subscriber-optin.html cmd/install.go docs/docs/content/templating.md
git commit -m "feat(templates): greetings read well when a subscriber has no first name

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Patch record, full verification and review gate

**Files:**
- Modify: `PATCHES.md` (rule 2 paragraph; new section at the end)

**Interfaces:**
- Consumes: everything above.
- Produces: a branch ready for review. **No merge, no push to `gunmade`.**

- [ ] **Step 1: Record the change in PATCHES.md**

In `PATCHES.md` rule 2, after the paragraph ending `fix up the recorded version in the \`settings\` table.`, add:

```markdown
   **Second recorded exception (Sep 2026):** subscriber first/last names and
   campaign preview text alter two upstream tables. `subscribers.name` becomes a
   generated column and `campaigns.preview_text` is added. It deliberately does
   **not** use a `migList` version; see "Subscriber names and preview text" below.
```

Append to the end of `PATCHES.md`:

````markdown
## Subscriber names and preview text (Sep 2026)

Spec: `docs/superpowers/specs/2026-09-17-subscriber-names-preview-text-design.md`.
Plan: `docs/superpowers/plans/2026-09-17-subscriber-names-preview-text.md`.

**What it does.** Subscribers store `first_name` and `last_name`.
`subscribers.name` is now `GENERATED ALWAYS AS (btrim(first_name || ' ' || last_name)) STORED`,
so every upstream read of `name` still works and any write to it fails loudly.
listmonk no longer invents a name from the email address (the importer/admin
rule and the public form rule are both removed), and the migration blanks the
names those rules made. Campaigns get a preview text (inbox preheader),
injected after the first `<body>` at compile time.

**Schema step, and why it is not in `migList`.** `cmd/upgrade.go` runs every
`migList` entry above the last recorded version; a fork version would make a
later upstream migration with the same or a lower number skip silently. The
step lives in `internal/migrations/fork_names_preview.go` (`RunFork`,
`ForkPending`). `upgrade()` calls it on every `--upgrade`, and `checkUpgrade()`
checks it on every start, **both before their "nothing pending" early returns**
— a database at the last `migList` version always takes those returns.

New files (no rebase risk): `models/names.go`, `models/names_test.go`,
`models/autopreheader.go`, `models/autopreheader_test.go`,
`internal/migrations/fork_names_preview.go` (+ `_db_test.go`),
`internal/core/subscriber_names_db_test.go`,
`internal/core/campaign_preview_db_test.go`,
`internal/subimporter/names_test.go`, `scripts/fork/namecheck/main.go`,
`scripts/fork/upgrade-e2e.sh`.

| Modified file | Change | If it conflicts |
| --- | --- | --- |
| `schema.sql` | `first_name`, `last_name`, generated `name`; `campaigns.preview_text` | Re-add; the expression must match `fork_names_preview.go`. |
| `queries/subscribers.sql` | Five write queries write `first_name` (`$3`) and `last_name` (appended last); export selects both | Re-apply. **Grep any new upstream query for writes to `subscribers.name`** — they fail at runtime. |
| `queries/campaigns.sql` | `create-campaign` `$22`, `update-campaign` `$21` = `preview_text` | Re-append as the last parameter. |
| `models/subscribers.go` | `FirstName`/`LastName` fields replace upstream's guessing methods | Keep the fields; a method of the same name will not compile. |
| `models/campaigns.go` | `PreviewText` field; two `preheaderIn` calls in `CompileTemplate` | Re-add: the first right after the base body is chosen, the second after content link tracking. Both before `regTplFuncs`. |
| `internal/core/subscribers.go`, `internal/core/campaigns.go` | Extra query arguments | Re-add. |
| `internal/subimporter/importer.go` | CSV `first_name`/`last_name`; statements; `ValidateFields` no longer invents names | Re-apply. |
| `cmd/upgrade.go` | `runForkStep` in both paths of `upgrade()`; `ForkPending` in `checkUpgrade()` | Re-add **before** the early returns. `scripts/fork/upgrade-e2e.sh` proves it. |
| `cmd/subscribers.go`, `cmd/public.go`, `cmd/install.go`, `cmd/campaigns.go` | Name resolution, blank-friendly preferences, export columns, preview text validation and test send | Re-apply. |
| `static/...`, `docs/docs/content/templating.md` | Greetings tolerate blank first names; preferences name input not `required` | Re-apply. |
| `frontend/src/views/SubscriberForm.vue`, `Campaign.vue`, `Campaigns.vue`, `i18n/en.json` | First/last inputs; preview text input, payloads, clone | Re-add. |

**Before deploying.** Back up `listmonk-data` (the step drops and re-creates a
column), run `go run ./scripts/fork/namecheck -dsn ...` against a copy of
production, and edit any greeting it lists. Coolify deploys every push to
`gunmade`, so merging is deploying.

**Tests.**

```sh
LISTMONK_TEST_DSN='postgres://lmtest@127.0.0.1:55432/lmtest?sslmode=disable' go test ./...
scripts/fork/upgrade-e2e.sh
```
````

- [ ] **Step 2: Full automated verification**

Run each command and record the result:

```bash
gofmt -l models internal cmd scripts          # expect no output
go build ./... && go vet ./...
go test ./...                                 # with LISTMONK_TEST_DSN exported; DB tests must PASS, not SKIP
go test ./... -run 'Fork|SubscriberNameQueries|CampaignPreviewText|Growth' -v 2>&1 | grep -E '^(=== RUN|--- (PASS|FAIL|SKIP))' 
scripts/fork/upgrade-e2e.sh                   # expect ALL PASSED
cd frontend && yarn lint && yarn build && cd ..
make dist                                     # expect success, as recorded in PATCHES.md "Verification performed"
```

Expected: every command succeeds; no `--- SKIP` and no `--- FAIL` lines.

- [ ] **Step 3: Manual check in a running app**

Build and start the new binary against a fresh local database:

```bash
"$PGBIN/createdb" -h 127.0.0.1 -p 55432 -U lmtest lm_manual
export LISTMONK_db__host=127.0.0.1 LISTMONK_db__port=55432 LISTMONK_db__user=lmtest LISTMONK_db__password=unused \
  LISTMONK_db__database=lm_manual LISTMONK_db__ssl_mode=disable LISTMONK_app__address=127.0.0.1:9000 \
  LISTMONK_ADMIN_USER=admin LISTMONK_ADMIN_PASSWORD=admin-password-123
./listmonk --config '' --install --idempotent --yes && ./listmonk --config ''
```

Open `http://127.0.0.1:9000/admin` and check each item. Record pass/fail for each:

1. Subscribers → New: the form shows First name and Last name. Save with both blank; the list shows an empty name, and the toast shows the email.
2. Edit that subscriber: set First "Mary", Last "Jo Smith"; the list shows "Mary Jo Smith".
3. Import a CSV with header `email,first_name,last_name` and one with `email,name`, and one with `email` only. Names are stored as sent, split, and blank respectively.
4. Export subscribers: the header is `uuid,email,name,first_name,last_name,attributes,status,created_at,updated_at`.
5. Public form at `/subscription/form`: subscribe with no name. The subscriber has a blank name, not the email prefix.
6. Open a subscriber's manage-preferences link, clear the name, save. The page saves with no error.
7. Campaigns → New: the Preview text field is under Subject. Save, reload: the value persists. Preview the campaign and view the frame source: the hidden `<div style="display:none;...mso-hide:all;">` with the text is directly after `<body ...>`.
8. Switch a campaign to the visual editor with preview text set. Preview: the block is inside the visual document, after its `<body>`.
9. Clone that campaign: the clone has the same preview text.
10. Switch the content type to plain text: the Preview text field is hidden.

Stop the app and `"$PGBIN/dropdb" -h 127.0.0.1 -p 55432 -U lmtest lm_manual`.

- [ ] **Step 4: Commit the patch record**

```bash
git add PATCHES.md
git commit -m "docs(patches): record subscriber names and preview text

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 5: Review gate**

Run `/codex:review` on the branch diff against `gunmade/gunmade`. Fix real findings, re-run the Step 2 commands, and commit. Maximum two review rounds; report any open findings after that.

- [ ] **Step 6: Hand off — stop here**

Report to the owner: the verification results, the manual check results, the review findings and their resolution. **Ask before pushing the branch or opening a pull request.** Merging to `gunmade` deploys to production and runs the migration; it follows the spec's "Rollout and safety" steps (fresh deployed-commit check, namecheck on a production copy, `pg_dump` backup) and is the owner's decision.

`listmonk-private/CUSTOMIZATIONS.md` has uncommitted owner edits. Do not touch it; suggest the owner add a one-line pointer to this section of `PATCHES.md`.
