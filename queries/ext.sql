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

-- name: ext-dashboard-counts
-- FORK(hcm): mat_dashboard_counts (schema.sql) computed live for one brand, with the same JSON shape.
-- $1 = brand list IDs ('{}' matches nothing). Subscribers are those on a brand list; campaigns are brand
-- campaigns (all lists in the brand). An orphan has no list, so it belongs to no brand and the count is 0.
WITH members AS (
    SELECT DISTINCT subscriber_id FROM subscriber_lists WHERE list_id = ANY($1::INT[])
),
subs AS (
    SELECT COUNT(*) AS num, s.status FROM subscribers s
    JOIN members m ON m.subscriber_id = s.id
    GROUP BY s.status
),
camps AS (
    SELECT c.status, c.sent FROM campaigns c
    -- FORK(hcm): automation step campaigns are not counted.
    WHERE NOT EXISTS (SELECT 1 FROM ext.automation_steps s WHERE s.campaign_id = c.id)
    AND EXISTS (SELECT 1 FROM campaign_lists x WHERE x.campaign_id = c.id)
    AND NOT EXISTS (
        SELECT 1 FROM campaign_lists x
        WHERE x.campaign_id = c.id AND (x.list_id IS NULL OR NOT (x.list_id = ANY($1::INT[])))
    )
)
SELECT JSON_BUILD_OBJECT(
    'subscribers', JSON_BUILD_OBJECT(
        'total', (SELECT SUM(num) FROM subs),
        'blocklisted', (SELECT num FROM subs WHERE status='blocklisted'),
        'orphans', 0
    ),
    'lists', JSON_BUILD_OBJECT(
        'total', (SELECT COUNT(*) FROM lists WHERE id = ANY($1::INT[])),
        'private', (SELECT COUNT(*) FROM lists WHERE id = ANY($1::INT[]) AND type='private'),
        'public', (SELECT COUNT(*) FROM lists WHERE id = ANY($1::INT[]) AND type='public'),
        'optin_single', (SELECT COUNT(*) FROM lists WHERE id = ANY($1::INT[]) AND optin='single'),
        'optin_double', (SELECT COUNT(*) FROM lists WHERE id = ANY($1::INT[]) AND optin='double')
    ),
    'campaigns', JSON_BUILD_OBJECT(
        'total', (SELECT COUNT(*) FROM camps),
        'by_status', (
            SELECT JSON_OBJECT_AGG (status, num) FROM
            (SELECT status, COUNT(*) AS num FROM camps GROUP BY status) r
        )
    ),
    'messages', (SELECT SUM(sent) AS messages FROM camps)
) AS data;

-- name: ext-dashboard-charts
-- FORK(hcm): mat_dashboard_charts (schema.sql) computed live for one brand, with the same JSON shape.
-- $1 = brand list IDs ('{}' matches nothing). Only views and clicks of brand campaigns count.
WITH bc AS (
    SELECT c.id FROM campaigns c
    WHERE EXISTS (SELECT 1 FROM campaign_lists x WHERE x.campaign_id = c.id)
    AND NOT EXISTS (
        SELECT 1 FROM campaign_lists x
        WHERE x.campaign_id = c.id AND (x.list_id IS NULL OR NOT (x.list_id = ANY($1::INT[])))
    )
),
clicks AS (
    SELECT JSON_AGG(ROW_TO_JSON(row))
    FROM (
        WITH viewDates AS (
          SELECT created_at::DATE AS to_date,
                 created_at::DATE - INTERVAL '30 DAY' AS from_date
                 FROM link_clicks WHERE campaign_id IN (SELECT id FROM bc) ORDER BY id DESC LIMIT 1
        )
        SELECT COUNT(*) AS count, created_at::DATE as date FROM link_clicks
          WHERE campaign_id IN (SELECT id FROM bc)
            AND created_at >= (SELECT from_date FROM viewDates)
            AND created_at < (SELECT to_date FROM viewDates) + INTERVAL '1 day'
          GROUP by date ORDER BY date
    ) row
),
views AS (
    SELECT JSON_AGG(ROW_TO_JSON(row))
    FROM (
        WITH viewDates AS (
          SELECT created_at::DATE AS to_date,
                 created_at::DATE - INTERVAL '30 DAY' AS from_date
                 FROM campaign_views WHERE campaign_id IN (SELECT id FROM bc) ORDER BY id DESC LIMIT 1
        )
        SELECT COUNT(*) AS count, created_at::DATE as date FROM campaign_views
          WHERE campaign_id IN (SELECT id FROM bc)
            AND created_at >= (SELECT from_date FROM viewDates)
            AND created_at < (SELECT to_date FROM viewDates) + INTERVAL '1 day'
          GROUP by date ORDER BY date
    ) row
)
SELECT JSON_BUILD_OBJECT('link_clicks', COALESCE((SELECT * FROM clicks), '[]'),
                         'campaign_views', COALESCE((SELECT * FROM views), '[]')
                        ) AS data;

-- name: ext-query-subscribers-for-export
-- raw: true
-- FORK(hcm): copy of query-subscribers-for-export for a brand view. $1 = brand list IDs; a subscriber
-- matches when on ANY of them (upstream uses = ALL). DISTINCT because the join yields one row per list.
-- Unprepared statement for issuring arbitrary WHERE conditions for
-- searching subscribers to do bulk CSV export.
SELECT DISTINCT subscribers.id,
       subscribers.uuid,
       subscribers.email,
       subscribers.name,
       subscribers.first_name,
       subscribers.last_name,
       subscribers.status,
       subscribers.attribs,
       subscribers.created_at,
       subscribers.updated_at
       FROM subscribers
    LEFT JOIN subscriber_lists
    ON (
        -- Optional list filtering.
        (CASE WHEN CARDINALITY($1::INT[]) > 0 THEN true ELSE false END)
        AND subscriber_lists.subscriber_id = subscribers.id
        AND ($4 = '' OR subscriber_lists.status = $4::subscription_status)
    )
    WHERE subscriber_lists.list_id = ANY($1::INT[]) AND id > $2
    AND (CASE WHEN CARDINALITY($3::INT[]) > 0 THEN id=ANY($3) ELSE true END)
    AND (CASE WHEN $5 != '' THEN name ~* $5 OR email ~* $5 ELSE TRUE END)
    AND (%query%)
    ORDER BY subscribers.id ASC LIMIT (CASE WHEN $6 < 1 THEN NULL ELSE $6 END);
