---
name: peek-server
description: Run and administer a self-hosted peek server — the HTML sharing service behind the peek CLI. Use when the user wants to install, start, deploy, or self-host a peek server, create or revoke access tokens, set or clear page passwords, or configure peekd. Triggers on "run a peek server", "self-host peek", "deploy peek", "create a peek token", "peek admin", "set up peekd". For just uploading HTML and reading comments as a client, use the peek skill instead.
---

# peek-server — Run and administer a peek server

peek is a self-hosted HTML sharing server (`peekd`) with a companion CLI
(`peek`). This skill covers **running and administering** the server: starting
`peekd`, issuing access tokens, managing page passwords, configuring runtime
limits/storage, and deploying behind a reverse proxy.

> Just uploading HTML and reading comments as a client? Use the **peek** skill.

## When to use this skill

- The user wants to **install, run, deploy, or self-host** a peek server.
- The user needs to **create, list, or revoke access tokens** for users/agents.
- The user wants to **set or clear a page password** as an admin/owner.
- The user mentions **peekd**, peek server setup, or peek deployment.

## Install the server binary

The server binary is `peekd`. Get it from a release or build from source.

```sh
# Download a release archive (contains both peekd and peek), then:
curl -sSL https://github.com/puemos/peek/releases/latest/download/peek_<version>_linux_amd64.tar.gz | tar xz
sudo mv peek_*/peekd peek_*/peek /usr/local/bin/
```

Or build from source (needs Go):

```sh
git clone https://github.com/puemos/peek && cd peek
go build -o bin/peekd ./cmd/peekd
go build -o bin/peek  ./cmd/peek
```

## Run the server

```sh
peekd --addr :7700 --data ./data --base-url https://peek.example.com
```

On first run, `peekd` prints a one-time setup URL. Open it to create the first
local admin account. After setup, use the dashboard or `peek login --host
https://peek.example.com`.

### Docker

```sh
docker run -d --name peek -p 7700:7700 \
  -v peek-data:/data \
  -e PEEK_BASE_URL=https://peek.example.com \
  ghcr.io/puemos/peek:latest
```

The image runs as uid `65532` and stores the SQLite DB, uploads, and generated
secret under `/data`. If bind-mounting a host directory, make it writable by
uid `65532`.

### Configuration

| Flag | Env | Default | Description |
|---|---|---|---|
| `--addr` | `PEEK_ADDR` | `:7700` | Listen address |
| `--data` | `PEEK_DATA` | `./data` | Data dir (db + uploads + secret) |
| `--base-url` | `PEEK_BASE_URL` | `http://localhost:7700` | Public base URL in share links. **Use `https://…` in production** — it enables `Secure` cookies + HSTS. |
| `--max-upload` | `PEEK_MAX_UPLOAD` | `2097152` (2 MiB) | Max upload size in bytes |
| `--secret` | `PEEK_SECRET` | *(stored in data dir)* | HMAC/encryption secret. Set explicitly when multiple instances must share signed cookies/settings. |
| `--max-total-size` | `PEEK_MAX_TOTAL_SIZE` | `0` | Total upload storage limit in bytes (`0` = unlimited) |
| `--retention-days` | `PEEK_RETENTION_DAYS` | `0` | Auto-delete uploads older than N days (`0` = off) |
| `--trusted-proxy` | `PEEK_TRUSTED_PROXY` | `false` | Trust `X-Forwarded-For` for analytics/audit IPs. Enable only behind your reverse proxy. |
| `--storage` | `PEEK_STORAGE` | `file` | Storage backend: `file` or `s3` |
| `--s3-endpoint` | `PEEK_S3_ENDPOINT` | *(empty)* | S3-compatible endpoint URL |
| `--s3-bucket` | `PEEK_S3_BUCKET` | *(empty)* | S3 bucket name |
| `--s3-region` | `PEEK_S3_REGION` | `us-east-1` | S3 region |
| `--s3-access-key` | `PEEK_S3_ACCESS_KEY` | *(empty)* | S3 access key |
| `--s3-secret-key` | `PEEK_S3_SECRET_KEY` | *(empty)* | S3 secret key, encrypted in settings after initialization |

