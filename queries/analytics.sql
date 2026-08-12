-- Analytics queries for the list analytics dashboard (gunmade fork).
--
-- Each query returns a single JSON column. This matches the existing dashboard
-- pattern (get-dashboard-charts) and means the Go layer needs no result structs.
--
-- $1 is a list ID. Pass 0 to include every list.

-- name: get-analytics-growth
-- Weekly joins and the running total, for the audience growth chart.
-- The running total is computed in an outer query. Putting the window function
-- alongside the GROUP BY makes Postgres reject the ORDER BY as ungrouped.
SELECT COALESCE(JSON_AGG(t ORDER BY t.week), '[]') FROM (
    SELECT
        w.week,
        w.joined,
        w.unsubscribed,
        (SUM(w.joined) OVER (ORDER BY w.week))::int AS cumulative
    FROM (
        SELECT
            date_trunc('week', sl.created_at)::date                            AS week,
            COUNT(*)::int                                                       AS joined,
            (COUNT(*) FILTER (WHERE sl.status = 'unsubscribed'))::int           AS unsubscribed
        FROM subscriber_lists sl
        WHERE ($1 = 0 OR sl.list_id = $1)
        GROUP BY 1
    ) w
) t;


-- name: get-analytics-campaigns
-- Per-campaign opens, clicks and bounces. Rates are computed in the frontend so
-- that a zero send count cannot divide by zero here.
SELECT COALESCE(JSON_AGG(t ORDER BY t.started_at DESC NULLS LAST), '[]') FROM (
    SELECT
        c.id,
        c.name,
        c.subject,
        c.started_at,
        c.sent::int                                                                   AS sent,
        COUNT(DISTINCT v.subscriber_id)::int                                          AS unique_opens,
        (SELECT COUNT(DISTINCT lc.subscriber_id)::int
           FROM link_clicks lc WHERE lc.campaign_id = c.id)                           AS unique_clicks,
        (SELECT COUNT(*)::int
           FROM bounces b WHERE b.campaign_id = c.id AND b.type <> 'complaint')       AS bounces,
        (SELECT COUNT(*)::int
           FROM bounces b WHERE b.campaign_id = c.id AND b.type = 'complaint')        AS complaints
    FROM campaigns c
    LEFT JOIN campaign_views v ON v.campaign_id = c.id
    WHERE c.status = 'finished'
    GROUP BY c.id
    ORDER BY c.started_at DESC NULLS LAST
    LIMIT 50
) t;


-- name: get-analytics-cohorts
-- How much of the list still engages. This is the number that decides whether
-- a sunset policy is worth running.
--
-- Uses correlated subqueries rather than a join across campaign_views and
-- link_clicks. Both tables are indexed on subscriber_id, and joining them
-- together multiplies rows before aggregation.
SELECT COALESCE(JSON_AGG(t ORDER BY t.cohort), '[]') FROM (
    WITH members AS (
        SELECT DISTINCT sl.subscriber_id AS id
        FROM subscriber_lists sl
        WHERE sl.status <> 'unsubscribed'
          AND ($1 = 0 OR sl.list_id = $1)
    ),
    activity AS (
        SELECT m.id,
               GREATEST(
                   COALESCE((SELECT MAX(created_at) FROM campaign_views
                              WHERE subscriber_id = m.id), 'epoch'::timestamptz),
                   COALESCE((SELECT MAX(created_at) FROM link_clicks
                              WHERE subscriber_id = m.id), 'epoch'::timestamptz)
               ) AS last_seen
        FROM members m
        JOIN subscribers s ON s.id = m.id AND s.status = 'enabled'
    )
    SELECT
        CASE
            WHEN last_seen = 'epoch'::timestamptz        THEN '0_never'
            WHEN last_seen > NOW() - INTERVAL '30 days'  THEN '1_active_30d'
            WHEN last_seen > NOW() - INTERVAL '90 days'  THEN '2_active_90d'
            WHEN last_seen > NOW() - INTERVAL '180 days' THEN '3_active_180d'
            ELSE                                              '4_cold'
        END           AS cohort,
        COUNT(*)::int AS subscribers
    FROM activity
    GROUP BY 1
) t;


-- name: get-analytics-campaign-timeline
-- Opens and clicks per hour for the first 48 hours after a campaign started.
-- generate_series supplies every bucket so the chart has no gaps.
-- $1: campaign ID.
SELECT COALESCE(JSON_AGG(t ORDER BY t.hour), '[]') FROM (
    SELECT h.hour,
           COALESCE(v.opens, 0)  AS opens,
           COALESCE(l.clicks, 0) AS clicks
    FROM generate_series(0, 47) AS h(hour)
    LEFT JOIN (
        SELECT FLOOR(EXTRACT(EPOCH FROM (cv.created_at - c.started_at)) / 3600)::int AS hour,
               COUNT(*)::int AS opens
        FROM campaign_views cv
        JOIN campaigns c ON c.id = cv.campaign_id
        WHERE cv.campaign_id = $1
        GROUP BY 1
    ) v ON v.hour = h.hour
    LEFT JOIN (
        SELECT FLOOR(EXTRACT(EPOCH FROM (lc.created_at - c.started_at)) / 3600)::int AS hour,
               COUNT(*)::int AS clicks
        FROM link_clicks lc
        JOIN campaigns c ON c.id = lc.campaign_id
        WHERE lc.campaign_id = $1
        GROUP BY 1
    ) l ON l.hour = h.hour
) t;


-- name: get-analytics-campaign-links
-- Top links for one campaign, with total and unique click counts.
-- $1: campaign ID.
SELECT COALESCE(JSON_AGG(t ORDER BY t.clicks DESC), '[]') FROM (
    SELECT l.url,
           COUNT(*)::int                          AS clicks,
           COUNT(DISTINCT lc.subscriber_id)::int  AS unique_clickers
    FROM link_clicks lc
    JOIN links l ON l.id = lc.link_id
    WHERE lc.campaign_id = $1
    GROUP BY l.url
    ORDER BY 2 DESC
    LIMIT 20
) t;


-- name: get-analytics-activity
-- Recent per-subscriber events, newest first. This is the feed EmailOctopus
-- shows beside a campaign report.
--
-- The joins to subscribers drop rows whose subscriber_id is NULL, which is what
-- listmonk writes when individual subscriber tracking is off. With that setting
-- off this returns nothing, and that is correct.
--
-- $1: campaign ID, or 0 for every campaign.
SELECT COALESCE(JSON_AGG(t ORDER BY t.created_at DESC), '[]') FROM (
    SELECT u.email, u.subscriber_id, u.action, u.created_at, u.campaign
    FROM (
        SELECT s.email, s.id AS subscriber_id, 'opened' AS action,
               v.created_at, c.name AS campaign
        FROM campaign_views v
        JOIN subscribers s ON s.id = v.subscriber_id
        JOIN campaigns c   ON c.id = v.campaign_id
        WHERE ($1 = 0 OR v.campaign_id = $1)

        UNION ALL

        SELECT s.email, s.id, 'clicked', lc.created_at, c.name
        FROM link_clicks lc
        JOIN subscribers s ON s.id = lc.subscriber_id
        JOIN campaigns c   ON c.id = lc.campaign_id
        WHERE ($1 = 0 OR lc.campaign_id = $1)
    ) u
    ORDER BY u.created_at DESC
    LIMIT 50
) t;
