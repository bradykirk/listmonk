-- Automations (hcm fork). Spec: docs/superpowers/specs/2026-10-12-automations-design.md
-- Runs after brands.Bootstrap, which creates schema ext.
CREATE TABLE IF NOT EXISTS ext.automations (
  id          SERIAL PRIMARY KEY,
  name        TEXT NOT NULL,
  list_id     INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
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

CREATE TABLE IF NOT EXISTS ext.automation_intents (
  id          BIGSERIAL PRIMARY KEY,
  email       TEXT NOT NULL,         -- lower-cased; the subscriber id is unknown for a new email
  list_id     INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
  was_active  BOOLEAN NOT NULL,      -- 6.0 state before the change
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS automation_intents_created ON ext.automation_intents (created_at);
-- swept_at rotates the sweep: rows that stay pending go to the back of the queue.
ALTER TABLE ext.automation_intents ADD COLUMN IF NOT EXISTS swept_at TIMESTAMPTZ;
