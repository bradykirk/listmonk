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

-- name: ext-subscriber-activity
-- FORK(hcm): copy of get-subscriber-activity limited to one brand. $2 = brand list IDs.
-- A view or click with no campaign, or a campaign outside the brand, is excluded.
WITH views AS (
    SELECT
        c.id,
        c.uuid,
        c.name,
        c.subject,
        COUNT(*) as view_count,
        MAX(cv.created_at) as last_viewed_at
    FROM campaign_views cv
    LEFT JOIN campaigns c ON c.id = cv.campaign_id
    WHERE cv.subscriber_id = $1
    AND (
        EXISTS (SELECT 1 FROM campaign_lists x WHERE x.campaign_id = c.id)
        AND NOT EXISTS (
            SELECT 1 FROM campaign_lists x
            WHERE x.campaign_id = c.id AND (x.list_id IS NULL OR NOT (x.list_id = ANY($2::INT[])))
        )
    )
    GROUP BY c.id, c.uuid, c.name, c.subject
    ORDER BY last_viewed_at DESC
),
clicks AS (
    SELECT
        l.id as link_id,
        l.url,
        c.id as campaign_id,
        c.uuid as campaign_uuid,
        c.name as campaign_name,
        c.subject as campaign_subject,
        COUNT(*) as click_count,
        MAX(lc.created_at) as last_clicked_at
    FROM link_clicks lc
    LEFT JOIN links l ON l.id = lc.link_id
    LEFT JOIN campaigns c ON c.id = lc.campaign_id
    WHERE lc.subscriber_id = $1
    AND (
        EXISTS (SELECT 1 FROM campaign_lists x WHERE x.campaign_id = c.id)
        AND NOT EXISTS (
            SELECT 1 FROM campaign_lists x
            WHERE x.campaign_id = c.id AND (x.list_id IS NULL OR NOT (x.list_id = ANY($2::INT[])))
        )
    )
    GROUP BY l.id, l.url, c.id, c.uuid, c.name, c.subject
    ORDER BY last_clicked_at DESC
)
SELECT
    COALESCE((SELECT JSON_AGG(v) FROM views v), '[]') as campaign_views,
    COALESCE((SELECT JSON_AGG(c) FROM clicks c), '[]') as link_clicks;

-- name: ext-export-subscriber-data
-- FORK(hcm): copy of export-subscriber-data limited to one brand. $3 = brand list IDs.
-- The profile is shared across brands and does not change.
WITH prof AS (
    SELECT id, uuid, email, name, attribs, status, created_at, updated_at FROM subscribers WHERE
    CASE WHEN $1 > 0 THEN id = $1 ELSE uuid = $2 END
),
subs AS (
    SELECT subscriber_lists.status AS subscription_status,
            (CASE WHEN lists.type = 'private' THEN 'Private list' ELSE lists.name END) as name,
            lists.type, subscriber_lists.created_at
    FROM lists
    LEFT JOIN subscriber_lists ON (subscriber_lists.list_id = lists.id)
    WHERE subscriber_lists.subscriber_id = (SELECT id FROM prof)
    AND lists.id = ANY($3::INT[])
),
views AS (
    SELECT subject as campaign, COUNT(subscriber_id) as views FROM campaign_views
        LEFT JOIN campaigns ON (campaigns.id = campaign_views.campaign_id)
        WHERE subscriber_id = (SELECT id FROM prof)
        AND (
            EXISTS (SELECT 1 FROM campaign_lists x WHERE x.campaign_id = campaigns.id)
            AND NOT EXISTS (
                SELECT 1 FROM campaign_lists x
                WHERE x.campaign_id = campaigns.id AND (x.list_id IS NULL OR NOT (x.list_id = ANY($3::INT[])))
            )
        )
        GROUP BY campaigns.id ORDER BY campaigns.id
),
clicks AS (
    SELECT url, COUNT(subscriber_id) as clicks FROM link_clicks
        LEFT JOIN links ON (links.id = link_clicks.link_id)
        WHERE subscriber_id = (SELECT id FROM prof)
        AND (
            EXISTS (SELECT 1 FROM campaign_lists x WHERE x.campaign_id = link_clicks.campaign_id)
            AND NOT EXISTS (
                SELECT 1 FROM campaign_lists x
                WHERE x.campaign_id = link_clicks.campaign_id AND (x.list_id IS NULL OR NOT (x.list_id = ANY($3::INT[])))
            )
        )
        GROUP BY links.id ORDER BY links.id
)
SELECT (SELECT email FROM prof) as email,
        COALESCE((SELECT JSON_AGG(t) FROM prof t), '{}') AS profile,
        COALESCE((SELECT JSON_AGG(t) FROM subs t), '[]') AS subscriptions,
        COALESCE((SELECT JSON_AGG(t) FROM views t), '[]') AS campaign_views,
        COALESCE((SELECT JSON_AGG(t) FROM clicks t), '[]') AS link_clicks;
