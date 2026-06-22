---
title: Reference
description: Server flags, environment variables, runtime settings, CLI configuration, and operational endpoints.
kicker: peekd reference
---

## Server Parameters

| Flag | Env | Default | Notes |
| --- | --- | --- | --- |
| `--addr` | `PEEK_ADDR` | `:7700` | Listen address. |
| `--data` | `PEEK_DATA` | `./data` | Data directory for SQLite, generated secret, and file uploads. |
| `--base-url` | `PEEK_BASE_URL` | `http://localhost:7700` | Public absolute URL. Use HTTPS in production. |
| `--secret` | `PEEK_SECRET` | generated | HMAC/encryption secret. Provide one when multiple processes must share cookie/settings secrets. |
| `--max-upload` | `PEEK_MAX_UPLOAD` | `2097152` | Maximum upload size in bytes. Must be greater than zero. |
| `--max-total-size` | `PEEK_MAX_TOTAL_SIZE` | `0` | Server-wide storage cap in bytes. `0` means unlimited. |
| `--retention-days` | `PEEK_RETENTION_DAYS` | `0` | Delete uploads older than N days. `0` disables. |
| `--trusted-proxy` | `PEEK_TRUSTED_PROXY` | `false` | Trust `X-Forwarded-For` for analytics and audit IP fields. |
| `--storage` | `PEEK_STORAGE` | `file` | Storage backend: `file` or `s3`. |
| `--s3-endpoint` | `PEEK_S3_ENDPOINT` | empty | S3-compatible endpoint URL. |
| `--s3-bucket` | `PEEK_S3_BUCKET` | empty | S3 bucket name. |
| `--s3-region` | `PEEK_S3_REGION` | `us-east-1` | S3 signing region. |
| `--s3-access-key` | `PEEK_S3_ACCESS_KEY` | empty | S3 access key ID. |
| `--s3-secret-key` | `PEEK_S3_SECRET_KEY` | empty | S3 secret access key. |
| `--s3-allow-private-endpoint` | `PEEK_S3_ALLOW_PRIVATE_ENDPOINT` | `false` | Allows HTTP/private/link-local endpoints for controlled private deployments. |

`PEEK_LOG_LEVEL` accepts `debug`, `info`, or `warn`. Any other value falls back to info.

## Runtime Settings

Admins can change these from the dashboard or the settings API:

| Key | Type | Notes |
| --- | --- | --- |
| `auth_token_login_enabled` | boolean | Allows web login with an access token when OAuth is not required. |
| `oauth_google_enabled` | boolean | Enables Google login if credentials are configured. |
| `oauth_google_client_id` | string | Google OAuth web client ID. |
| `oauth_google_client_secret` | secret string | Google OAuth web client secret. |
| `oauth_github_enabled` | boolean | Enables GitHub login if credentials are configured. |
| `oauth_github_client_id` | string | GitHub OAuth app client ID. |
| `oauth_github_client_secret` | secret string | GitHub OAuth app client secret. |
| `storage` | `file` or `s3` | Startup setting; restart to apply backend changes. |
| `s3_endpoint` | URL | Validated to block unsafe endpoints unless private endpoints are explicitly allowed. |
| `s3_bucket` | string | Bucket name for uploaded HTML bytes. |
| `s3_region` | string | S3 signing region. |
| `s3_access_key` | string | S3 access key ID. |
| `s3_secret_key` | secret string | S3 secret access key. |
| `max_upload` | bytes | Maximum individual upload size. |
| `max_total_size` | bytes | Server-wide storage cap; `0` means unlimited. |
| `max_uploads_per_token` | count | Per-owner upload count cap; `0` means unlimited. |
| `max_storage_per_token` | bytes | Per-owner storage cap; `0` means unlimited. |
| `retention_days` | days | Auto-delete threshold; `0` disables cleanup. |

## CLI Configuration

The CLI stores config under the user config directory in `peek/config.json`. Override per command with:

```sh
PEEK_HOST=https://peek.example.com
PEEK_TOKEN=...
```

Token input paths, from safest to least safe:

```text
peek login
peek login --token-stdin
peek login --token-file <path>
peek login --token <value>
```

## Operational Endpoints

| Endpoint | Purpose |
| --- | --- |
| `/healthz` | Liveness. |
| `/readyz` | Readiness, including database reachability. |
| `/metrics` | Prometheus text metrics. Protect it before exposing Peek outside a trusted network. |
| `/setup` | First-run setup only. Closes after the first account exists. |
| `/oauth/google/callback` | Google OAuth callback. |
| `/oauth/github/callback` | GitHub OAuth callback. |

## Operator Checks

Health:

```sh
peekd healthcheck --addr https://peek.example.com
curl -fsS https://peek.example.com/readyz
```

Backup:

```sh
PEEK_DATA=/var/lib/peek peekd backup /backups/peek-$(date +%F).db
```

Smoke upload:

```sh
peek login --host https://peek.example.com
printf '<!doctype html><h1>Peek smoke</h1>' > /tmp/peek-smoke.html
peek upload /tmp/peek-smoke.html --visibility public
peek list
```
