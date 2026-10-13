# Automations (welcome sequences) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A new signup on a list with an enabled automation gets the automation's email(s) from Listmonk, sent as a hidden campaign with normal campaign stats.

**Architecture:** New package `internal/automations` (schema, store, worker) in the `hcm` fork. Each step is a hidden draft campaign. A worker goroutine claims due enrollments, runs the brand guard and a marker check, renders the campaign for one subscriber, and sends it synchronously through a new `Manager.SendCampaignMessage`. Enrollment comes from `POST /api/ext/automations/enroll` (gateway) and from the public form / opt-in confirm paths, made durable with intent rows.

**Tech Stack:** Go 1.24 (echo v4, sqlx, lib/pq), PostgreSQL, Vue 2.7 + Buefy, bash e2e scripts.

**Spec:** `docs/superpowers/specs/2026-10-12-automations-design.md` (read it before any task; section numbers below refer to it).

## Global Constraints

- Branch `hcm`, worktree `/Users/brady/GM/listmonk-hcm`. Never touch branch `gunmade`.
- All fork tables live in schema `ext`. DDL is `CREATE ... IF NOT EXISTS` and runs at startup before queries are prepared.
- Every hunk in an upstream file is marked `// FORK(hcm):` (Go/JS) or `-- FORK(hcm):` (SQL), and every changed upstream file is listed in `PATCHES.md` under a new "Automations (hcm branch)" section.
- Each goyesql `-- name:` line comes first in its block.
- Tests that need Postgres use `LISTMONK_TEST_DSN` (local test Postgres `lm-brands-pg`, `127.0.0.1:55432`, throwaway DBs). Never connect to a production database. Generate random test credentials and never print them.
- E-mail addresses in tests: `@example.com` only. No real mail leaves the machine (SMTP is a closed port or the local sink of `scripts/brands-e2e.sh --smtp-sink`).
- Marker string, exact: `[[SENDER_INFO]]`.
- Status values, exact: `active`, `sending`, `done`, `skipped`, `failed`, `unknown`.
- Backoff, exact: after attempt 1 → 1 min, 2 → 5 min, 3 → 30 min, 4 → 2 h; a failure on attempt 5 → `failed`.
- Stale `sending` → `unknown` after 10 minutes. Intent sweep: older than 2 minutes; expire after 7 days. Worker tick 30 s, up to 100 enrollments per tick. Marker defer 10 min; marker log at most once per hour per step.
- Active subscription (spec 6.0): `confirmed`, or `unconfirmed` on a list with `optin='single'`.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Do not push.
- `go test ./...` and `cd frontend && yarn build` must pass at the end of every task that touches them.

## Review Focus

1. A gateway retry calls enroll twice for one person → exactly one enrollment and one email (unique key; covered in Task 3 test `TestEnrollTwice`).
2. A step campaign appears in `GET /api/campaigns`, the dashboard count, or can be started from the campaign page → it must not (Task 6 test `TestHiddenCampaignNotListed`, e2e step 4).
3. The messenger is down for a while → the enrollment retries with backoff and never records a send that did not happen (Task 5 test `TestWorkerRetryThenFail`).
4. A person on the RI list joins the TA list through the public form (409 branch) → one TA welcome; re-submitting the same form → none (Task 8 tests).
5. Someone puts the marker back into the template (not the campaign body) of an enabled automation → no send; enrollment waits (Task 5 test `TestWorkerMarkerDefers`).

---

## File map

- Create `internal/automations/schema.sql` — DDL (spec 4, 6.2.1).
- Create `internal/automations/store.go` — types, `Bootstrap`, CRUD, `StepOwner`.
- Create `internal/automations/enroll.go` — `Enroll`, active rule, intents.
- Create `internal/automations/worker.go` — claim / finish / retry / defer / stale, `Worker` loop.
- Create `internal/automations/*_test.go` and `testdb_test.go` (copy of the brands helper with prefix `automations_`).
- Modify `internal/manager/manager.go` — `campaignOut` helper, `SendCampaignMessage`.
- Create `cmd/automations.go` — HTTP handlers, worker wiring helpers.
- Modify `cmd/main.go`, `cmd/handlers.go`, `cmd/campaigns.go`, `cmd/public.go`, `internal/brands/routes.go`.
- Modify `queries/campaigns.sql`, `queries/analytics.sql`, `queries/ext.sql` (hidden-campaign exclusion).
- Create `frontend/src/views/Automations.vue`, `frontend/src/views/AutomationForm.vue`; modify `frontend/src/api/index.js`, `frontend/src/router/index.js`, `frontend/src/components/Navigation.vue`.
- Create `scripts/automations-e2e.sh`, `scripts/convert-eo-welcome.py`, `scripts/fixtures/ta-welcome.html` (output).
- Modify `PATCHES.md`.

---

### Task 1: Schema, store and bootstrap

**Files:**
- Create: `internal/automations/schema.sql`, `internal/automations/store.go`, `internal/automations/testdb_test.go`, `internal/automations/store_db_test.go`
- Modify: `cmd/main.go` (after `brands.Bootstrap`)

**Interfaces:**
- Consumes: `brands.Bootstrap(db)` must run first (it creates schema `ext`).
- Produces:
  - `func Bootstrap(db *sqlx.DB) error`
  - `type Automation struct { ID int; Name string; ListID int; Enabled bool; CreatedAt, UpdatedAt time.Time }` (db tags `id,name,list_id,enabled,created_at,updated_at`; json tags snake_case)
  - `type Step struct { AutomationID, Position, DelayMinutes, CampaignID int }` (db/json `automation_id, position, delay_minutes, campaign_id`)
  - `type Summary struct { Automation; Enrolled, Sent, Active, Failed, Unknown int }` (json `enrolled, sent, active, failed, unknown`)
  - `type StepOwnerInfo struct { AutomationID, ListID, Position int; Enabled bool }`
  - `var ErrEnabledOnList = errors.New("another automation on this list is enabled")`, `var ErrEnabled = errors.New("disable the automation first")`, `var ErrHasEnrollments = errors.New("the list cannot change after the first enrollment")`
  - `type Store struct{ db *sqlx.DB }`, `func NewStore(db *sqlx.DB) *Store`
  - `func (s *Store) Create(name string, listID, campaignID int) (Automation, error)` — inserts the automation (disabled) and step 1 (delay 0) in one transaction.
  - `func (s *Store) Get(id int) (Automation, []Step, error)` — `sql.ErrNoRows` when missing.
  - `func (s *Store) All(listIDs []int) ([]Summary, error)` — `listIDs == nil` → all; non-nil → only those lists (empty slice → none).
  - `func (s *Store) Update(id int, name string, listID int, steps []Step) error` — name, list (refused with `ErrHasEnrollments` when the list changes and an enrollment exists), and `delay_minutes` of existing positions.
  - `func (s *Store) SetEnabled(id int, on bool) error` — unique-index violation → `ErrEnabledOnList`.
  - `func (s *Store) Delete(id int) (campaignIDs []int, err error)` — `ErrEnabled` when enabled; deletes the automation (cascade) and returns the step campaign ids for the caller to delete.
  - `func (s *Store) StepOwner(campaignID int) (StepOwnerInfo, bool, error)`

- [ ] **Step 1: Write `schema.sql`** — copy the DDL of spec 4 and spec 6.2.1 verbatim, with the intent column named `was_active`, and add:

```sql
CREATE INDEX IF NOT EXISTS automation_intents_created ON ext.automation_intents (created_at);
```

- [ ] **Step 2: Write `testdb_test.go`** — copy `internal/brands/testdb_test.go`, change the package to `automations` and the database prefix to `automations_`, and after installing `../../schema.sql` run `brands.Bootstrap(db)` then `Bootstrap(db)`. Add helpers used by later tests:

