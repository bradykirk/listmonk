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
-- Per-campaign opens, clicks, bounces and unsubscribes. Rates are computed in
-- the frontend so that a zero send count cannot divide by zero here.
--
-- Bounces are split by type. Mailbox providers treat them differently: a hard
-- bounce is a permanent address failure and counts hardest against sender
-- reputation, a soft bounce is usually a full mailbox or a temporary defer, and
-- a complaint is a "mark as spam". SES suspends on 5% bounces or 0.1%
-- complaints, so the two need separate numbers, not one combined figure.
--
-- `unsubscribes` is ATTRIBUTED, not exact. listmonk's unsubscribe handler sets
-- subscriber_lists.status and updated_at and stores no campaign reference, so
-- the true figure cannot be recovered. This counts unsubscribes from the
-- campaign's own lists in the 72 hours after it started. Two campaigns sent to
-- the same list inside one window will both claim the same unsubscribe.
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
           FROM bounces b WHERE b.campaign_id = c.id AND b.type = 'hard')             AS hard_bounces,
        (SELECT COUNT(*)::int
           FROM bounces b WHERE b.campaign_id = c.id AND b.type = 'soft')             AS soft_bounces,
        (SELECT COUNT(*)::int
           FROM bounces b WHERE b.campaign_id = c.id AND b.type = 'complaint')        AS complaints,
        (SELECT COUNT(*)::int
           FROM subscriber_lists sl
           JOIN campaign_lists cl ON cl.campaign_id = c.id AND cl.list_id = sl.list_id
          WHERE sl.status = 'unsubscribed'
            AND c.started_at IS NOT NULL
            AND sl.updated_at >= c.started_at
            AND sl.updated_at <  c.started_at + INTERVAL '72 hours')                  AS unsubscribes
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


-- name: get-analytics-summary
-- The headline numbers, as one JSON object.
--
-- Audience counts respect the list filter ($1). Campaign rates do not: a
-- campaign is sent to a set of lists, so attributing its opens to one list
-- would be wrong. They cover the last 90 days, which is the window that
-- reputation at the mailbox providers actually reflects.
--
-- Every value is a raw count. Rates are divided in the frontend so that a zero
-- denominator is a display decision rather than a NULL from the database.
SELECT JSON_BUILD_OBJECT(
    'subscribers', (
        SELECT COUNT(DISTINCT sl.subscriber_id)::int
        FROM subscriber_lists sl
        JOIN subscribers s ON s.id = sl.subscriber_id
        WHERE s.status = 'enabled'
          AND sl.status <> 'unsubscribed'
          AND ($1 = 0 OR sl.list_id = $1)
    ),
    'joined_30d', (
        SELECT COUNT(DISTINCT sl.subscriber_id)::int
        FROM subscriber_lists sl
        WHERE sl.created_at > NOW() - INTERVAL '30 days'
          AND ($1 = 0 OR sl.list_id = $1)
    ),
    'unsubscribed_30d', (
        SELECT COUNT(DISTINCT sl.subscriber_id)::int
        FROM subscriber_lists sl
        WHERE sl.status = 'unsubscribed'
          AND sl.updated_at > NOW() - INTERVAL '30 days'
          AND ($1 = 0 OR sl.list_id = $1)
    ),
    'campaigns_90d', (
        SELECT COUNT(*)::int FROM campaigns
        WHERE status = 'finished' AND started_at > NOW() - INTERVAL '90 days'
    ),
    'sent_90d', (
        SELECT COALESCE(SUM(sent), 0)::int FROM campaigns
        WHERE status = 'finished' AND started_at > NOW() - INTERVAL '90 days'
    ),
    'opens_90d', (
        SELECT COALESCE(SUM(n), 0)::int FROM (
            SELECT COUNT(DISTINCT cv.subscriber_id) AS n
            FROM campaign_views cv
            JOIN campaigns c ON c.id = cv.campaign_id
            WHERE c.status = 'finished' AND c.started_at > NOW() - INTERVAL '90 days'
            GROUP BY cv.campaign_id
        ) x
    ),
    'clicks_90d', (
        SELECT COALESCE(SUM(n), 0)::int FROM (
            SELECT COUNT(DISTINCT lc.subscriber_id) AS n
            FROM link_clicks lc
            JOIN campaigns c ON c.id = lc.campaign_id
            WHERE c.status = 'finished' AND c.started_at > NOW() - INTERVAL '90 days'
            GROUP BY lc.campaign_id
        ) x
    ),
    'bounces_90d', (
        SELECT COUNT(*)::int FROM bounces b
        JOIN campaigns c ON c.id = b.campaign_id
        WHERE b.type <> 'complaint'
          AND c.status = 'finished' AND c.started_at > NOW() - INTERVAL '90 days'
    ),
    'complaints_90d', (
        SELECT COUNT(*)::int FROM bounces b
        JOIN campaigns c ON c.id = b.campaign_id
        WHERE b.type = 'complaint'
          AND c.status = 'finished' AND c.started_at > NOW() - INTERVAL '90 days'
    )
);


