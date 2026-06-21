# Peek

[![CI](https://github.com/puemos/peek/actions/workflows/ci.yml/badge.svg)](https://github.com/puemos/peek/actions/workflows/ci.yml) [![Release](https://img.shields.io/github/v/release/puemos/peek?sort=semver)](https://github.com/puemos/peek/releases) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE) [![Go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)

Share an HTML page inside your company, get a link, collect feedback right on the page.

Peek is a self-hosted internal review tool. Upload any HTML file and get a live preview URL where reviewers can leave comments pinned to elements, anchored to selected text, or on the whole page. Single static Go binary, pure-Go SQLite, no CGO.

![Peek viewer with pinned comments](assets/hero.png)

## Quick start

```sh
# 1. Start the server (first run prints a setup URL)
peekd --addr :7700 --data ./data --base-url http://localhost:7700

# 2. Open the setup URL from the server logs and create the first admin.

# 3. Log in
peek login --host http://localhost:7700

# 4. Share a page
peek upload mypage.html
#  -> http://localhost:7700/p/PhiUs-lMbZE_Sw

# Password-protect it
peek upload mypage.html --password s3cret

# Manage
peek list
peek stats PhiUs-lMbZE_Sw
peek password PhiUs-lMbZE_Sw --set newpass   # or --clear
peek delete PhiUs-lMbZE_Sw
peek export PhiUs-lMbZE_Sw                    # export upload data
```

## Install

**Prebuilt binaries** (recommended): grab a `peekd` + `peek` archive for your OS/arch from the [releases page](https://github.com/puemos/peek/releases).

**CLI one-liner:**
```sh
curl -fsSL https://raw.githubusercontent.com/puemos/peek/main/install.sh | sh
```
Override with `PEEK_VERSION` / `PEEK_INSTALL_DIR`.

**Go install:**
```sh
go install github.com/puemos/peek/cmd/peekd@latest   # server
go install github.com/puemos/peek/cmd/peek@latest    # CLI
```

**Docker:**
```sh
docker run -d --name peek -p 7700:7700 \
  -v peek-data:/data \
  -e PEEK_BASE_URL=https://peek.example.com \
  ghcr.io/puemos/peek:latest
```
The `/data` volume holds the database, uploads, and signing key. On first run Peek prints a one-time setup URL. The image is `scratch`-based, multi-arch (`amd64`/`arm64`), runs as uid 65532.

```sh
docker compose up -d              # local stack
docker compose --profile s3 up -d # with MinIO storage
```

## Features

- **Pinned, text-anchored, and page-level comments** with numbered on-page markers that track elements during scroll. Click any comment to jump to its anchor. Text highlights use the CSS Custom Highlight API without mutating the DOM.
- **Sandboxed iframe rendering** (`sandbox="allow-scripts"`, no `allow-same-origin`): uploaded HTML cannot read cookies, hit the server, or touch the parent page.
- **One static binary** with no runtime dependencies. Go stdlib + pure-Go SQLite.
- **CLI, web dashboard, and agent skill**: upload from the terminal, a browser, or let a coding agent share HTML for your team.
- **Privacy-respecting analytics**: total/unique visits, recent views with SHA-256 hashed IPs.
- **Built for internal deployment**: first-run admin setup, invite-only OAuth (Google/GitHub), health/readiness checks, graceful shutdown, structured JSON logging, Prometheus metrics, audit log, token expiry, quota/retention controls, data export/deletion, S3 endpoint SSRF protection.

Peek is not a SaaS product. It avoids billing, public multi-tenant onboarding, marketplace workflows, and growth-oriented product surfaces.

## Commenting

Open `/p/<slug>` and use the floating island at the bottom:

1. Select text in the page, then click the Comment bubble to anchor a comment to that quote.
2. Click Comment, then click any element to pin a comment to it.
3. Or leave a page-level note not tied to anything.

Comments live in a side panel. Your name is asked once and remembered. Password-protected pages gate commenting behind the same session.

## CLI reference

```
peek login [--host <url>]              browser login when available; token fallback
peek login --token-stdin               read an access token from stdin
peek login --token-file <path>         read an access token from a file
peek config show                       show current host + masked token
peek upload <file.html> [--password <pw> | --password-stdin] [--name <filename>]
peek list
peek delete <slug>
peek delete-all                        delete all your uploads
peek password <slug> --set <pw> | --set-stdin | --clear
peek stats <slug>
peek comments <slug>                   list comments on one of your uploads
peek export <slug>                     export all data for an upload
peek token create --name <name>        create a user token (admin only)
peek token list                        list tokens (admin only)
peek token revoke <id>                 revoke a token by id (admin only)
peek version                           show version
```

Login methods, most secure first: `peek login` (browser) `PEEK_TOKEN=…` env `peek login --token-stdin` (pipe) `peek login --token-file <path>`. The `--token <value>` flag still works where token login is allowed but warns since it leaks into shell history.

## Configuration

### Server (`peekd`)

| Flag | Env | Default | Description |
| --- | --- | --- | --- |
| `--addr` | `PEEK_ADDR` | `:7700` | Listen address |
| `--data` | `PEEK_DATA` | `./data` | Data dir (db + uploads + secret) |
| `--base-url` | `PEEK_BASE_URL` | `http://localhost:7700` | Public base URL. Use `https://…` in production for Secure cookies + HSTS. |
| `--max-upload` | `PEEK_MAX_UPLOAD` | `2097152` (2 MiB) | Max upload size in bytes |
| `--secret` | `PEEK_SECRET` | _(stored in data dir)_ | HMAC/encryption secret for multi-instance cookie/settings sharing |
| `--max-total-size` | `PEEK_MAX_TOTAL_SIZE` | `0` | Total upload storage limit in bytes (`0` = unlimited) |
| `--retention-days` | `PEEK_RETENTION_DAYS` | `0` | Auto-delete uploads older than N days (`0` = off) |
| `--trusted-proxy` | `PEEK_TRUSTED_PROXY` | `false` | Trust `X-Forwarded-For` for analytics/audit IPs |
| `--storage` | `PEEK_STORAGE` | `file` | Storage backend: `file` or `s3` |
| `--s3-endpoint` | `PEEK_S3_ENDPOINT` | _(empty)_ | S3-compatible endpoint URL |
| `--s3-bucket` | `PEEK_S3_BUCKET` | _(empty)_ | S3 bucket name |
| `--s3-region` | `PEEK_S3_REGION` | `us-east-1` | S3 region |
| `--s3-access-key` | `PEEK_S3_ACCESS_KEY` | _(empty)_ | S3 access key |
| `--s3-secret-key` | `PEEK_S3_SECRET_KEY` | _(empty)_ | S3 secret key |
| `--s3-allow-private-endpoint` | `PEEK_S3_ALLOW_PRIVATE_ENDPOINT` | `false` | Allow private/link-local S3 endpoints for dev |

Admins can update runtime settings from the dashboard: max upload size, total storage limit, per-token quotas, retention days, and S3 credentials. Changing the storage backend requires a restart.

`peekd --version` prints the version. `peekd backup [path]` writes a consistent SQLite snapshot via `VACUUM INTO`. `peekd healthcheck` checks `/healthz`.

### CLI (`peek`)

Config saved to `<user-config-dir>/peek/config.json`. Override per-command with `PEEK_HOST` / `PEEK_TOKEN`.

## Security model

| Threat | Mitigation |
| --- | --- |
| Anyone can upload | Every upload requires a valid API token or authenticated dashboard session. |
| Token theft from database | Tokens stored as SHA-256 hashes; plaintext shown only at creation. |
| Long-lived tokens | API tokens can include an expiry; expired tokens are rejected. |
| Token leaking via terminal | `peek login` uses browser login; token fallback reads from hidden prompt/stdin/file. |
| OAuth token exposure | Provider tokens used only for profile lookup; not stored. |
| Session-cookie theft | Dashboard cookie is a signed, revocable reference; `HttpOnly`, `SameSite=Strict`, `Secure` on https. |
| CSRF / clickjacking | Dashboard forms require CSRF tokens; CSP with `frame-ancestors 'none'`. |
| Credentials in clear | Secure cookies + HSTS on https. Server warns on non-local plain http. |
| Uploaded HTML harms host | Server never executes HTML, only streams bytes. |
| Uploaded HTML steals data | Rendered in `<iframe sandbox="allow-scripts">` with opaque origin. |
| Cross-origin attacks | Parent and iframe communicate only via `postMessage`. |
| Bypassing password gate | `/raw` requires a short-lived HMAC-signed view token bound to visitor cookie. |
| Brute force / spam | Per-IP rate limits on login, password gate, and comment posting. |
| Malicious content | HTML sniffed, binaries rejected, configurable `MaxBytesReader`. |
| SSRF via S3 | Endpoints must be HTTP(S); private/link-local IPs blocked unless explicitly allowed. |
| Path traversal / SQLi | Random base64url slugs; all queries parameterized. |
| Password / IP leakage | Passwords bcrypt-hashed; analytics IPs SHA-256 hashed. |

## Deployment

Run `peekd` behind a TLS-terminating reverse proxy with `--base-url https://peek.example.com`. Example with Caddy:

```
peek.example.com {
    reverse_proxy 127.0.0.1:7700
}
```

Set `--trusted-proxy` if your proxy forwards `X-Forwarded-For`. Back up the `data/` directory.

Operational endpoints:

| Endpoint | Purpose |
| --- | --- |
| `/healthz` | Liveness check |
| `/readyz` | Readiness check |
| `/metrics` | Prometheus text metrics |

Protect `/metrics` at your proxy. Full deployment checklist: [docs/operations.md](docs/operations.md).

## Web dashboard

A browser dashboard at `/login` lets users upload, list, delete, password-protect, and view stats. First run starts at a one-time `/setup` URL that creates the initial admin. Admins can enable Google/GitHub OAuth from Settings. OAuth signups are invite-only: admins create invite links, users accept with an enabled OAuth provider. The same verified email maps to the same account across providers. Admins can disable users, promote/demote admins, and edit runtime limits, retention, auth, and S3 settings.

OAuth callback URLs:
```
https://peek.example.com/oauth/google/callback
https://peek.example.com/oauth/github/callback
```

## Agent skills

Coding agents (Claude Code, opencode, etc.) can install skills to share HTML and return a link:

```sh
npx skills add puemos/peek@peek -g -y
```

Two skills ship in this repo:

- **`peek`** (`skills/peek/SKILL.md`): consumer side -- install the CLI, upload HTML, get a link, read comments.
- **`peek-server`** (`skills/peek-server/SKILL.md`): run and administer a Peek server.

## Architecture

```
cmd/peekd/          server entrypoint
cmd/peek/           CLI entrypoint
internal/db/        SQLite store + schema
internal/models/    data types
internal/server/    HTTP server, handlers, auth, security
internal/cli/       CLI client + commands
internal/web/       Templates and embedded CSS/JS assets
skills/peek/        consumer agent skill
skills/peek-server/ server/admin agent skill
assets/             documentation media
scripts/            tooling
```

Runtime CSS/JS lives in `internal/web/assets` and is embedded by Go. The root `assets/` directory is documentation media only. See [docs/architecture.md](docs/architecture.md) for the package map.

## License

[MIT](LICENSE)
