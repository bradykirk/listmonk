CREATE SCHEMA IF NOT EXISTS ext;

CREATE TABLE IF NOT EXISTS ext.brands (
  slug          TEXT PRIMARY KEY CHECK (slug ~ '^[a-z][a-z0-9-]{1,30}$'),
  name          TEXT NOT NULL CHECK (name <> ''),
  list_tags     TEXT[] NOT NULL DEFAULT '{}',
  messengers    TEXT[] NOT NULL DEFAULT '{}',
  from_domains  TEXT[] NOT NULL DEFAULT '{}',
  sort_order    INT NOT NULL DEFAULT 0,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ext.template_brands (
  template_id INT PRIMARY KEY REFERENCES templates(id) ON DELETE CASCADE,
  brand_slug  TEXT REFERENCES ext.brands(slug) ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS ext.media_brands (
  media_id    INT PRIMARY KEY REFERENCES media(id) ON DELETE CASCADE,
  brand_slug  TEXT REFERENCES ext.brands(slug) ON UPDATE CASCADE
);
