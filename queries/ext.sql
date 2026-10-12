-- name: ext-campaign-in-brand
-- FORK(hcm): true when the campaign has lists and ALL of them are in the brand ($2).
SELECT EXISTS (SELECT 1 FROM campaign_lists x WHERE x.campaign_id = $1)
   AND NOT EXISTS (
       SELECT 1 FROM campaign_lists x
       WHERE x.campaign_id = $1 AND (x.list_id IS NULL OR NOT (x.list_id = ANY($2::INT[])))
   );