```go
// mkList inserts a list and returns its id. optin is "single" or "double".
func mkList(t *testing.T, db *sqlx.DB, name, optin string, tags ...string) int {
	t.Helper()
	var id int
	if err := db.Get(&id, `INSERT INTO lists (uuid, name, type, optin, tags) VALUES (gen_random_uuid(), $1, 'public', $2, $3) RETURNING id`,
		name, optin, pq.Array(tags)); err != nil {
		t.Fatalf("mkList: %v", err)
	}
	return id
}

// mkSub inserts a subscriber, optionally subscribed to listID with status st ("" = no subscription).
func mkSub(t *testing.T, db *sqlx.DB, email string, listID int, st string) int {
	t.Helper()
	var id int
	if err := db.Get(&id, `INSERT INTO subscribers (uuid, email, name, status) VALUES (gen_random_uuid(), $1, $1, 'enabled') RETURNING id`, email); err != nil {
		t.Fatalf("mkSub: %v", err)
	}
	if st != "" {
		if _, err := db.Exec(`INSERT INTO subscriber_lists (subscriber_id, list_id, status) VALUES ($1, $2, $3)`, id, listID, st); err != nil {
			t.Fatalf("mkSub list: %v", err)
		}
	}
	return id
}

// mkCampaign inserts a draft campaign on listID and returns its id.
func mkCampaign(t *testing.T, db *sqlx.DB, listID int) int {
	t.Helper()
	var id int
	if err := db.Get(&id, `INSERT INTO campaigns (uuid, name, subject, from_email, body, content_type, type, status, messenger, template_id)
		VALUES (gen_random_uuid(), 'step', 'Welcome', 'a@example.com', 'hi', 'html', 'regular', 'draft', 'email', 1) RETURNING id`); err != nil {
		t.Fatalf("mkCampaign: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO campaign_lists (campaign_id, list_id, list_name) VALUES ($1, $2, 'l')`, id, listID); err != nil {
		t.Fatalf("mkCampaign list: %v", err)
	}
	return id
}
```

(If `gen_random_uuid()` is missing on the test Postgres, use `md5(random()::text)::uuid`.)

- [ ] **Step 3: Write the failing tests** in `store_db_test.go`:

```go
func TestCreateGetAll(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	c := mkCampaign(t, db, l)
	a, err := s.Create("TA welcome", l, c)
	if err != nil { t.Fatal(err) }
	if a.Enabled { t.Fatal("new automation must be disabled") }
	got, steps, err := s.Get(a.ID)
	if err != nil || got.Name != "TA welcome" || len(steps) != 1 || steps[0].CampaignID != c || steps[0].DelayMinutes != 0 {
		t.Fatalf("get: %+v %+v %v", got, steps, err)
	}
	all, err := s.All(nil)
	if err != nil || len(all) != 1 { t.Fatalf("all nil: %v %v", all, err) }
	none, err := s.All([]int{})
	if err != nil || len(none) != 0 { t.Fatalf("all empty: %v %v", none, err) }
}

func TestOneEnabledPerList(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	a1, _ := s.Create("a1", l, mkCampaign(t, db, l))
	a2, _ := s.Create("a2", l, mkCampaign(t, db, l))
	if err := s.SetEnabled(a1.ID, true); err != nil { t.Fatal(err) }
	if err := s.SetEnabled(a2.ID, true); !errors.Is(err, ErrEnabledOnList) { t.Fatalf("want ErrEnabledOnList, got %v", err) }
}

func TestDeleteRules(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	c := mkCampaign(t, db, l)
	a, _ := s.Create("a", l, c)
	_ = s.SetEnabled(a.ID, true)
	if _, err := s.Delete(a.ID); !errors.Is(err, ErrEnabled) { t.Fatalf("want ErrEnabled, got %v", err) }
	_ = s.SetEnabled(a.ID, false)
	ids, err := s.Delete(a.ID)
	if err != nil || len(ids) != 1 || ids[0] != c { t.Fatalf("delete: %v %v", ids, err) }
}

func TestStepOwnerAndCampaignRestrict(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	c := mkCampaign(t, db, l)
	a, _ := s.Create("a", l, c)
	info, ok, err := s.StepOwner(c)
	if err != nil || !ok || info.AutomationID != a.ID || info.ListID != l || info.Position != 1 || info.Enabled {
		t.Fatalf("owner: %+v %v %v", info, ok, err)
	}
	if _, ok, _ := s.StepOwner(c + 1000); ok { t.Fatal("unknown campaign must not be a step") }
	if _, err := db.Exec(`DELETE FROM campaigns WHERE id=$1`, c); err == nil {
		t.Fatal("deleting a step campaign must fail (ON DELETE RESTRICT)")
	}
}

func TestListChangeAfterEnrollment(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	l2 := mkList(t, db, "TA2", "single", "ta")
	a, _ := s.Create("a", l, mkCampaign(t, db, l))
	sub := mkSub(t, db, "x@example.com", l, "confirmed")
	if _, err := db.Exec(`INSERT INTO ext.automation_enrollments (automation_id, subscriber_id, status, next_at) VALUES ($1,$2,'active',now())`, a.ID, sub); err != nil { t.Fatal(err) }
	if err := s.Update(a.ID, "a", l2, nil); !errors.Is(err, ErrHasEnrollments) { t.Fatalf("want ErrHasEnrollments, got %v", err) }
}
```

- [ ] **Step 4: Run** `LISTMONK_TEST_DSN=<dsn> go test ./internal/automations/ -run 'TestCreate|TestOne|TestDelete|TestStepOwner|TestListChange' -v` — expected: compile failure (types missing).

- [ ] **Step 5: Implement `store.go`.** Rules:
  - `Bootstrap` embeds `schema.sql` (`//go:embed`) and runs it in one transaction (same shape as `brands.Bootstrap`).
  - `All` SQL:

```sql
SELECT a.*,
  (SELECT COUNT(*) FROM ext.automation_enrollments e WHERE e.automation_id = a.id) AS enrolled,
  (SELECT COUNT(*) FROM ext.automation_sends s JOIN ext.automation_enrollments e ON e.id = s.enrollment_id WHERE e.automation_id = a.id) AS sent,
  (SELECT COUNT(*) FROM ext.automation_enrollments e WHERE e.automation_id = a.id AND e.status IN ('active','sending')) AS active,
  (SELECT COUNT(*) FROM ext.automation_enrollments e WHERE e.automation_id = a.id AND e.status = 'failed') AS failed,
  (SELECT COUNT(*) FROM ext.automation_enrollments e WHERE e.automation_id = a.id AND e.status = 'unknown') AS unknown
FROM ext.automations a
WHERE ($1::INT[] IS NULL OR a.list_id = ANY($1::INT[]))
ORDER BY a.id
```

    Pass `nil` as a typed NULL (`pq.Array([]int(nil))` sends `NULL` only with `sql.NullString`; use `interface{}(nil)` when `listIDs == nil`, else `pq.Array(listIDs)`).
  - `SetEnabled`: `UPDATE ext.automations SET enabled=$2, updated_at=now() WHERE id=$1`; map `*pq.Error` with `Constraint == "automations_one_enabled_per_list"` to `ErrEnabledOnList`; 0 rows → `sql.ErrNoRows`.
  - `Delete`: in a transaction: `SELECT enabled ... FOR UPDATE`; enabled → `ErrEnabled`; collect `campaign_id` from steps; `DELETE FROM ext.automations WHERE id=$1` (cascade removes steps, enrollments, sends); commit; return ids. The caller deletes the campaigns after the commit.
  - `StepOwner`:

```sql
SELECT s.automation_id, a.list_id, s.position, a.enabled
FROM ext.automation_steps s JOIN ext.automations a ON a.id = s.automation_id
WHERE s.campaign_id = $1
```

- [ ] **Step 6: Wire bootstrap in `cmd/main.go`** directly after the `brands.Bootstrap` block:

```go
	// FORK(hcm): automations tables (ext schema) before queries are prepared.
	if err := automations.Bootstrap(db); err != nil {
		lo.Fatalf("error bootstrapping automations schema: %v", err)
	}
```

- [ ] **Step 7: Run** the Step 4 command → PASS; `go build ./...` → OK.

- [ ] **Step 8: Commit**

