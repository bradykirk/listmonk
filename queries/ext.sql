-- name: ext-campaign-in-brand
-- FORK(hcm): true when the campaign has lists and ALL of them are in the brand ($2).
SELECT EXISTS (SELECT 1 FROM campaign_lists x WHERE x.campaign_id = $1)
   AND NOT EXISTS (
       SELECT 1 FROM campaign_lists x
       WHERE x.campaign_id = $1 AND (x.list_id IS NULL OR NOT (x.list_id = ANY($2::INT[])))
   );

-- name: ext-campaign-list-ids
-- FORK(hcm): the campaign's list IDs for the send guard. A deleted list (NULL list_id) becomes 0, which has no tags.
SELECT COALESCE(list_id, 0) FROM campaign_lists WHERE campaign_id = $1;
