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
-- Every running or scheduled campaign must show a brand, not BLOCK. Fix a BLOCK row
-- before the deploy, or the manager guard pauses that campaign.
-- Check messenger and from domain by eye (spec 6.1: the domain rule is suffix matching).
--
-- lists.tags is VARCHAR(100)[], so it is cast to TEXT[] for the overlap test.
WITH b(slug, list_tags, messengers, from_domains) AS (VALUES
  ('ri', '{ri,ri-cold}'::text[], '{email-ri}'::text[], '{rangeinsiders.com}'::text[]),
  ('ta', '{ta}', '{email-ta}', '{news.tacticaladvisor.net}'),
  ('ct', '{ct}', '{email-ct}', '{}')),
lb AS (
  SELECT l.id, l.name, ARRAY(SELECT b.slug FROM b WHERE l.tags::text[] && b.list_tags) AS brands FROM lists l)
SELECT 'list' AS kind, id, name, brands::text AS result FROM lb
UNION ALL
SELECT 'campaign', c.id, c.name,
  CASE
    WHEN NOT EXISTS (SELECT 1 FROM campaign_lists cl WHERE cl.campaign_id = c.id) THEN 'BLOCK: no lists'
    WHEN EXISTS (SELECT 1 FROM campaign_lists cl LEFT JOIN lb ON lb.id = cl.list_id
                 WHERE cl.campaign_id = c.id AND COALESCE(cardinality(lb.brands), 0) <> 1) THEN 'BLOCK: unassigned/ambiguous/deleted list'
    WHEN (SELECT COUNT(DISTINCT lb.brands[1]) FROM campaign_lists cl JOIN lb ON lb.id = cl.list_id WHERE cl.campaign_id = c.id) > 1 THEN 'BLOCK: mixed brands'
    ELSE 'brand ' || (SELECT lb.brands[1] FROM campaign_lists cl JOIN lb ON lb.id = cl.list_id WHERE cl.campaign_id = c.id LIMIT 1)
         || ' messenger ' || c.messenger || ' from ' || c.from_email
  END
FROM campaigns c WHERE c.status IN ('draft', 'scheduled', 'running', 'paused')
UNION ALL
SELECT 'template', t.id, t.name,
  CASE WHEN t.id IN (5, 6) THEN 'seed: ri' WHEN t.id IN (1, 2, 3, 4) THEN 'seed: shared' ELSE 'seed: unassigned' END
FROM templates t
ORDER BY kind, id;