```bash
git add internal/automations cmd/main.go
git commit -m "Automations: ext schema, store and startup bootstrap

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Synchronous campaign send in the manager

**Files:**
- Modify: `internal/manager/manager.go` (`worker()` body around the `models.Message` build, new methods)
- Test: `internal/manager/send_fork_test.go`

**Interfaces:**
- Produces:
  - `func (m *Manager) campaignOut(msg CampaignMessage) models.Message` (unexported)
  - `func (m *Manager) SendCampaignMessage(msg CampaignMessage) error` — attaches media, builds the message with `campaignOut`, returns `ErrUnknownMessenger` (new exported var, text `"unknown messenger"`) when `m.messengers[msg.Campaign.Messenger]` is missing, else the result of `Push`.

- [ ] **Step 1: Write the failing test** `send_fork_test.go` (package `manager`). Build a `Manager` the way the existing tests in the package do (read `internal/manager/*_test.go` first; if there is none, construct `&Manager{messengers: map[string]Messenger{"email": fake}, cfg: Config{UnsubHeader: true}, log: log.New(io.Discard, "", 0)}` and a `CampaignMessage` with `Campaign: &models.Campaign{UUID: "c-uuid", Messenger: "email", Name: "n"}`, `Subscriber: models.Subscriber{UUID: "s-uuid", ID: 7}`, `to: "x@example.com"`, `subject: "Hi"`, `body: []byte("b")`, `unsubURL: "https://u"`). The fake messenger records the last `models.Message` and returns a configured error.

```go
func TestSendCampaignMessage(t *testing.T) {
	f := &fakeMsgr{name: "email"}
	m := newForkTestManager(f)
	msg := testCampMsg("email")
	if err := m.SendCampaignMessage(msg); err != nil { t.Fatal(err) }
	if f.last.Headers.Get(models.EmailHeaderCampaignUUID) != "c-uuid" || f.last.Headers.Get(models.EmailHeaderSubscriberUUID) != "s-uuid" {
		t.Fatalf("headers: %v", f.last.Headers)
	}
	if f.last.Headers.Get("List-Unsubscribe") != "<https://u>" { t.Fatalf("unsub header: %v", f.last.Headers) }
	f.err = errors.New("smtp down")
	if err := m.SendCampaignMessage(msg); err == nil || err.Error() != "smtp down" { t.Fatalf("want push error, got %v", err) }
	if err := m.SendCampaignMessage(testCampMsg("nope")); !errors.Is(err, ErrUnknownMessenger) { t.Fatalf("want ErrUnknownMessenger, got %v", err) }
}
```

- [ ] **Step 2: Run** `go test ./internal/manager/ -run TestSendCampaignMessage -v` → FAIL (undefined).

- [ ] **Step 3: Implement.** Move the block in `worker()` from `out := models.Message{` through `out.Headers = h` into `campaignOut`, unchanged, returning `out`. In `worker()` replace it with `out := m.campaignOut(msg) // FORK(hcm): shared with SendCampaignMessage`. Add:

```go
// FORK(hcm): ErrUnknownMessenger is returned by SendCampaignMessage.
var ErrUnknownMessenger = errors.New("unknown messenger")

// SendCampaignMessage sends one campaign message now and returns the
// messenger's result. FORK(hcm): automations need the result; the queue in
// PushCampaignMessage only logs it.
func (m *Manager) SendCampaignMessage(msg CampaignMessage) error {
	if err := m.attachMedia(msg.Campaign); err != nil {
		return err
	}
	ms, ok := m.messengers[msg.Campaign.Messenger]
	if !ok {
		return ErrUnknownMessenger
	}
	return ms.Push(m.campaignOut(msg))
}
```

- [ ] **Step 4: Run** `go test ./internal/manager/ -v` → PASS (including existing tests).

- [ ] **Step 5: Commit** (`internal/manager/manager.go`, `internal/manager/send_fork_test.go`), message `Automations: synchronous campaign send in the manager`.

---

### Task 3: Enroll and intents

**Files:**
- Create: `internal/automations/enroll.go`, `internal/automations/enroll_db_test.go`

**Interfaces:**
- Consumes: Task 1 `Store`.
- Produces:
  - Reasons (exact strings): `ReasonNoAutomation = "no-automation"`, `ReasonNoSubscriber = "no-subscriber"`, `ReasonBlocklisted = "blocklisted"`, `ReasonNotActive = "not-active"`, `ReasonAlreadyEnrolled = "already-enrolled"`, `ReasonNoStep = "no-step"`.
  - `func (s *Store) Enroll(subscriberID, listID int) (enrolled bool, reason string, err error)`
  - `func (s *Store) ActiveOn(subscriberID, listID int) (bool, error)` — spec 6.0; no row → false.
  - `func (s *Store) BeginIntents(email string, listIDs []int) ([]int64, error)` — one row per list that has an enabled automation; `email` lower-cased and trimmed; `was_active` from the current state (no subscriber → false).
  - `func (s *Store) ResolveIntents(ids []int64) (int, error)` — returns the number enrolled.
  - `func (s *Store) SweepIntents(minAge, maxAge time.Duration) (enrolled int, err error)`
  - `func (s *Store) ListIDsByUUID(uuids []string) ([]int, error)`

- [ ] **Step 1: Write the failing tests:**

```go
func setupEnabled(t *testing.T, db *sqlx.DB, optin string) (*Store, int, int) {
	s := NewStore(db)
	l := mkList(t, db, "TA", optin, "ta")
	a, err := s.Create("a", l, mkCampaign(t, db, l))
	if err != nil { t.Fatal(err) }
	if err := s.SetEnabled(a.ID, true); err != nil { t.Fatal(err) }
	return s, l, a.ID
}

func TestEnrollRules(t *testing.T) {
	db := testDB(t)
	s, l, _ := setupEnabled(t, db, "single")
	other := mkList(t, db, "RI", "single", "ri")
	cases := []struct{ name, st string; list int; want bool; reason string }{
		{"confirmed", "confirmed", l, true, ""},
		{"single unconfirmed is active", "unconfirmed", l, true, ""},
		{"unsubscribed", "unsubscribed", l, false, ReasonNotActive},
		{"no automation on list", "confirmed", other, false, ReasonNoAutomation},
	}
	for i, c := range cases {
		sub := mkSub(t, db, fmt.Sprintf("e%d@example.com", i), c.list, c.st)
		ok, reason, err := s.Enroll(sub, c.list)
		if err != nil || ok != c.want || reason != c.reason { t.Errorf("%s: %v %q %v", c.name, ok, reason, err) }
	}
	sub := mkSub(t, db, "bl@example.com", l, "confirmed")
	db.MustExec(`UPDATE subscribers SET status='blocklisted' WHERE id=$1`, sub)
	if ok, reason, _ := s.Enroll(sub, l); ok || reason != ReasonBlocklisted { t.Errorf("blocklisted: %v %q", ok, reason) }
	if ok, reason, _ := s.Enroll(999999, l); ok || reason != ReasonNoSubscriber { t.Errorf("missing: %v %q", ok, reason) }
}

func TestEnrollDoubleOptinUnconfirmed(t *testing.T) {
	db := testDB(t)
	s, l, _ := setupEnabled(t, db, "double")
	sub := mkSub(t, db, "d@example.com", l, "unconfirmed")
	if ok, reason, _ := s.Enroll(sub, l); ok || reason != ReasonNotActive { t.Fatalf("%v %q", ok, reason) }
}

func TestEnrollTwice(t *testing.T) {
	db := testDB(t)
	s, l, aid := setupEnabled(t, db, "single")
	sub := mkSub(t, db, "t@example.com", l, "confirmed")
	if ok, _, err := s.Enroll(sub, l); !ok || err != nil { t.Fatal("first enroll") }
	if ok, reason, _ := s.Enroll(sub, l); ok || reason != ReasonAlreadyEnrolled { t.Fatalf("second: %v %q", ok, reason) }
	var n int
	db.Get(&n, `SELECT COUNT(*) FROM ext.automation_enrollments WHERE automation_id=$1`, aid)
	if n != 1 { t.Fatalf("enrollments = %d", n) }
	var due bool
	db.Get(&due, `SELECT next_at <= now() + interval '1 second' FROM ext.automation_enrollments WHERE automation_id=$1`, aid)
	if !due { t.Fatal("delay 0 must be due now") }
}

func TestIntentNewSignup(t *testing.T) {
	db := testDB(t)
	s, l, _ := setupEnabled(t, db, "single")
	ids, err := s.BeginIntents(" New@Example.com ", []int{l})
	if err != nil || len(ids) != 1 { t.Fatalf("begin: %v %v", ids, err) }
	mkSub(t, db, "new@example.com", l, "unconfirmed") // the upstream insert
	n, err := s.ResolveIntents(ids)
	if err != nil || n != 1 { t.Fatalf("resolve: %d %v", n, err) }
	var left int
	db.Get(&left, `SELECT COUNT(*) FROM ext.automation_intents`)
	if left != 0 { t.Fatal("resolved intent must be deleted") }
}

func TestIntentAlreadyActiveNoWelcome(t *testing.T) {
	db := testDB(t)
	s, l, _ := setupEnabled(t, db, "single")
	mkSub(t, db, "old@example.com", l, "confirmed")
	ids, _ := s.BeginIntents("old@example.com", []int{l})
	if n, _ := s.ResolveIntents(ids); n != 0 { t.Fatal("a re-submit by an active subscriber must not enroll") }
}

func TestIntentSweepRecoversCrash(t *testing.T) {
	db := testDB(t)
	s, l, _ := setupEnabled(t, db, "single")
	ids, _ := s.BeginIntents("crash@example.com", []int{l})
	mkSub(t, db, "crash@example.com", l, "unconfirmed")
	db.MustExec(`UPDATE ext.automation_intents SET created_at = now() - interval '3 minutes' WHERE id = $1`, ids[0])
	n, err := s.SweepIntents(2*time.Minute, 7*24*time.Hour)
	if err != nil || n != 1 { t.Fatalf("sweep: %d %v", n, err) }
}

func TestIntentPendingOptinKeptThenExpired(t *testing.T) {
	db := testDB(t)
	s, l, _ := setupEnabled(t, db, "double")
	ids, _ := s.BeginIntents("p@example.com", []int{l})
	mkSub(t, db, "p@example.com", l, "unconfirmed")
	db.MustExec(`UPDATE ext.automation_intents SET created_at = now() - interval '1 day' WHERE id = $1`, ids[0])
	if n, _ := s.SweepIntents(2*time.Minute, 7*24*time.Hour); n != 0 { t.Fatal("not active yet") }
	var left int
	db.Get(&left, `SELECT COUNT(*) FROM ext.automation_intents`)
	if left != 1 { t.Fatal("a pending double opt-in intent must be kept") }
	db.MustExec(`UPDATE ext.automation_intents SET created_at = now() - interval '8 days'`)
	s.SweepIntents(2*time.Minute, 7*24*time.Hour)
	db.Get(&left, `SELECT COUNT(*) FROM ext.automation_intents`)
	if left != 0 { t.Fatal("an intent older than 7 days must be deleted") }
}
```

- [ ] **Step 2: Run** `go test ./internal/automations/ -run 'TestEnroll|TestIntent' -v` → FAIL (undefined).

- [ ] **Step 3: Implement `enroll.go`.** Rules:
  - Active SQL (used by `ActiveOn` and inside transactions):

```sql
SELECT (sl.status = 'confirmed' OR (sl.status = 'unconfirmed' AND l.optin = 'single'))
FROM subscriber_lists sl JOIN lists l ON l.id = sl.list_id
WHERE sl.subscriber_id = $1 AND sl.list_id = $2
```

  - `enrollTx(tx *sqlx.Tx, subscriberID, listID int) (bool, string, error)` does spec 6.1 steps 1–3 in order: enabled automation for the list (`SELECT id FROM ext.automations WHERE list_id=$1 AND enabled`) → `ReasonNoAutomation`; subscriber row (`SELECT status FROM subscribers WHERE id=$1`) → `ReasonNoSubscriber`; `blocklisted` → `ReasonBlocklisted`; active → `ReasonNotActive`; step 1 delay (`SELECT delay_minutes FROM ext.automation_steps WHERE automation_id=$1 AND position=1`, none → `ReasonNoStep`); then

```sql
INSERT INTO ext.automation_enrollments (automation_id, subscriber_id, next_position, next_at, status)
VALUES ($1, $2, 1, now() + make_interval(mins => $3), 'active')
ON CONFLICT (automation_id, subscriber_id) DO NOTHING
```

    0 rows → `ReasonAlreadyEnrolled`. `Enroll` wraps `enrollTx` in its own transaction.
  - `BeginIntents`: normalise `strings.ToLower(strings.TrimSpace(email))`; for each list id with an enabled automation, compute `was_active` (`SELECT id FROM subscribers WHERE LOWER(email)=$1`; missing → false; else the active SQL, no row → false) and insert. Return the new ids.
  - `resolveOne(id)`: in one transaction: `SELECT ... FROM ext.automation_intents WHERE id=$1 FOR UPDATE SKIP LOCKED` (none → 0); look up the subscriber by email; not found or not active → leave the row, commit, 0; active and `was_active` → delete, 0; active and not `was_active` → `enrollTx`, delete, commit, 1 if enrolled.
  - `ResolveIntents(ids)` loops `resolveOne`; `SweepIntents` runs `DELETE FROM ext.automation_intents WHERE created_at < now() - $maxAge` first, then `resolveOne` for every id with `created_at < now() - $minAge`.
  - `ListIDsByUUID`: `SELECT id FROM lists WHERE uuid = ANY($1::UUID[])`.

- [ ] **Step 4: Run** the Step 2 command → PASS.

- [ ] **Step 5: Commit** (`internal/automations/enroll.go`, `internal/automations/enroll_db_test.go`), message `Automations: enroll rules and durable public-form intents`.

---

### Task 4: Enrollment state machine (store side of the worker)

**Files:**
- Create: `internal/automations/state.go`, `internal/automations/state_db_test.go`

**Interfaces:**
- Produces:
  - `type Claimed struct { ID int64; AutomationID, SubscriberID, Position, Attempts, ListID, CampaignID int; Enabled, HasStep bool }`
  - `func (s *Store) ClaimOne() (*Claimed, error)` — nil, nil when nothing is due.
  - `func (s *Store) Finish(id int64, status, reason string) error` — final states `skipped`/`failed`; sets `next_at=NULL`, `last_error=reason`.
  - `func (s *Store) Advance(c *Claimed) error` — records the send and moves on (spec 6.3 step 6).
  - `func (s *Store) Retry(c *Claimed, cause string) error` — spec 6.3 step 7 with the Global Constraints backoff.
  - `func (s *Store) Defer(c *Claimed, d time.Duration, reason string) error` — back to `active`, `attempts = attempts - 1`, `next_at = now() + d`.
  - `func (s *Store) MarkStale(after time.Duration) (int, error)`
  - `func Backoff(attempts int) time.Duration`

- [ ] **Step 1: Write the failing tests:**

```go
func claimSetup(t *testing.T) (*sqlx.DB, *Store, int, int) {
	db := testDB(t)
	s, l, aid := setupEnabled(t, db, "single")
	sub := mkSub(t, db, "w@example.com", l, "confirmed")
	if ok, _, _ := s.Enroll(sub, l); !ok { t.Fatal("enroll") }
	return db, s, aid, sub
}

func TestClaimAdvanceDone(t *testing.T) {
	db, s, aid, _ := claimSetup(t)
	c, err := s.ClaimOne()
	if err != nil || c == nil || c.AutomationID != aid || c.Position != 1 || c.Attempts != 1 || !c.Enabled || !c.HasStep { t.Fatalf("claim: %+v %v", c, err) }
	if again, _ := s.ClaimOne(); again != nil { t.Fatal("a claimed row must not be claimed twice") }
	if err := s.Advance(c); err != nil { t.Fatal(err) }
	var st string; var sends int
	db.Get(&st, `SELECT status FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	db.Get(&sends, `SELECT COUNT(*) FROM ext.automation_sends WHERE enrollment_id=$1`, c.ID)
	if st != "done" || sends != 1 { t.Fatalf("status %s sends %d", st, sends) }
}

func TestAdvanceToStepTwo(t *testing.T) {
	db, s, aid, _ := claimSetup(t)
	var l int
	db.Get(&l, `SELECT list_id FROM ext.automations WHERE id=$1`, aid)
	db.MustExec(`INSERT INTO ext.automation_steps (automation_id, position, delay_minutes, campaign_id) VALUES ($1, 2, 1440, $2)`, aid, mkCampaign(t, db, l))
	c, _ := s.ClaimOne()
	s.Advance(c)
	var st string; var pos int; var later bool
	db.Get(&st, `SELECT status FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	db.Get(&pos, `SELECT next_position FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	db.Get(&later, `SELECT next_at > now() + interval '23 hours' FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	if st != "active" || pos != 2 || !later { t.Fatalf("%s %d %v", st, pos, later) }
}

func TestRetryBackoffAndFail(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	want := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
	for i, d := range want {
		if Backoff(i+1) != d { t.Fatalf("Backoff(%d) = %v", i+1, Backoff(i+1)) }
	}
	for i := 1; i <= 5; i++ {
		db.MustExec(`UPDATE ext.automation_enrollments SET next_at = now() - interval '1 second' WHERE status='active'`)
		c, _ := s.ClaimOne()
		if c == nil || c.Attempts != i { t.Fatalf("attempt %d: %+v", i, c) }
		s.Retry(c, "smtp down")
	}
	var st, le string
	db.Get(&st, `SELECT status FROM ext.automation_enrollments`)
	db.Get(&le, `SELECT last_error FROM ext.automation_enrollments`)
	if st != "failed" || le != "smtp down" { t.Fatalf("%s %s", st, le) }
}

func TestDeferRefundsAttempt(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	c, _ := s.ClaimOne()
	s.Defer(c, 10*time.Minute, "sender info marker")
	var st string; var att int
	db.Get(&st, `SELECT status FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	db.Get(&att, `SELECT attempts FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	if st != "active" || att != 0 { t.Fatalf("%s %d", st, att) }
}

func TestMarkStale(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	c, _ := s.ClaimOne()
	db.MustExec(`UPDATE ext.automation_enrollments SET sending_at = now() - interval '11 minutes' WHERE id=$1`, c.ID)
	if n, _ := s.MarkStale(10 * time.Minute); n != 1 { t.Fatal("stale") }
	var st string
	db.Get(&st, `SELECT status FROM ext.automation_enrollments WHERE id=$1`, c.ID)
	if st != "unknown" { t.Fatal(st) }
}
```

- [ ] **Step 2: Run** `go test ./internal/automations/ -run 'TestClaim|TestAdvance|TestRetry|TestDefer|TestMarkStale' -v` → FAIL.

- [ ] **Step 3: Implement `state.go`.** `ClaimOne` SQL:

```sql
WITH c AS (
  UPDATE ext.automation_enrollments e
     SET status = 'sending', sending_at = now(), attempts = attempts + 1, updated_at = now()
   WHERE e.id = (SELECT id FROM ext.automation_enrollments
                  WHERE status = 'active' AND next_at <= now()
                  ORDER BY next_at LIMIT 1 FOR UPDATE SKIP LOCKED)
  RETURNING e.id, e.automation_id, e.subscriber_id, e.next_position, e.attempts
)
SELECT c.id, c.automation_id, c.subscriber_id, c.next_position AS position, c.attempts,
       COALESCE(a.list_id, 0) AS list_id, COALESCE(a.enabled, false) AS enabled,
       COALESCE(st.campaign_id, 0) AS campaign_id, (st.campaign_id IS NOT NULL) AS has_step
FROM c LEFT JOIN ext.automations a ON a.id = c.automation_id
       LEFT JOIN ext.automation_steps st ON st.automation_id = c.automation_id AND st.position = c.next_position
```

  `Advance` in one transaction: `INSERT INTO ext.automation_sends (enrollment_id, position, campaign_id) VALUES (...) ON CONFLICT DO NOTHING`; look up `delay_minutes` of position+1; found → `status='active', next_position=next_position+1, next_at=now()+delay, attempts=0, last_error=NULL, sending_at=NULL`; not found → `status='done', next_at=NULL, sending_at=NULL`. `Retry`: `c.Attempts >= 5` → `Finish(c.ID, "failed", cause)`; else `status='active', next_at=now()+Backoff(c.Attempts), last_error=cause, sending_at=NULL`. `MarkStale`: `UPDATE ... SET status='unknown', next_at=NULL WHERE status='sending' AND sending_at < now() - $1`. `Backoff`: 1→1m, 2→5m, 3→30m, otherwise 2h.

- [ ] **Step 4: Run** the Step 2 command → PASS.

- [ ] **Step 5: Commit** (`state.go`, `state_db_test.go`), message `Automations: enrollment claim, advance, retry, defer and stale handling`.

---

### Task 5: Worker loop

**Files:**
- Create: `internal/automations/worker.go`, `internal/automations/worker_test.go`

**Interfaces:**
- Consumes: Task 3 `ActiveOn`, Task 4 state methods.
- Produces:

```go
// Message is one rendered step email for one subscriber.
type Message interface {
	Content() string // subject + "\n" + HTML body + "\n" + alt body, after the template is applied
	Send() error     // synchronous; nil means the messenger accepted it
}

type Deps struct {
	// Blocklisted reports the subscriber status.
	Blocklisted func(subscriberID int) (bool, error)
	// Guard runs brand send guard rules 1-4 for the step campaign.
	Guard func(campaignID int) error
	// Render loads the campaign and the subscriber and renders the message.
	Render func(campaignID, subscriberID int) (Message, error)
}

type Worker struct { /* store, deps, log, markerLogged map[string]time.Time, now func() time.Time */ }

