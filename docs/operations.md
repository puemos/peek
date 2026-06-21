# Internal Operations

Peek is intended to be run by one company or trusted team as internal infrastructure. This guide is the operator checklist for that model.

## Recommended Topology

Run `peekd` behind a TLS-terminating reverse proxy such as Caddy, nginx, Traefik, or your platform ingress. Set `--base-url` / `PEEK_BASE_URL` to the externally visible `https://...` URL so generated links, secure cookies, and HSTS are correct.

Use one primary `peekd` process per data directory. Peek stores mutable state in SQLite plus uploaded objects. Horizontal scaling is not the default operating model; if the project ever needs that, the storage, session, and migration model should be revisited deliberately instead of hidden behind ad hoc process replication.

## Persistent State

The data directory contains:

- `peek.db`: SQLite database with accounts, tokens, comments, settings, visits, audit logs, and upload metadata.
- `secret.key`: the signing/encryption secret generated on first run when `PEEK_SECRET` is not provided.
- `uploads/`: file-backed upload objects when `PEEK_STORAGE=file`.

Back up the whole data directory for simple deployments. For database-consistent snapshots while the server may be running, use:

```sh
PEEK_DATA=/var/lib/peek peekd backup /backups/peek-$(date +%F).db
```

If you use S3 storage, also back up or lifecycle-manage the bucket according to your internal retention policy. The SQLite backup contains object metadata, comments, auth state, settings, and analytics, not the remote object bytes themselves.

## Startup Configuration

Peek fails fast for invalid operational config: `base-url` must be an absolute `http` or `https` URL, `max-upload` must be greater than zero, storage must be `file` or `s3`, and total-size / retention limits cannot be negative.

Minimum production-like configuration:

```sh
peekd \
  --addr 127.0.0.1:7700 \
  --data /var/lib/peek \
  --base-url https://peek.example.com \
  --trusted-proxy
```

Set `--trusted-proxy` only when the reverse proxy is trusted and overwrites or controls `X-Forwarded-For`. Without it, Peek intentionally ignores forwarded IP headers for analytics and audit logs.

## First Run And Access

On first run, Peek creates a one-time setup URL in the logs and stores the setup code in the data directory. Use it to create the first admin. After an account exists, the setup file is removed and the setup route is closed.

For company usage, prefer OAuth with invite-only signup. Admins can configure Google or GitHub OAuth in Settings, create invite links, and promote or disable users later. Direct bearer tokens remain appropriate for internal automation, CI jobs, and agents.

## Quotas, Retention, And Storage

Use runtime settings for normal day-to-day limits: maximum upload size, total storage cap, per-token count/storage limits, retention days, auth options, and S3 credentials. Storage backend selection itself is a startup concern and requires restart because it changes where object bytes are read and written.

Retention cleanup runs in the background and deletes uploads older than the configured number of days. `0` disables automatic cleanup. Keep retention aligned with your company’s data handling policy rather than treating Peek as an unlimited archive.

For S3-compatible storage, private/link-local endpoints are blocked by default to avoid SSRF. Set `PEEK_S3_ALLOW_PRIVATE_ENDPOINT=true` only for explicit private deployments such as local MinIO or controlled internal object stores.

## Observability

Use `/healthz` for liveness and `/readyz` for readiness; readiness verifies the database is reachable. `peekd healthcheck` calls `/healthz` and can be used in container or service-manager probes.

Peek logs structured JSON to stderr. Set `PEEK_LOG_LEVEL=debug`, `info`, or `warn`. Audit events are persisted in SQLite and important operational failures are logged with structured fields.

`/metrics` exposes Prometheus text metrics. Protect it at the proxy or network layer when the server is reachable from outside a trusted network.

## Upgrades And Rollbacks

Before upgrading, take a SQLite backup and confirm you know where uploaded object bytes live. Run the new binary against a staging copy of the data directory when changing storage, auth, retention, or database-related code.

Rollback is simplest when the database has not been migrated by a newer binary. If a release includes migrations, treat rollback as restoring the pre-upgrade backup plus object storage state.

## Local Operator Smoke Test

After deployment or upgrade:

```sh
peekd healthcheck --addr https://peek.example.com
peek login --host https://peek.example.com
printf '<!doctype html><h1>Peek smoke</h1>' > /tmp/peek-smoke.html
peek upload /tmp/peek-smoke.html
peek list
```

Then open the uploaded page, add a comment, and verify the dashboard shows the upload and visit stats.