-- name: get-analytics-domains
-- Engagement and bounces per mailbox provider.
--
-- This is the table that finds a deliverability problem before the overall rate
-- moves. If Gmail opens at 30% and Yahoo at 4%, the campaign is not the
-- problem: Yahoo is filtering it to spam, and only a per-provider split shows
-- that. Neither EmailOctopus nor stock listmonk reports it.
--
-- EXISTS subqueries rather than joins across campaign_views, link_clicks and
-- bounces: joining three one-to-many tables multiplies rows before the count.
-- All three are indexed on subscriber_id.
--
-- $1: list ID, or 0 for every list.
SELECT COALESCE(JSON_AGG(t ORDER BY t.subscribers DESC), '[]') FROM (
    WITH members AS (
        SELECT DISTINCT s.id, LOWER(SPLIT_PART(s.email, '@', 2)) AS domain
        FROM subscribers s
        JOIN subscriber_lists sl ON sl.subscriber_id = s.id
        WHERE sl.status <> 'unsubscribed'
          AND ($1 = 0 OR sl.list_id = $1)
    )
    SELECT
        m.domain,
        COUNT(*)::int AS subscribers,
        COUNT(*) FILTER (WHERE EXISTS (
            SELECT 1 FROM campaign_views cv WHERE cv.subscriber_id = m.id))::int AS openers,
        COUNT(*) FILTER (WHERE EXISTS (
            SELECT 1 FROM link_clicks lc WHERE lc.subscriber_id = m.id))::int    AS clickers,
        COUNT(*) FILTER (WHERE EXISTS (
            SELECT 1 FROM bounces b WHERE b.subscriber_id = m.id))::int          AS bounced
    FROM members m
    GROUP BY m.domain
    ORDER BY 2 DESC
    LIMIT 15
) t;


-- name: get-analytics-send-times
-- Open rate by the weekday and hour a campaign was sent.
--
-- Timestamps are read in the database's timezone, not the recipient's. With one
-- sender and a mostly US audience that is the useful reading anyway; treat it
-- as "when we press send", not "when they read".
--
-- Thin data lies here. A slot holding one campaign shows that campaign's open
-- rate, not a trend, so the campaign count is returned and the frontend hides
-- slots below a threshold.
SELECT COALESCE(JSON_AGG(t ORDER BY t.dow, t.hour), '[]') FROM (
    SELECT
        EXTRACT(DOW  FROM c.started_at)::int AS dow,
        EXTRACT(HOUR FROM c.started_at)::int AS hour,
        COUNT(*)::int                        AS campaigns,
        COALESCE(SUM(c.sent), 0)::int        AS sent,
        COALESCE(SUM((SELECT COUNT(DISTINCT cv.subscriber_id)
                        FROM campaign_views cv
                       WHERE cv.campaign_id = c.id)), 0)::int AS opens
    FROM campaigns c
    WHERE c.status = 'finished'
      AND c.started_at IS NOT NULL
    GROUP BY 1, 2
) t;