func NewWorker(s *Store, d Deps, lo *log.Logger) *Worker
func (w *Worker) Tick() // one pass: MarkStale(10m), SweepIntents(2m, 7d), then up to 100 ClaimOne+process
func (w *Worker) Run(ctx context.Context, every time.Duration) // Tick at start, then every `every`
const Marker = "[[SENDER_INFO]]"
```

- [ ] **Step 1: Write the failing tests** with fake `Deps` against the test DB (`claimSetup` from Task 4):

```go
type fakeMsg struct{ content string; err error; sent *int }
func (m fakeMsg) Content() string { return m.content }
func (m fakeMsg) Send() error { if m.err == nil { *m.sent++ }; return m.err }

func depsWith(sent *int, content string, sendErr, guardErr error) Deps {
	return Deps{
		Blocklisted: func(int) (bool, error) { return false, nil },
		Guard:       func(int) error { return guardErr },
		Render:      func(int, int) (Message, error) { return fakeMsg{content, sendErr, sent}, nil },
	}
}

func status(t *testing.T, db *sqlx.DB) string { var s string; db.Get(&s, `SELECT status FROM ext.automation_enrollments`); return s }

func TestWorkerSendsOnce(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	sent := 0
	w := NewWorker(s, depsWith(&sent, "Welcome", nil, nil), log.New(io.Discard, "", 0))
	w.Tick(); w.Tick()
	if sent != 1 || status(t, db) != "done" { t.Fatalf("sent %d status %s", sent, status(t, db)) }
}

