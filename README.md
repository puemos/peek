# Peek

[![CI](https://github.com/puemos/peek/actions/workflows/ci.yml/badge.svg)](https://github.com/puemos/peek/actions/workflows/ci.yml) [![Release](https://img.shields.io/github/v/release/puemos/peek?sort=semver)](https://github.com/puemos/peek/releases) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE) [![Go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)

**Share an HTML page inside your company, get a link, collect feedback right on the page.**

Peek is a self-hosted internal review tool for teams that need to share HTML previews without turning them into public deployments. It turns any HTML file into a live preview URL and lets reviewers drop comments **pinned to a specific element, anchored to selected text, or on the whole page** — like Figma/Linear review, but for any HTML. Single static Go binary, pure-Go SQLite, no CGO.

![Peek viewer with pinned comments](assets/hero.png)

<p align="center"><em>Pinned & text-anchored comments on a shared page.</em></p>

## Demo

![Peek demo](assets/demo.gif)

## Why Peek

- 🧷 **Comment anywhere on the page** — pin to an **element**, highlight **selected text** (Medium-style), or leave a **page-level** note. Each anchored comment gets a numbered on-page pin that tracks the element as you scroll.
- 🎯 **Click a comment → jump to it** — the panel and the on-page pins are linked both ways; selected text stays highlighted via the CSS Custom Highlight API (no DOM mutation of the user's HTML).
- 🔒 **Safe by construction** — uploaded HTML renders inside a sandboxed, opaque-origin iframe. It can never read your cookies, hit the server, or touch the parent page.
- ⚡ **One static binary** — Go stdlib + pure-Go SQLite, no CGO, no runtime deps.
- 🧰 **CLI + web dashboard + agent skill** — upload from the terminal, a browser, or let a coding agent share HTML for your team.
- 📊 **Privacy-respecting analytics** — total/unique visits, recent views with SHA-256-hashed IPs.
- 🏭 **Built for internal company deployments** — first-run admin setup, invite-only OAuth, health/readiness checks, graceful shutdown, structured JSON logging, Prometheus metrics, audit log, token expiry, quota/retention controls, data export/deletion, and S3 endpoint SSRF protection.

Peek is not designed as a SaaS product. It intentionally avoids billing, public multi-tenant onboarding, marketplace workflows, and growth-oriented product surfaces. The operating model is one company, team, lab, or agency running its own Peek server for trusted reviewers and internal automation.

## Install

### Install the CLI (one-liner)

```sh
curl -fsSL https://raw.githubusercontent.com/puemos/peek/main/install.sh | sh
```

Installs just the `peek` CLI for your OS/arch (override with `PEEK_VERSION` / `PEEK_INSTALL_DIR`). To run a server too, use one of the options below.

### Download a release (recommended)

Grab a prebuilt archive for your OS/arch from the [releases page](https://github.com/puemos/peek/releases). Each archive contains two binaries: `peekd` (server) and `peek` (CLI).

```sh
# example: Linux x86_64
curl -sSL https://github.com/puemos/peek/releases/latest/download/peek_<version>_linux_amd64.tar.gz | tar xz
sudo mv peek_*/peekd peek_*/peek /usr/local/bin/
```

### Go install

```sh
go install github.com/puemos/peek/cmd/peekd@latest   # server
go install github.com/puemos/peek/cmd/peek@latest    # CLI
```

### Build from source

```sh
git clone https://github.com/puemos/peek && cd peek
go build -o bin/peekd ./cmd/peekd
go build -o bin/peek  ./cmd/peek
```

### Run with Docker

A slim image (`scratch`-based, multi-arch `amd64`/`arm64`) is published to GitHub Container Registry on every release:

```sh
docker run -d --name peek -p 7700:7700 \
  -v peek-data:/data \
  -e PEEK_BASE_URL=https://peek.example.com \
  ghcr.io/puemos/peek:latest
```

The `/data` volume holds the SQLite database, uploads, and the signing key — keep it to persist state across restarts. On first run, Peek prints a one-time setup URL. Open it, create the first admin account, then sign in normally. Configure via env vars: `PEEK_ADDR` (default `:7700`), `PEEK_DATA` (default `/data`), `PEEK_BASE_URL`, `PEEK_MAX_UPLOAD`, `PEEK_SECRET` (for shared-secret deployments and encrypted settings), `PEEK_LOG_LEVEL` (`debug`/`info`/`warn`), `PEEK_MAX_TOTAL_SIZE`, `PEEK_RETENTION_DAYS`, `PEEK_TRUSTED_PROXY`, and the S3 vars listed below. Set `PEEK_S3_ALLOW_PRIVATE_ENDPOINT=true` only for explicit dev/private S3-compatible endpoints such as the local MinIO compose profile. The container runs as a nonroot user (uid `65532`) and includes a `HEALTHCHECK`. If you bind-mount a host directory instead of a named volume, make sure that path is writable by uid `65532`.

For a local Docker Compose stack:

```sh
docker compose up -d

# Optional S3-compatible storage test stack with MinIO:
docker compose --profile s3 up -d
```

## Quick start

```sh
# 1. Start the server (first run prints a setup URL)
peekd --addr :7700 --data ./data --base-url http://localhost:7700

# 2. Open the setup URL from the server logs and create the first admin.

# 3. Log in. If browser login is available, this opens the approval flow.
peek login --host http://localhost:7700

# 4. Share a page
peek upload mypage.html
#  -> uploaded: http://localhost:7700/p/PhiUs-lMbZE_Sw

# Password-protect it
peek upload mypage.html --password s3cret

# Manage
peek list
peek stats PhiUs-lMbZE_Sw
peek password PhiUs-lMbZE_Sw --set newpass   # or --clear
peek delete PhiUs-lMbZE_Sw
peek export PhiUs-lMbZE_Sw                    # export upload data
peek delete-all                               # delete all your uploads
```

## Commenting

Open `/p/<slug>` and use the floating island at the bottom:

1. **Select text** in the page → a _Comment_ bubble appears → comment on that exact quote. The text stays highlighted and gets a numbered pin.
2. **Click _Comment_** → click any **element** to pin a comment to it.
3. **…or comment on the page** → a general note not tied to anything.

Comments live in a side panel; clicking one scrolls to and flashes its anchor. Your name is asked once (a minimal prompt) and remembered. For password-protected pages, commenting is gated behind the same session.

## Screenshots

| Dashboard                          | Comments panel                         |
| ---------------------------------- | -------------------------------------- |
| ![Dashboard](assets/dashboard.png) | ![Comments panel](assets/comments.png) |

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

**Login input, most secure first:** `peek login` (browser login when available; otherwise hidden token prompt) · `PEEK_TOKEN=…` env · `peek login --token-stdin` (pipe) · `peek login --token-file <path>`. If OAuth is enabled on the server, CLI login must use the browser flow; direct `PEEK_TOKEN` remains available for automation. The `--token <value>` flag still works only where token login is allowed and warns, since it leaks into shell history and `ps`.

## Configuration

### Server (`peekd`)

| Flag               | Env                   | Default                 | Description                                                                                             |
| ------------------ | --------------------- | ----------------------- | ------------------------------------------------------------------------------------------------------- |
| `--addr`           | `PEEK_ADDR`           | `:7700`                 | Listen address                                                                                          |
| `--data`           | `PEEK_DATA`           | `./data`                | Data dir (db + uploads + secret)                                                                        |
| `--base-url`       | `PEEK_BASE_URL`       | `http://localhost:7700` | Public base URL in share links. **Use `https://…` in production** — it enables `Secure` cookies + HSTS. |
| `--max-upload`     | `PEEK_MAX_UPLOAD`     | `2097152` (2 MiB)       | Max upload size in bytes                                                                                |
| `--secret`         | `PEEK_SECRET`         | _(stored in data dir)_  | HMAC/encryption secret. Set explicitly when multiple instances must share signed cookies/settings.      |
| `--max-total-size` | `PEEK_MAX_TOTAL_SIZE` | `0`                     | Total upload storage limit in bytes (`0` = unlimited)                                                   |
| `--retention-days` | `PEEK_RETENTION_DAYS` | `0`                     | Auto-delete uploads older than N days (`0` = off)                                                       |
| `--trusted-proxy`  | `PEEK_TRUSTED_PROXY`  | `false`                 | Trust `X-Forwarded-For` for analytics/audit IPs. Enable only behind your reverse proxy.                 |
| `--storage`        | `PEEK_STORAGE`        | `file`                  | Storage backend: `file` or `s3`                                                                         |
| `--s3-endpoint`    | `PEEK_S3_ENDPOINT`    | _(empty)_               | S3-compatible endpoint URL                                                                              |
| `--s3-bucket`      | `PEEK_S3_BUCKET`      | _(empty)_               | S3 bucket name                                                                                          |
| `--s3-region`      | `PEEK_S3_REGION`      | `us-east-1`             | S3 region                                                                                               |
| `--s3-access-key`  | `PEEK_S3_ACCESS_KEY`  | _(empty)_               | S3 access key                                                                                           |
| `--s3-secret-key`  | `PEEK_S3_SECRET_KEY`  | _(empty)_               | S3 secret key, encrypted in settings after initialization                                               |
| `--s3-allow-private-endpoint` | `PEEK_S3_ALLOW_PRIVATE_ENDPOINT` | `false` | Allow private/link-local S3 endpoints for explicit dev deployments such as local MinIO.                  |

`peekd --version` prints the server version. `PEEK_DATA=./data peekd backup [path/to/backup.db]` writes a consistent SQLite snapshot using `VACUUM INTO`. `peekd healthcheck` checks `/healthz`; set `PEEK_HEALTHCHECK_ADDR=host:port` when the probe should target a specific address.

Admins can also update runtime settings from the dashboard: max upload size, total storage limit, max uploads per token, max storage per token, retention days, and S3 credentials. Changing the storage backend itself requires a restart.

### CLI (`peek`)

Saved to `<user-config-dir>/peek/config.json` (e.g. `~/.config/peek` on Linux, `~/Library/Application Support/peek` on macOS). Overridable per-command with `PEEK_HOST` / `PEEK_TOKEN`.

## Security model

| Threat                                    | Mitigation                                                                                                                                                                   |
| ----------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Anyone can upload                         | Every upload requires a valid API token or authenticated dashboard session.                                                                                                  |
| Token theft from the database/backups     | Tokens are stored only as **SHA-256 hashes**; the plaintext is shown once at creation. `peek token revoke <id>` invalidates one.                                             |
| Long-lived service tokens                 | API-created tokens can include an expiry; expired tokens are rejected during auth.                                                                                           |
| Token leaking via the terminal            | `peek login` uses browser login when available; token fallback reads from a **hidden prompt / stdin / file**, never argv.                                                    |
| OAuth provider token exposure             | Provider access tokens are used only for profile/email lookup and are not stored. Google requires a verified OIDC email; GitHub requires a verified email from `user:email`. |
| CLI browser token delivery                | Browser approval creates a normal Peek API token exactly once through a short-lived device flow; the API token is never put in a URL and is stored only as a hash.           |
| Session-cookie theft                      | The dashboard cookie is a **signed, revocable reference** to the account id — not the token itself. `HttpOnly`, `SameSite=Strict`, and `Secure` (auto on https).             |
| Dashboard CSRF / clickjacking             | Dashboard forms require CSRF tokens and dashboard pages send a CSP with `frame-ancestors 'none'`.                                                                            |
| Credentials sent in clear                 | `Secure` cookies + **HSTS** when the base URL is https; the server **warns** on startup if a non-local base URL is plain http. Run behind a TLS reverse proxy.               |
| Uploaded HTML harms the host              | The server never executes HTML — it only streams bytes.                                                                                                                      |
| Uploaded HTML steals cookies/data         | Rendered in `<iframe sandbox="allow-scripts">` with **no `allow-same-origin`** (opaque origin): no access to server cookies, storage, or same-origin requests.               |
| Uploaded HTML attacks the parent page     | Parent ↔ iframe communicate only via `postMessage`; the iframe can only send "pick"/"pin" events.                                                                            |
| Hot-linking / bypassing the password gate | `/raw` requires a short-lived HMAC-signed view token issued only by `/p/<slug>` and bound to the visitor cookie.                                                             |
| Brute force / spam                        | Per-IP rate limits on `/login`, CLI login, the password gate, and comment posting.                                                                                           |
| Malicious content / huge uploads          | HTML sniffed, binaries rejected, configurable max size, `MaxBytesReader`.                                                                                                    |
| SSRF via S3 settings                      | S3 endpoints must be HTTP(S), HTTPS unless private endpoints are explicitly allowed, and cannot resolve to private/link-local IPs by default.                                |
| Path traversal / SQLi                     | Random base64url slugs (filenames never user-derived); all queries parameterized.                                                                                            |
| Password / IP leakage                     | Account and page passwords are bcrypt-hashed; analytics IPs SHA-256-hashed with the server secret.                                                                           |

## Deployment

Peek speaks plain HTTP and expects a **TLS-terminating reverse proxy** in front of it (this is what enables `Secure` cookies + HSTS). Example with Caddy:

```
peek.example.com {
    reverse_proxy 127.0.0.1:7700
}
```

Run `peekd` with `--base-url https://peek.example.com` so links and cookie flags are correct. If your proxy forwards `X-Forwarded-For` and you want those client IPs used for analytics/audit logs, also set `--trusted-proxy`; otherwise Peek intentionally ignores forwarded IP headers. Back up the `data/` directory — it holds the DB, uploads, and the signing secret.

Operational endpoints:

| Endpoint   | Purpose                                             |
| ---------- | --------------------------------------------------- |
| `/healthz` | Liveness check                                      |
| `/readyz`  | Readiness check; verifies the database is reachable |
| `/metrics` | Prometheus text metrics                             |

Protect `/metrics` at your proxy if the server is reachable from untrusted networks.

## Architecture

The repository is intentionally split into narrow internal packages: `internal/server` owns HTTP routing and request handling, `internal/uploads` owns upload creation and validation, `internal/db` owns SQLite persistence, `internal/objectstore` owns file and S3 storage backends, `internal/web` owns server-side templates and view models, `internal/peekd` owns daemon runtime/flags/backup/healthcheck orchestration, and `internal/cli` owns the terminal client. See [docs/architecture.md](docs/architecture.md) for the package map and local quality gates.

## Web GUI

A browser dashboard at `/login` lets company users upload files or paste HTML, list/delete uploads, set passwords, and view stats. First run starts at a one-time `/setup` URL that creates the initial local admin account. Admins can then enable Google and/or GitHub OAuth from Settings by entering each provider's web client ID and secret. Configure provider callback URLs as:

```
https://peek.example.com/oauth/google/callback
https://peek.example.com/oauth/github/callback
```

For local GitHub testing, create a temporary OAuth App at `https://github.com/settings/applications/new` with:

```
Homepage URL: http://127.0.0.1:7700
Authorization callback URL: http://127.0.0.1:7700/oauth/github/callback
```

Use the actual local port from `--addr` / `--base-url`, then paste the Client ID and generated client secret into dashboard Settings. Regenerate or delete test client secrets after sharing them or committing test notes.

OAuth signups are invite-only. Admins create manual invite links in the dashboard, send them to users, and users accept with an enabled OAuth provider. When OAuth is enabled, non-admin dashboard and CLI login must use OAuth; the local password form remains available to admins. When OAuth is not enabled, token login remains available and can be disabled from Settings. Direct bearer tokens still work for API/automation. The same verified email maps to the same Peek account across Google and GitHub. Admins can disable users or promote/demote admins later, and can edit runtime limits, retention, auth, and S3 settings. Sessions are signed, revocable, `HttpOnly`, `SameSite=Strict` cookies with CSRF protection on every form. Non-admins only see their own uploads.

## Agent skills

Coding agents (Claude Code, opencode, …) can install skills so they can share HTML and return a link automatically:

```sh
npx skills add puemos/peek@peek -g -y
```

Two skills ship in this repo:

- **`peek`** (`skills/peek/SKILL.md`) — consumer side: install the CLI, upload HTML, get a link, read comments.
- **`peek-server`** (`skills/peek-server/SKILL.md`) — run and administer a peek server: tokens, passwords, deployment.

## Project layout

```
cmd/peekd/          server entrypoint
cmd/peek/           CLI entrypoint
internal/db/        SQLite store + schema
internal/models/    data types
internal/server/    HTTP server, handlers, security, embedded assets
internal/cli/       CLI client + commands
skills/peek/        consumer agent skill (upload + read comments)
skills/peek-server/ server/admin agent skill
assets/             README / launch media
scripts/            tooling
```

The application CSS/JS used at runtime lives in `internal/web/assets` and is embedded directly by Go. The root `assets/` directory is documentation media only. The screenshots and 1920x1080/60fps MP4 demo video are generated — run `pnpm install`, then rerun `pnpm gen-assets` (needs Go, Node, ffmpeg, and Chrome) to refresh documentation media whenever the UI changes.

## License

[MIT](LICENSE)
