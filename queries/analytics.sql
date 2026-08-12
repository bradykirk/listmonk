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
           FROM bounces b WHERE b.campaign_id = c.id)                                 AS bounces
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