-- name: get-dashboard-growth
-- Signups, unsubscribes and audience size per day (or week) for the home
-- Dashboard's audience growth section, as one JSON object.
--
-- $1 IANA time zone of the viewer, $2 number of buckets, $3 'day' or 'week',
-- $4 the current time, $5 the range label (echoed back).
--
-- Counting rules:
-- * A person is a subscriber with at least one list subscription. Each person
--   counts once, on subscribers.created_at.
-- * A person is in the audience while enabled with at least one subscription
--   that is not unsubscribed.
-- * A person who is not in the audience left at the latest evidence of leaving:
--   an unsubscribed subscription's updated_at, the subscriber's updated_at when
--   not enabled, a campaign_unsubs row, or a bounce. The last two matter because
--   the unsubscribe-link and bounce paths do not always bump updated_at.
-- * A person blocklisted with no evidence later than a minute after creation
--   (a blocklist import, say) never joined and is left out.
-- * Timestamps in the future are clamped into the current bucket, so the last
--   point always equals audience_now.
--
-- History is rebuilt from current state, so it is approximate: deleted
-- subscribers and deleted memberships are gone, and a person who unsubscribed
-- and later rejoined counts by their current state.
--
-- Every parameter is cast once in params so that Postgres never has to infer a
-- type from a later context.
WITH params AS (
    SELECT $1::TEXT AS tz, $2::INT AS buckets, $3::TEXT AS unit, $4::TIMESTAMPTZ AS now, $5::TEXT AS label
),
rng AS (
    SELECT
        p.tz,
        p.unit,
        date_trunc(p.unit, p.now AT TIME ZONE p.tz)::DATE AS last_bucket,
        date_trunc(p.unit, p.now AT TIME ZONE p.tz)::DATE
            - (p.buckets - 1) * (CASE WHEN p.unit = 'week' THEN 7 ELSE 1 END) AS first_bucket,
        (CASE WHEN p.unit = 'week' THEN 7 ELSE 1 END) AS step,
        p.buckets
    FROM params p
),
subs AS (
    SELECT
        subscriber_id,
        BOOL_OR(status <> 'unsubscribed')                        AS has_active,
        MAX(updated_at) FILTER (WHERE status = 'unsubscribed')   AS unsub_at
    FROM subscriber_lists
    GROUP BY subscriber_id
),
cu AS (
    SELECT subscriber_id, MAX(created_at) AS at
    FROM campaign_unsubs
    WHERE subscriber_id IS NOT NULL
    GROUP BY subscriber_id
),
bo AS (
    SELECT subscriber_id, MAX(created_at) AS at
    FROM bounces
    GROUP BY subscriber_id
),
people AS (
    SELECT
        s.status,
        s.created_at,
        (s.status = 'enabled' AND sb.has_active) AS active,
        GREATEST(
            sb.unsub_at,
            CASE WHEN s.status <> 'enabled' THEN s.updated_at END,
            cu.at,
            CASE WHEN s.status = 'blocklisted' OR NOT sb.has_active THEN bo.at END
        ) AS evidence
    FROM subscribers s
    JOIN subs sb ON sb.subscriber_id = s.id
    LEFT JOIN cu ON cu.subscriber_id = s.id
    LEFT JOIN bo ON bo.subscriber_id = s.id
    WHERE s.created_at IS NOT NULL
),
marks AS (
    SELECT
        LEAST(date_trunc(r.unit, pe.created_at AT TIME ZONE r.tz)::DATE, r.last_bucket) AS joined_on,
        (CASE WHEN pe.active THEN NULL
         ELSE LEAST(
            date_trunc(r.unit, GREATEST(COALESCE(pe.evidence, pe.created_at), pe.created_at) AT TIME ZONE r.tz)::DATE,
            r.last_bucket)
         END) AS left_on
    FROM people pe
    CROSS JOIN rng r
    WHERE NOT (pe.status = 'blocklisted'
               AND COALESCE(pe.evidence, pe.created_at) < pe.created_at + INTERVAL '1 minute')
),
buckets AS (
    SELECT r.first_bucket + i * r.step AS d
    FROM rng r, generate_series(0, r.buckets - 1) AS i
),
joins AS (
    SELECT joined_on AS d, COUNT(*) AS n FROM marks GROUP BY 1
),
lefts AS (
    SELECT left_on AS d, COUNT(*) AS n FROM marks WHERE left_on IS NOT NULL GROUP BY 1
),
baseline AS (
    SELECT
        COUNT(*) FILTER (WHERE m.joined_on < r.first_bucket)
        - COUNT(*) FILTER (WHERE m.left_on < r.first_bucket) AS n
    FROM marks m
    CROSS JOIN rng r
),
series AS (
    SELECT
        b.d,
        COALESCE(j.n, 0)::INT AS signups,
        COALESCE(l.n, 0)::INT AS unsubscribes,
        (bl.n + SUM(COALESCE(j.n, 0) - COALESCE(l.n, 0)) OVER (ORDER BY b.d))::INT AS audience
    FROM buckets b
    LEFT JOIN joins j ON j.d = b.d
    LEFT JOIN lefts l ON l.d = b.d
    CROSS JOIN baseline bl
)
SELECT JSON_BUILD_OBJECT(
    'range',        (SELECT label FROM params),
    'bucket',       (SELECT unit FROM params),
    'tz',           (SELECT tz FROM params),
    'audience_now', (SELECT COUNT(*)::INT FROM marks WHERE left_on IS NULL),
    'signups',      (SELECT COALESCE(SUM(signups), 0)::INT FROM series),
    'unsubscribes', (SELECT COALESCE(SUM(unsubscribes), 0)::INT FROM series),
    'net',          (SELECT (COALESCE(SUM(signups), 0) - COALESCE(SUM(unsubscribes), 0))::INT FROM series),
    'series',       (SELECT COALESCE(JSON_AGG(JSON_BUILD_OBJECT(
                        'date', d, 'signups', signups, 'unsubscribes', unsubscribes, 'audience', audience
                    ) ORDER BY d), '[]') FROM series)
);
