-- name: record-bounce
-- Insert a bounce and count the bounces for the subscriber and either unsubscribe them,
WITH sub AS (
    SELECT id, status FROM subscribers WHERE CASE WHEN $1 != '' THEN uuid = $1::UUID ELSE email = $2 END
),
camp AS (
    SELECT id FROM campaigns WHERE $3 != '' AND uuid = $3::UUID
),
num AS (
    -- Add a +1 to include the current insertion that is happening.
    SELECT COUNT(*) + 1 AS num FROM bounces WHERE subscriber_id = (SELECT id FROM sub) AND type = $4
),
-- block1 and block2 will run when $8 = 'blocklist' and the number of bounces exceed $8.
block1 AS (
    UPDATE subscribers SET status='blocklisted'
    WHERE $9 = 'blocklist' AND (SELECT num FROM num) >= $8 AND id = (SELECT id FROM sub) AND (SELECT status FROM sub) != 'blocklisted'
),
block2 AS (
    UPDATE subscriber_lists SET status='unsubscribed'
    WHERE $9 = 'unsubscribe' AND (SELECT num FROM num) >= $8 AND subscriber_id = (SELECT id FROM sub) AND (SELECT status FROM sub) != 'blocklisted'
),
bounce AS (
    -- Record the bounce if the subscriber is not already blocklisted;
    INSERT INTO bounces (subscriber_id, campaign_id, type, source, meta, created_at)
    SELECT (SELECT id FROM sub), (SELECT id FROM camp), $4, $5, $6, $7
    WHERE NOT EXISTS (SELECT 1 WHERE (SELECT status FROM sub) = 'blocklisted' OR (SELECT num FROM num) > $8)
)
-- This delete  will only run when $9 = 'delete' and the number of bounces exceed $8.
DELETE FROM subscribers
    WHERE $9 = 'delete' AND (SELECT num FROM num) >= $8 AND id = (SELECT id FROM sub);

-- name: query-bounces
SELECT COUNT(*) OVER () AS total,
    bounces.id,
    bounces.type,
    bounces.source,
    bounces.meta,
    bounces.created_at,
    bounces.subscriber_id,
    subscribers.uuid AS subscriber_uuid,
    subscribers.email AS email,
    subscribers.status as subscriber_status,
    (
        CASE WHEN bounces.campaign_id IS NOT NULL
        THEN JSON_BUILD_OBJECT('id', bounces.campaign_id, 'name', campaigns.name)
        ELSE NULL END
    ) AS campaign
FROM bounces
LEFT JOIN subscribers ON (subscribers.id = bounces.subscriber_id)
LEFT JOIN campaigns ON (campaigns.id = bounces.campaign_id)
WHERE ($1 = 0 OR bounces.id = $1)
    AND ($2 = 0 OR bounces.campaign_id = $2)
    AND ($3 = 0 OR bounces.subscriber_id = $3)
    AND ($4 = '' OR bounces.source = $4)
    -- FORK(hcm): brand scope. $7 = brand list IDs, NULL = all brands. A bounce matches when its
    -- campaign is in the brand, or it has no campaign and its subscriber is on a brand list.
    AND ($7::INT[] IS NULL
        OR (bounces.campaign_id IS NOT NULL
            AND EXISTS (SELECT 1 FROM campaign_lists x WHERE x.campaign_id = bounces.campaign_id)
            AND NOT EXISTS (
                SELECT 1 FROM campaign_lists x
                WHERE x.campaign_id = bounces.campaign_id AND (x.list_id IS NULL OR NOT (x.list_id = ANY($7::INT[])))
            ))
        OR (bounces.campaign_id IS NULL AND EXISTS (
            SELECT 1 FROM subscriber_lists sl WHERE sl.subscriber_id = bounces.subscriber_id AND sl.list_id = ANY($7::INT[])
        )))
ORDER BY %order% OFFSET $5 LIMIT (CASE WHEN $6 < 1 THEN NULL ELSE $6 END);

-- name: delete-bounces
-- FORK(hcm): brand scope. $3 = brand list IDs, NULL = all brands (same predicate as query-bounces).
DELETE FROM bounces WHERE ($2 = TRUE OR id = ANY($1))
    AND ($3::INT[] IS NULL
        OR (bounces.campaign_id IS NOT NULL
            AND EXISTS (SELECT 1 FROM campaign_lists x WHERE x.campaign_id = bounces.campaign_id)
            AND NOT EXISTS (
                SELECT 1 FROM campaign_lists x
                WHERE x.campaign_id = bounces.campaign_id AND (x.list_id IS NULL OR NOT (x.list_id = ANY($3::INT[])))
            ))
        OR (bounces.campaign_id IS NULL AND EXISTS (
            SELECT 1 FROM subscriber_lists sl WHERE sl.subscriber_id = bounces.subscriber_id AND sl.list_id = ANY($3::INT[])
        )));

-- name: delete-bounces-by-subscriber
WITH sub AS (
    SELECT id FROM subscribers WHERE CASE WHEN $1 > 0 THEN id = $1 ELSE uuid = $2 END
)
DELETE FROM bounces WHERE subscriber_id = (SELECT id FROM sub);

-- name: blocklist-bounced-subscribers
WITH subs AS (
    SELECT subscriber_id FROM bounces
),
b AS (
    UPDATE subscribers SET status='blocklisted', updated_at=NOW()
    WHERE id = ANY(SELECT subscriber_id FROM subs)
)
UPDATE subscriber_lists SET status='unsubscribed', updated_at=NOW()
    WHERE subscriber_id = ANY(SELECT subscriber_id FROM subs);