func TestWorkerRetryThenFail(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	sent := 0
	w := NewWorker(s, depsWith(&sent, "Welcome", errors.New("smtp down"), nil), log.New(io.Discard, "", 0))
	for i := 0; i < 5; i++ {
		db.MustExec(`UPDATE ext.automation_enrollments SET next_at = now() - interval '1 second' WHERE status='active'`)
		w.Tick()
	}
	var sends int
	db.Get(&sends, `SELECT COUNT(*) FROM ext.automation_sends`)
	if sent != 0 || sends != 0 || status(t, db) != "failed" { t.Fatalf("sent %d sends %d status %s", sent, sends, status(t, db)) }
}

func TestWorkerGuardSkips(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	sent := 0
	w := NewWorker(s, depsWith(&sent, "Welcome", nil, errors.New("messenger not allowed")), log.New(io.Discard, "", 0))
	w.Tick()
	if sent != 0 || status(t, db) != "skipped" { t.Fatal(status(t, db)) }
}

func TestWorkerMarkerDefers(t *testing.T) {
	db, s, _, _ := claimSetup(t)
	sent := 0
	w := NewWorker(s, depsWith(&sent, "Hi\n<p>[[SENDER_INFO]]</p>\n", nil, nil), log.New(io.Discard, "", 0))
	w.Tick()
	var later bool
	db.Get(&later, `SELECT next_at > now() + interval '9 minutes' FROM ext.automation_enrollments`)
	if sent != 0 || status(t, db) != "active" || !later { t.Fatalf("sent %d status %s", sent, status(t, db)) }
}

