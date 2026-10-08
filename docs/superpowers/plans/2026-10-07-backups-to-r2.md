# Off-site backups of lists.gunmade.com to Cloudflare R2

## Goal

Never lose listmonk data. Two things are irreplaceable:

- Postgres (`listmonk-data` volume): subscribers, lists, opt-in state, campaigns, templates.
- Uploaded media (`listmonk-uploads` volume): images referenced by already-sent email.

Today nothing is backed up. Coolify (v4.3.23) has 0 S3 destinations and 0 backup schedules on the
`listmonk-private` application, and notifications are off.

## Constraints found

- Coolify deploys `docker-compose.gunmade.yml` from the `gunmade` branch of
  `github.com/bradykirk/listmonk` (application uuid `v12fidm9etaielunetuwnvbi`).
- The app is a Docker Compose *application*, so Coolify's database-backup feature (pg_dump) is not
  offered. Its "Storage backups" page only offers volume backups (`..._listmonk-data`,
  `..._listmonk-uploads`).
- A volume backup of a live Postgres data dir is not a consistent snapshot. Rejected for the DB.

## Design

### 1. Compose change (`docker-compose.gunmade.yml`)

Add a named volume `listmonk-dumps` mounted on the `db` service at `/backups`. Nothing else changes.
Existing volume names stay untouched.

### 2. Coolify scheduled task — logical dump

- Container: `db`. Schedule: `0 3 * * *` (UTC).
- Command:

  ```sh
  set -eu
  rm -f /backups/*.tmp
  f=/backups/listmonk-$(date -u +%Y%m%dT%H%M%SZ).dump
  trap 'rm -f "$f.tmp"' EXIT
  pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc -f "$f.tmp"
  pg_restore --list "$f.tmp" > /dev/null
  mv "$f.tmp" "$f"
  find /backups -name 'listmonk-*.dump' -mtime +6 -delete
  ```

- Local socket auth is `trust` in the official postgres image, so no password is needed.
- `pg_restore --list` validates the archive before it is published under its final name.
- Writing to `.tmp` then `mv` means a volume backup never ships a half-written dump as final.
- Failure path: the `EXIT` trap removes this run's partial file, and every run first removes any
  `.tmp` left by a killed run. Partial files never accumulate.
- Retention only runs after a new dump succeeded, so repeated failures never delete the last good
  dumps. Seven good dumps of this DB are small; the dump volume shares the host disk with Postgres,
  so disk usage is part of the first-week check.

### 3. Cloudflare R2

- Private bucket `gunmade-listmonk-backups`.
- R2 API token scoped to that bucket only, Object Read & Write. The user pastes keys; Claude does not.
- Bucket lock rule: objects cannot be deleted for 30 days (protects against a stolen token or a bad
  retention setting).

### 4. Coolify S3 destination

- S3 Storage → R2 endpoint `https://<account_id>.r2.cloudflarestorage.com`, region `auto`,
  bucket above. Validate the connection in Coolify.

### 5. Coolify storage backups

- `listmonk-dumps` volume → R2, daily at 03:30 UTC (30 min after the dump).
- `listmonk-uploads` volume → R2, daily at 04:00 UTC.
- Retention on S3: keep at least 35 days so Coolify never tries to delete a locked object.
- No backup of `listmonk-data` (inconsistent; covered by the dumps).

### 6. Alerts and freshness

- Enable one Coolify notification channel (email) with both **scheduled task failure** and
  **backup failure** events on. A failed dump alerts from the task itself, so the 03:30 upload
  shipping an older dump never goes unnoticed.
- The dump file name carries its UTC timestamp, so the age of the newest object in R2 is visible
  at a glance.
- Not in scope now: an external dead-man check (for example healthchecks.io pinged on success),
  which would also catch Coolify's scheduler itself stopping. Recommended follow-up.

### 7. Verification

- Deploy; confirm `listmonk-dumps` exists and app + db are healthy.
- Run the scheduled task once manually; confirm a `.dump` in `/backups`.
- Run both storage backups once manually; confirm objects in R2.
- Restore drill, both archives, in an isolated local stack:
  - Download the newest dump from R2, `pg_restore` it into a throwaway `postgres:17` container,
    and compare row counts of `subscribers`, `lists`, `campaigns` and `media` with production.
  - Download the uploads archive from R2, extract it into a fresh volume, and compare the file
    count and a checksum list (`find . -type f -exec sha256sum {} +`) with the live
    `/listmonk/uploads` in `listmonk_app`.
  - Start listmonk on the restored DB and volume, and load one restored media URL in the browser.
- Document both restore procedures (DB and media archive extraction) in `CUSTOMIZATIONS.md` (or the gunmade-branch equivalent).

## Risks

- Deploy restarts the app and db containers (brief downtime, no data change). The build is the same
  commit plus a compose edit; `--upgrade` has no pending migrations.
- If Coolify's volume backup stops the container while it runs, `db` would go down nightly. Check
  this on the first manual run.
- Hetzner server backups (optional, +20% server cost) are a second layer, not a replacement.
