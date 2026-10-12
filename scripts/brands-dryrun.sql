-- Brands production dry run. READ-ONLY: one SELECT (with CTEs), no CREATE, INSERT,
-- UPDATE, DELETE or SET. Safe to paste into psql on production before the deploy.
--
-- It inlines the initial brand rules (same values as brands.SeedInitial) and shows:
--   list      the brand(s) each list would get from its tags. Exactly one is good.
--   campaign  for each draft, scheduled, running or paused campaign: its brand, or the
--             BLOCK reason the send guard would apply.
--   template  the owner that the seed gives each template. Templates 1-4 must be the
--             upstream defaults (shared), 5-6 must be RI's.
--
-- The campaign rows apply all three send guard rules (spec 6.1): lists of one brand,
-- messenger in the brand's messengers, from domain equal to or a subdomain of one of
-- the brand's from domains (empty from_domains blocks). A from address that
-- net/mail.ParseAddress would reject shows as BLOCK: from address unparsed.
-- No running or scheduled campaign may show any BLOCK. Fix a BLOCK row before the
-- deploy, or the manager guard pauses that campaign.
--   privilege one row: true if the current role can CREATE in this database. Bootstrap
--             runs CREATE SCHEMA IF NOT EXISTS ext and exits without it. It must be true.
--
-- lists.tags is VARCHAR(100)[], so it is cast to TEXT[] for the overlap test.
WITH b(slug, list_tags, messengers, from_domains) AS (VALUES
  ('ri', '{ri,ri-cold}'::text[], '{email-ri}'::text[], '{rangeinsiders.com}'::text[]),
  ('ta', '{ta}', '{email-ta}', '{news.tacticaladvisor.net}'),
  ('ct', '{ct}', '{email-ct}', '{}')),
lb AS (
  SELECT l.id, l.name, ARRAY(SELECT b.slug FROM b WHERE l.tags::text[] && b.list_tags) AS brands FROM lists l),
cb AS (
  SELECT c.id, c.name, c.messenger, c.from_email,
    (SELECT lb.brands[1] FROM campaign_lists cl JOIN lb ON lb.id = cl.list_id WHERE cl.campaign_id = c.id LIMIT 1) AS brand,
    -- Domain of the parsed address: strip the display name, take the text after the last @.
    lower(substring(btrim(regexp_replace(c.from_email, '^.*<|>.*$', '', 'g')) from '[^@]*$')) AS dom
  FROM campaigns c WHERE c.status IN ('draft', 'scheduled', 'running', 'paused'))
SELECT 'list' AS kind, id, name, brands::text AS result FROM lb
UNION ALL
SELECT 'campaign', cb.id, cb.name,
  CASE
    WHEN NOT EXISTS (SELECT 1 FROM campaign_lists cl WHERE cl.campaign_id = cb.id) THEN 'BLOCK: no lists'
    WHEN EXISTS (SELECT 1 FROM campaign_lists cl LEFT JOIN lb ON lb.id = cl.list_id
                 WHERE cl.campaign_id = cb.id AND COALESCE(cardinality(lb.brands), 0) <> 1) THEN 'BLOCK: unassigned/ambiguous/deleted list'
    WHEN (SELECT COUNT(DISTINCT lb.brands[1]) FROM campaign_lists cl JOIN lb ON lb.id = cl.list_id WHERE cl.campaign_id = cb.id) > 1 THEN 'BLOCK: mixed brands'
    WHEN NOT EXISTS (SELECT 1 FROM b WHERE b.slug = cb.brand AND cb.messenger = ANY(b.messengers)) THEN 'BLOCK: messenger'
    -- net/mail.ParseAddress must accept the from address, or the guard blocks it. Strict
    -- anchored RFC 5322 atext forms only (over-blocking is the safe direction): a bare
    -- dot-atom address, or name-addr with a quoted or an atext-word display name.
    WHEN NOT (cb.from_email ~ '^[A-Za-z0-9!#$%&''*+/=?^_`{|}~-]+(\.[A-Za-z0-9!#$%&''*+/=?^_`{|}~-]+)*@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*$'
              OR cb.from_email ~ '^("[^"\\]*"|([A-Za-z0-9!#$%&''*+/=?^_`{|}~-]+\.?([[:space:]]+[A-Za-z0-9!#$%&''*+/=?^_`{|}~-]+\.?)*)?)[[:space:]]*<[A-Za-z0-9!#$%&''*+/=?^_`{|}~-]+(\.[A-Za-z0-9!#$%&''*+/=?^_`{|}~-]+)*@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*>[[:space:]]*$') THEN 'BLOCK: from address unparsed'
    WHEN NOT EXISTS (SELECT 1 FROM b, unnest(b.from_domains) AS d
                     WHERE b.slug = cb.brand AND d <> ''
                       AND (cb.dom = lower(d) OR right(cb.dom, length(d) + 1) = '.' || lower(d))) THEN 'BLOCK: from domain'
    ELSE 'brand ' || cb.brand || ' messenger ' || cb.messenger || ' from ' || cb.from_email
  END
FROM cb
UNION ALL
SELECT 'template', t.id, t.name,
  CASE WHEN t.id IN (5, 6) THEN 'seed: ri' WHEN t.id IN (1, 2, 3, 4) THEN 'seed: shared' ELSE 'seed: unassigned' END
FROM templates t
UNION ALL
SELECT 'privilege', 0, 'CREATE on ' || current_database(),
  has_database_privilege(current_user, current_database(), 'CREATE')::text
ORDER BY kind, id;