func TestWorkerSkipsUnsubscribedAndDisabled(t *testing.T) {
	db, s, aid, sub := claimSetup(t)
	db.MustExec(`UPDATE subscriber_lists SET status='unsubscribed' WHERE subscriber_id=$1`, sub)
	sent := 0
	w := NewWorker(s, depsWith(&sent, "Welcome", nil, nil), log.New(io.Discard, "", 0))
	w.Tick()
	if sent != 0 || status(t, db) != "skipped" { t.Fatal("unsubscribed must skip") }
	_ = aid
}
```

- [ ] **Step 2: Run** `go test ./internal/automations/ -run TestWorker -v` → FAIL.

- [ ] **Step 3: Implement `worker.go`.** `process(c)` order (spec 6.3):
  1. `!c.Enabled` → `Finish(id, "skipped", "automation disabled")`; `!c.HasStep` → `Finish(id, "skipped", "no step")`.
  2. `Blocklisted` true → `Finish(..., "skipped", "blocklisted")`; `ActiveOn(sub, list)` false → `Finish(..., "skipped", "not active")`. A lookup error → `Retry(c, err.Error())`.
  3. `Guard` error → log `automations: guard blocked step %d/%d: %v` and `Finish(..., "skipped", "guard: "+err.Error())`.
  4. `Render` error → `Retry(c, "render: "+err.Error())`.
  5. `strings.Contains(msg.Content(), Marker)` → `Defer(c, 10*time.Minute, "sender info marker")`; log `automations: marker in step %d/%d` only when the last log for key `"<aid>/<pos>"` is older than 1 hour.
  6. `Send` error → `Retry(c, err.Error())`; nil → `Advance(c)`.
  An error from a store call is logged with prefix `automations:` and the loop continues.

- [ ] **Step 4: Run** `go test ./internal/automations/ -v` → PASS (all tasks so far).

- [ ] **Step 5: Commit** (`worker.go`, `worker_test.go`), message `Automations: worker loop with guard, marker, retry and skip rules`.

---

### Task 6: Hidden step campaigns in upstream campaign routes

**Files:**
- Modify: `queries/campaigns.sql` (`query-campaigns`), `queries/analytics.sql` (dashboard campaign counts near lines 282/291), `queries/ext.sql` (brand dashboard counts near line 126), `cmd/campaigns.go` (`UpdateCampaign`, `UpdateCampaignStatus`, `DeleteCampaign`, `DeleteCampaigns`)
- Test: `internal/automations/hidden_db_test.go` (SQL), e2e in Task 10

**Interfaces:**
- Consumes: `Store.StepOwner` (Task 1); `App.automations *automations.Store` (field added in this task to `cmd/main.go` `App` struct, built with `automations.NewStore(db)`); `brands.Service.GuardSender(listIDs, messenger, fromEmail, brands.TemplateRef)`.
- Produces: `func (a *App) stepCampaignGuard(c echo.Context, id int, req *campReq) error` in `cmd/automations.go`; constant message `msgAutomationEmail = "This campaign is an automation email. Change it on the Automations page."`.

- [ ] **Step 1: Write the failing SQL test** `hidden_db_test.go`: load `../../queries/campaigns.sql` with goyesql, prepare `query-campaigns` (replace `%query%`/`%order%` the way `cmd` does: `%order%` → `c.created_at DESC`), create one normal campaign and one step campaign, and assert the list call (`$1 = 0`) returns only the normal one while `$1 = <step id>` returns the step campaign:

```go
func TestHiddenCampaignNotListed(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	l := mkList(t, db, "TA", "single", "ta")
	normal := mkCampaign(t, db, l)
	step := mkCampaign(t, db, l)
	if _, err := s.Create("a", l, step); err != nil { t.Fatal(err) }
	q := loadQuery(t, "../../queries/campaigns.sql", "query-campaigns", map[string]string{"%order%": "c.created_at DESC"})
	ids := func(id int) []int {
		var out []int
		rows, err := db.Queryx(q, id, pq.StringArray{}, pq.StringArray{}, "", true, pq.Array([]int{}), 0, 0, nil)
		if err != nil { t.Fatal(err) }
		defer rows.Close()
		for rows.Next() { m := map[string]any{}; rows.MapScan(m); out = append(out, int(m["id"].(int64))) }
		return out
	}
	if got := ids(0); len(got) != 1 || got[0] != normal { t.Fatalf("list: %v", got) }
	if got := ids(step); len(got) != 1 || got[0] != step { t.Fatalf("by id: %v", got) }
}
```

  (`loadQuery` reads the file with `github.com/knadh/goyesql/v2`, picks the named query and applies the replacements. Match the argument count to the current `query-campaigns` signature; read `cmd/campaigns.go` `GetCampaigns` / `core.QueryCampaigns` for the exact order before writing the call.)

- [ ] **Step 2: Run** → FAIL (step campaign listed).

- [ ] **Step 3: SQL changes.** In `query-campaigns` add before `ORDER BY`:

```sql
    -- FORK(hcm): automation step campaigns are hidden from lists (not from a lookup by id).
    AND ($1 <> 0 OR NOT EXISTS (SELECT 1 FROM ext.automation_steps s WHERE s.campaign_id = c.id))
```

  In the dashboard campaign count and sent-sum queries in `analytics.sql` and the brand variant in `ext.sql`, add `AND NOT EXISTS (SELECT 1 FROM ext.automation_steps s WHERE s.campaign_id = <alias>.id)` with a `FORK(hcm)` comment (read each query and use its table alias).

- [ ] **Step 4: Handler guards** in `cmd/campaigns.go`, each marked `// FORK(hcm):`:
  - `UpdateCampaignStatus` and `DeleteCampaign`: at the top, `if _, ok, err := a.automations.StepOwner(id); err != nil { return 500 } else if ok { return echo.NewHTTPError(http.StatusBadRequest, msgAutomationEmail) }`.
  - `DeleteCampaigns` (bulk): drop step campaign ids from the request's ids before deleting (query: `SELECT campaign_id FROM ext.automation_steps WHERE campaign_id = ANY($1)`); if every id is a step, return 400 `msgAutomationEmail`. If the bulk delete is query-based, add `AND NOT EXISTS (... automation_steps ...)` to its SQL instead.
  - `UpdateCampaign`: after the request is parsed and validated, call `a.stepCampaignGuard(c, id, &req)`; a non-nil error is returned as is.
  - `stepCampaignGuard` (in `cmd/automations.go`): not a step → nil. Step:
    1. `req.Lists` must be exactly `[info.ListID]` → else 400 `"An automation email must stay on the automation list."`.
    2. `info.Enabled` and `!auth.GetUser(c).HasPerm("campaigns:send")` → 403 `"Sending permission (campaigns:send) is needed to change the email of an enabled automation."`.
    3. `info.Enabled` and any of `req.Subject`, `req.Body`, `req.AltBody`, `req.PreviewText` contains `automations.Marker` → 400 `"Replace [[SENDER_INFO]] before you save an enabled automation email."`.
    4. `info.Enabled` → `a.brands.GuardSender([]int{info.ListID}, req.Messenger, req.FromEmail, brands.TemplateRef{ID: templateID(req), Visual: req.ContentType == models.CampaignContentTypeVisual})` error → 400 with the guard text. (`templateID(req)` is `req.TemplateID` or 0; read `campReq` for the exact field names.)

- [ ] **Step 5: Run** `go test ./... ` → PASS; `go build ./...` → OK.

- [ ] **Step 6: Commit** (the SQL files, `cmd/campaigns.go`, `cmd/automations.go`, `cmd/main.go`, `internal/automations/hidden_db_test.go`), message `Automations: hide step campaigns and guard their status, delete and edits`.

---

### Task 7: Automations HTTP API, route classes and worker start

**Files:**
- Modify: `cmd/automations.go`, `cmd/handlers.go`, `internal/brands/routes.go`, `cmd/main.go`
- Test: `cmd/automations_test.go` (pure helpers), e2e in Task 10

**Interfaces:**
- Consumes: Tasks 1–6.
- Produces handlers (registration in `handlers.go`, next to the `/api/ext/brands` block):

```go
		// FORK(hcm): automations.
		g.GET("/api/ext/automations", pm(a.GetAutomations, "campaigns:get_all", "campaigns:get"))
		g.POST("/api/ext/automations", pm(a.CreateAutomation, "campaigns:manage_all", "campaigns:manage"))
		g.GET("/api/ext/automations/:id", pm(hasID(a.GetAutomation), "campaigns:get_all", "campaigns:get"))
		g.PUT("/api/ext/automations/:id", pm(hasID(a.UpdateAutomation), "campaigns:manage_all", "campaigns:manage"))
		g.DELETE("/api/ext/automations/:id", pm(hasID(a.DeleteAutomation), "campaigns:manage_all", "campaigns:manage"))
		g.POST("/api/ext/automations/enroll", pm(a.EnrollAutomation, "subscribers:manage"))
```

  (Check the exact permission names used by the existing campaign routes in `handlers.go` and copy them.)

- Route classes in `internal/brands/routes.go`: the five `/api/ext/automations...` CRUD routes → `BrandAware`; `POST /api/ext/automations/enroll` → `BrandAware` (the handler returns 404 when a brand is selected and the list is outside it). `CheckRouteTable` must report no unclassified route.

