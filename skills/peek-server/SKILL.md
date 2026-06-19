---
name: peek-server
description: Run and administer a self-hosted peek server — the HTML sharing service behind the peek CLI. Use when the user wants to install, start, deploy, or self-host a peek server, create or revoke access tokens, set or clear page passwords, or configure peekd. Triggers on "run a peek server", "self-host peek", "deploy peek", "create a peek token", "peek admin", "set up peekd". For just uploading HTML and reading comments as a client, use the peek skill instead.
---

# peek-server — Run and administer a peek server

peek is a self-hosted HTML sharing server (`peekd`) with a companion CLI
(`peek`). This skill covers **running and administering** the server: starting
`peekd`, issuing access tokens, managing page passwords, and deploying behind a
reverse proxy.

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
# First run prints (and stores) an admin token. Set one explicitly with PEEK_ADMIN_TOKEN.
PEEK_ADMIN_TOKEN=<choose-a-secret> peekd \
  --addr :7700 --data ./data --base-url https://peek.example.com
```

The admin token is stored hashed in the SQLite DB on first run; after that the
`--admin-token` flag is ignored.

### Configuration

| Flag | Env | Default | Description |
|---|---|---|---|
| `--addr` | `PEEK_ADDR` | `:7700` | Listen address |
| `--data` | `PEEK_DATA` | `./data` | Data dir (db + uploads + secret) |
| `--base-url` | `PEEK_BASE_URL` | `http://localhost:7700` | Public base URL in share links. **Use `https://…` in production** — it enables `Secure` cookies + HSTS. |
| `--admin-token` | `PEEK_ADMIN_TOKEN` | *(random)* | Initial admin token (first run only) |
| `--max-upload` | `PEEK_MAX_UPLOAD` | `2097152` (2 MiB) | Max upload size in bytes |

## Administer with the CLI

Configure the CLI against the server with the admin token (see the **peek** skill
for `peek login`), then:

### Manage access tokens (admin only)

```sh
peek token create --name "Alice"   # prints a new user token once
peek token list                    # list tokens (ids + names)
peek token revoke <id>             # revoke a token by id
```

Tokens are stored hashed and shown only once at creation. Non-admin tokens can
upload and manage only their own uploads.

### Manage page passwords

```sh
peek password <slug> --set newpass   # protect a page
peek password <slug> --clear         # remove protection
```

## Deployment

peek speaks plain HTTP and expects a **TLS-terminating reverse proxy** in front
(this is what enables `Secure` cookies + HSTS). Example with Caddy:

```
peek.example.com {
    reverse_proxy 127.0.0.1:7700
}
```

Run `peekd` with `--base-url https://peek.example.com` so links and cookie flags
are correct, and make sure the proxy forwards `X-Forwarded-For` (used for
analytics). Back up the `data/` directory — it holds the DB, uploads, and the
signing secret.

## Web dashboard

The server also serves a browser dashboard at `/login`. Sign in with a token to
upload (file or paste HTML), list/delete uploads, set passwords, and view stats —
no CLI required. Direct non-technical users there.