`PEEK_LOG_LEVEL` controls structured JSON log verbosity (`debug`, `info`,
`warn`). `peekd --version` prints the server version.

Admins can update these runtime settings from the dashboard: max upload size,
total storage limit, max uploads per token, max storage per token, retention
days, and S3 credentials. Changing the storage backend itself requires a
restart.

## Administer with the CLI

Configure the CLI against the server with browser login (see the **peek** skill
for `peek login`), then:

### Manage access tokens (admin only)

```sh
peek token create --name "Alice"   # prints a new user token once
peek token list                    # list tokens (ids + names)
peek token revoke <id>             # revoke a token by id
```

Tokens are stored hashed and shown only once at creation. Non-admin tokens can
upload and manage only their own uploads.

Token expiry is available through the admin JSON API by sending
`{"name":"service","expires_hours":24}` to `POST /api/tokens`. The current CLI
creates non-expiring tokens.

### Manage OAuth and invitations

Admins can use the web dashboard Settings section to enable Google and/or GitHub
OAuth. Create web OAuth credentials with these callback URLs:

```text
https://peek.example.com/oauth/google/callback
https://peek.example.com/oauth/github/callback
```

For local GitHub testing, create a temporary OAuth App at
`https://github.com/settings/applications/new` with `Homepage URL` set to the
local base URL and `Authorization callback URL` set to
`http://127.0.0.1:<port>/oauth/github/callback`.

OAuth signups are invite-only. Create manual invite links in the dashboard and
send them to users. When OAuth is enabled, non-admin dashboard and CLI login
must use OAuth; the local password form remains available to admins. When OAuth
is not enabled, token login remains available and can be disabled from Settings.
Direct bearer tokens still work for API/automation. The same verified email maps
to the same Peek account across Google and GitHub. Admins can disable users or
promote/demote admins in the dashboard later.

### Manage page passwords

```sh
peek visibility <slug> password --password newpass   # protect a page
peek visibility <slug> public                        # remove protection
```

### Data export and deletion

```sh
peek export <slug>   # JSON export with upload metadata, visits, and comments
peek delete-all      # delete every upload owned by the configured token
```

### Health, metrics, and backups

```sh
curl -fsS http://127.0.0.1:7700/healthz
curl -fsS http://127.0.0.1:7700/readyz
curl -fsS http://127.0.0.1:7700/metrics

PEEK_DATA=./data peekd backup ./peek-backup.db
```

`/healthz` is liveness, `/readyz` checks database reachability, and `/metrics`
emits Prometheus text metrics. Protect `/metrics` at the reverse proxy if the
server is reachable from untrusted networks.

## Deployment

peek speaks plain HTTP and expects a **TLS-terminating reverse proxy** in front
(this is what enables `Secure` cookies + HSTS). Example with Caddy:

```
peek.example.com {
    reverse_proxy 127.0.0.1:7700
}
```

Run `peekd` with `--base-url https://peek.example.com` so links and cookie flags
are correct. If your proxy forwards `X-Forwarded-For` and you want those client
IPs used for analytics/audit logs, also set `--trusted-proxy`; otherwise Peek
intentionally ignores forwarded IP headers. Back up the `data/` directory — it
holds the DB, uploads, and the signing secret.

For S3-compatible storage, set `--storage s3` plus endpoint, bucket, region, and
credentials. S3 endpoints must be HTTP(S), HTTPS unless localhost, and cannot
resolve to private/link-local IPs.

## Web dashboard

The server also serves a browser dashboard at `/login`. Sign in with a token or
an enabled OAuth provider to upload (file or paste HTML), list/delete uploads,
set passwords, and view stats. Admins can also edit runtime limits, retention,
S3 settings, OAuth providers, invites, and users — no CLI required. Direct
non-technical users there.