- [ ] **Step 1: Write the failing unit test** `cmd/automations_test.go` for the enable-rule helper:

```go
func TestEnableProblems(t *testing.T) {
	ok := []stepContent{{Subject: "Welcome", Body: "<p>Hi</p>"}}
	if p := enableProblems(ok); p != "" { t.Fatalf("clean: %q", p) }
	if p := enableProblems(nil); p != "The automation has no steps." { t.Fatalf("no steps: %q", p) }
	bad := []stepContent{{Subject: "Welcome", Body: "<p>[[SENDER_INFO]]</p>"}}
	if p := enableProblems(bad); p != "Step 1 still contains [[SENDER_INFO]]." { t.Fatalf("marker: %q", p) }
	pv := []stepContent{{Subject: "W", PreviewText: "[[SENDER_INFO]]"}}
	if p := enableProblems(pv); p != "Step 1 still contains [[SENDER_INFO]]." { t.Fatalf("preview: %q", p) }
}
```

  `type stepContent struct { Subject, Body, AltBody, PreviewText string }`; `enableProblems` returns the first problem, steps numbered from 1.

- [ ] **Step 2: Run** `go test ./cmd/ -run TestEnableProblems -v` → FAIL.

- [ ] **Step 3: Implement the handlers** (spec 7, 6.4, 6.5):
  - Brand scope: `_, brandLists, scoped := brands.BrandFrom(c)`. `GetAutomations` passes `brandLists` when scoped, else `nil`. Single-item handlers return 404 when scoped and the automation's list is not in `brandLists`; `CreateAutomation` returns 400 `"Select a list of this brand."` in that case.
  - `CreateAutomation` body `{name, list_id, messenger?, from_email?}`: validate name (1–200 chars) and that the list exists (404). Messenger default: the first entry of the list brand's `messengers` (`a.brands.Store.Get(slug)` via `brands.BrandOfList`; no brand → `"email"`); from default: `a.cfg.FromEmail`. Create the hidden campaign with `a.core.CreateCampaign(models.Campaign{Name: "Automation: " + name, Subject: "Welcome", FromEmail: from, Body: "", ContentType: models.CampaignContentTypeRichtext, Type: models.CampaignTypeRegular, Messenger: messenger, TemplateID: <default template id>, Archive: false, ...}, []int{listID}, nil)` (read `CreateCampaign` for the required fields), then `a.automations.Create(name, listID, camp.ID)`. If `Create` fails, delete the campaign.
  - `GetAutomation` returns `{automation, steps: [{position, delay_minutes, campaign_id, campaign: {id, name, subject, messenger, from_email, views, clicks, bounces}, sent}], counts}`; campaign stats come from `a.core.GetCampaign` plus the existing campaign stats query used by the campaigns list (read how `GetCampaigns` fills `views`/`clicks`/`bounces`); `sent` per step is `SELECT COUNT(*) FROM ext.automation_sends WHERE campaign_id=$1` (add `func (s *Store) SentByCampaign(id int) (int, error)` to the store with a test in `store_db_test.go`).
  - `UpdateAutomation` body `{name, list_id, enabled, steps:[{position, delay_minutes}]}`:
    - `enabled=true` and the user lacks `campaigns:send` → 403 `"Sending permission (campaigns:send) is needed to enable an automation."`.
    - `enabled=true` → load each step campaign, build `[]stepContent`, `enableProblems` non-empty → 400 with it; each step campaign fails `a.brands.GuardSender([]int{listID}, camp.Messenger, camp.FromEmail, brands.TemplateRef{ID: camp.TemplateID, Visual: camp.ContentType == models.CampaignContentTypeVisual})` → 400 `"Step N: <guard text>"`.
    - `ErrEnabledOnList` → 409; `ErrHasEnrollments` → 400; delays must be 0–525600 → else 400.
  - `DeleteAutomation`: `ErrEnabled` → 400 `"Disable the automation first."`; on success delete each returned campaign with `a.core.DeleteCampaign(id)` (the step rows are gone, so `RESTRICT` no longer applies).
  - `EnrollAutomation` body `{subscriber_id, list_id}`: list missing → 404; subscriber missing → 404; `auth.GetUser(c).HasListPerm(auth.PermTypeManage, listID)` error → 403; scoped and list outside the brand → 404; then `a.automations.Enroll` → 200 `{"data": {"enrolled": bool, "reason": string}}`.
- [ ] **Step 4: Start the worker** in `cmd/main.go` after `go mgr.Run()`:

```go
	// FORK(hcm): automations worker (welcome emails).
	autoWorker := automations.NewWorker(autoStore, automationDeps(core, mgr, brandSvc, lo), lo)
	go autoWorker.Run(context.Background(), 30*time.Second)
```

  `autoStore` is the `automations.NewStore(db)` value that Task 6 created and assigned to `App.automations`; reuse it (do not create a second store). `automationDeps` lives in `cmd/automations.go`:
  - `Blocklisted`: `core.GetSubscriber(id, "", "")` → `sub.Status == models.SubscriberStatusBlockListed`.
  - `Guard(campaignID)`: `camp := core.GetCampaign(campaignID, "", "")`; list ids from `camp.Lists`; `brandSvc.GuardSender(listIDs, camp.Messenger, camp.FromEmail, brands.TemplateRef{ID: camp.TemplateID (0 when nil), Visual: camp.ContentType == models.CampaignContentTypeVisual})`.
  - `Render(campaignID, subscriberID)`: same steps as `sendTestMessage` (`LoadInlineImages`, `CompileTemplate(mgr.TemplateFuncs(&camp))`, `mgr.NewCampaignMessage(&camp, sub)`), returning a `Message` whose `Content()` is `msg.Subject() + "\n" + string(msg.Body()) + "\n" + string(msg.AltBody())` and whose `Send()` is `mgr.SendCampaignMessage(msg)`.
- [ ] **Step 5: Run** `go test ./...` → PASS; start a local instance (Task 10 setup) and check the log has no `unclassified routes` line.
- [ ] **Step 6: Commit** (`cmd/automations.go`, `cmd/automations_test.go`, `cmd/handlers.go`, `cmd/main.go`, `internal/brands/routes.go`, store additions), message `Automations: API, enable rules, enroll endpoint and worker start`.

---

### Task 8: Public form and opt-in confirm enrollment

**Files:**
- Modify: `cmd/public.go` (`processSubForm`, `confirmOptinSubscription`)
- Test: e2e in Task 10 (the handlers need a running app); unit-test the list-filter helper in `cmd/automations_test.go`

**Interfaces:**
- Consumes: `Store.ListIDsByUUID`, `Store.BeginIntents`, `Store.ResolveIntents` (Task 3).
- Produces: `func (a *App) beginSignupIntents(email string, listUUIDs []string) []int64` and `func (a *App) resolveSignupIntents(ids []int64)` in `cmd/automations.go`. Both log errors with prefix `automations:` and never return them.

- [ ] **Step 1: Implement the helpers.** `beginSignupIntents`: `ListIDsByUUID` → `BeginIntents(email, ids)`. `resolveSignupIntents`: `ResolveIntents(ids)`; log `automations: enrolled %d from the public form` when n > 0.
- [ ] **Step 2: Hook `processSubForm`.** Directly before `a.core.InsertSubscriber(newSub, ...)`:

```go
	// FORK(hcm): automations intent before the subscription change (spec 6.2.1).
	intentIDs := a.beginSignupIntents(req.Email, listUUIDs)
```

  On both success returns (`InsertSubscriber` and the 409 `UpdateSubscriberWithLists` branch) call `a.resolveSignupIntents(intentIDs)` before `return hasOptin, nil`.
- [ ] **Step 3: Hook `confirmOptinSubscription`.** Before `ConfirmOptionSubscription`: look up the subscriber by `subUUID` (`a.core.GetSubscriber(0, subUUID, "")`; on error skip the hook) and call `beginSignupIntents(sub.Email, listUUIDs)`; after the success, `resolveSignupIntents(ids)`.
- [ ] **Step 4: Run** `go build ./... && go test ./...` → PASS.
- [ ] **Step 5: Commit** (`cmd/public.go`, `cmd/automations.go`), message `Automations: enroll from the public form and the opt-in confirm page`.

---

### Task 9: Frontend

**Files:**
- Create: `frontend/src/views/Automations.vue`, `frontend/src/views/AutomationForm.vue`
- Modify: `frontend/src/api/index.js`, `frontend/src/router/index.js`, `frontend/src/components/Navigation.vue`

**Interfaces:**
- Consumes: the Task 7 API (JSON in snake_case; call with `{ camelCase: false }` as `getBrands` does).
- Produces API functions:

```js
// Automations (hcm fork).
export const getAutomations = () => http.get('/api/ext/automations', { camelCase: false, loading: models.campaigns });
export const getAutomation = (id) => http.get(`/api/ext/automations/${id}`, { camelCase: false });
export const createAutomation = (data) => http.post('/api/ext/automations', data, { camelCase: false });
export const updateAutomation = (id, data) => http.put(`/api/ext/automations/${id}`, data, { camelCase: false });
export const deleteAutomation = (id) => http.delete(`/api/ext/automations/${id}`);
```

- [ ] **Step 1: Router** — add after the campaigns routes:

```js
  {
    path: '/automations',
    name: 'automations',
    meta: { title: 'Automations', group: 'automations' },
    component: () => import('../views/Automations.vue'),
  },
  {
    path: '/automations/:id',
    name: 'automation',
    meta: { title: 'Automation', group: 'automations' },
    component: () => import('../views/AutomationForm.vue'),
  },
```

- [ ] **Step 2: Navigation** — between Campaigns and Users, matching the existing `b-menu-item` markup:

```vue
      <b-menu-item v-if="$can('campaigns:get_all', 'campaigns:get')" :to="{ name: 'automations' }" tag="router-link"
        :active="activeItem.automations" data-cy="automations" icon="email-fast-outline" label="Automations" />
```

  (Use an icon name that exists in the bundled icon set; check `frontend/src/assets/icons` or the existing icon list and pick the closest.)
- [ ] **Step 3: `Automations.vue`** — page header "Automations (n)", a "New" button (`$can('campaigns:manage_all', 'campaigns:manage')`) that opens a modal with Name and List (list select from the lists store, filtered to the current brand by the existing brand logic), and a `b-table` with columns: Name (link to `{ name: 'automation', params: { id } }`), List, Brand tag, Enabled (`b-switch`; on change call `updateAutomation` with the full object and show the server message on error, then reload), Enrolled, Sent, Failed, Unknown. Help text under the title: "Only new signups from the website form or the signup gateway start an automation. Imports do not."
- [ ] **Step 4: `AutomationForm.vue`** — fields Name, List (disabled when `counts.enrolled > 0`), Enabled switch, and the step 1 row: delay in minutes (`b-numberinput`, min 0; label "Send immediately" when 0), subject, messenger, from, "Edit email" (`router-link` to `{ name: 'campaign', params: { id: step.campaign_id } }`), "Stats" (link to the campaign analytics route used by the campaigns page for one campaign), and "Sent: n". Save calls `updateAutomation`. Delete (only when disabled) calls `deleteAutomation` and goes back to the list.
- [ ] **Step 5: Run** `bash scripts/brands-frontend-check.sh` (any new `/api/` URL in an `href` must use `brandURL`) and `cd frontend && yarn build` → both pass.
- [ ] **Step 6: Commit** (the five frontend files), message `Automations: list and edit pages`.

---

### Task 10: e2e script, TA conversion, PATCHES.md

**Files:**
- Create: `scripts/automations-e2e.sh`, `scripts/convert-eo-welcome.py`, `scripts/fixtures/ta-welcome.html`
- Modify: `PATCHES.md`

- [ ] **Step 1: `convert-eo-welcome.py`** (`python3 -I scripts/convert-eo-welcome.py IN OUT`):

```python
import re, sys
src, dst = sys.argv[1], sys.argv[2]
html = open(src, encoding="utf-8").read()
# Remove the EmailOctopus footer link (it is the only use of RewardsURL).
html = re.sub(r'<a[^>]*\{\{RewardsURL\}\}[^>]*>.*?</a>', '', html, flags=re.S)
repl = {"{{UnsubscribeURL}}": "{{ UnsubscribeURL }}", "{{WebVersionURL}}": "{{ MessageURL }}",
        "{{SenderInfo}}": "[[SENDER_INFO]]", "{{PreviewText}}": ""}
for k, v in repl.items():
    html = html.replace(k, v)
left = re.findall(r'\{\{[A-Za-z]+\}\}', html)
if left or "EmailOctopus" in html:
    sys.exit(f"FAIL: EmailOctopus content left: {sorted(set(left))} {'EmailOctopus' if 'EmailOctopus' in html else ''}")
open(dst, "w", encoding="utf-8").write(html)
print("OK", dst)
```

  Run it on `/Users/brady/HCM/tacticaladvisor/emails/welcome.html` → `scripts/fixtures/ta-welcome.html`; expected `OK`. Check by eye that the footer separator around the removed link still reads well (remove an orphan "|" or "·" next to it by editing the fixture if needed).
- [ ] **Step 2: `automations-e2e.sh`** in the style of `scripts/brands-e2e.sh` (same env vars, same local-only guard, same `--smtp-sink` reuse via `bash scripts/brands-e2e.sh --smtp-sink`). Checks, each printed `PASS`/`FAIL`:
  1. Create list `auto-e2e` (single opt-in, tag `ri`), create automation → 200, response has one step with a campaign id.
  2. Enable without `campaigns:send` (an API user with a role that has `campaigns:manage` only) → 403.
  3. Set the step campaign body to `<p>[[SENDER_INFO]]</p>` and enable → 400 with "still contains"; set body `<p>Welcome</p>`, from address on `rangeinsiders.com`, messenger `email` → guard 400 (messenger not in brand) when the instance messenger list lacks `email-ri`; use the messenger the brand allows on the test instance (`email-ri` defined as an SMTP messenger pointing at the sink), then enable → 200.
  4. `GET /api/campaigns?per_page=all` does not contain the step id; `GET /api/campaigns/<id>` → 200; `PUT /api/campaigns/<id>/status` `{"status":"running"}` → 400; `DELETE /api/campaigns/<id>` → 400.
  5. Create subscriber `e2e1@example.com` on the list via `POST /api/subscribers` with `preconfirm_subscriptions: true`, then `POST /api/ext/automations/enroll` → `enrolled: true`; repeat → `enrolled: false, reason: already-enrolled`; within 60 s the sink has exactly one mail to `e2e1@example.com` with header `X-Listmonk-Campaign: <step uuid>`.
  6. Import (`POST /api/import/subscribers`, mode subscribe) `e2e2@example.com` into the list → no mail to it within 60 s and no enrollment row (`GET /api/ext/automations/<id>` enrolled count unchanged).
  7. Public form `POST /subscription/form` with `email=e2e3@example.com` and the list UUID → one mail; the same form again → no second mail.
  8. Existing subscriber `e2e4@example.com` on another list submits the public form for `auto-e2e` → one mail.
  9. Disable, delete the automation → 200; the step campaign is gone (`GET /api/campaigns/<id>` → 404).
  10. `bash scripts/brands-e2e.sh` still reports `0 FAIL` (run separately; record the result).
- [ ] **Step 3: Run** both e2e scripts against a local instance built from this branch with the local test Postgres and the SMTP sink. Expected: all PASS (KNOWN-UPSTREAM items in `brands-e2e.sh` stay as before).
- [ ] **Step 4: `PATCHES.md`** — add "Automations (hcm branch)" with: spec link, new files, and a table of every upstream file touched (`cmd/main.go`, `cmd/handlers.go`, `cmd/campaigns.go`, `cmd/public.go`, `internal/manager/manager.go`, `internal/brands/routes.go`, `queries/campaigns.sql`, `queries/analytics.sql`, `queries/ext.sql`, `frontend/src/api/index.js`, `frontend/src/router/index.js`, `frontend/src/components/Navigation.vue`) with one line on each hunk.
- [ ] **Step 5: Commit** (scripts, fixture, `PATCHES.md`), message `Automations: e2e script, TA welcome conversion, patch notes`.

---

## Out of scope for this plan (separate follow-ups)

- email-infra gateway: replace `ports.sendWelcome` with an enroll call (spec 10 step 3). It is a different repository with its own tests and its own review gate. Do it after rollout step 2 passes on RI.
- Creating the TA automation on production and pasting `scripts/fixtures/ta-welcome.html` into its step campaign: after AWS account B, the `email-ta` messenger, the TA lists, and the postal address exist.
