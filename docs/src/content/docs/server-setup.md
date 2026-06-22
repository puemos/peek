---
title: Server Setup
description: Install Peek, complete first-run setup, and run it behind a production URL.
kicker: peekd --addr :7700
---

## Install

Install release binaries when you want the normal deployment path:

```sh
curl -fsSL https://raw.githubusercontent.com/puemos/peek/main/install.sh | sh
```

The release ships two binaries:

| Binary | Role |
| --- | --- |
| `peekd` | Server process. Hosts the dashboard, shared pages, API, metrics, and health endpoints. |
| `peek` | CLI. Uploads HTML, manages links, reads comments, and creates automation tokens. |

Go installs are also supported:

```sh
go install github.com/puemos/peek/cmd/peekd@latest
go install github.com/puemos/peek/cmd/peek@latest
```

## First Run

Start the server with a persistent data directory:

```sh
peekd --addr :7700 --data ./data --base-url http://localhost:7700
```

On a new data directory, `peekd` prints a one-time setup URL. Open it and create the first admin account. After that account exists, setup closes.

The data directory stores the SQLite database, signing/encryption secret, and file-backed uploads when file storage is selected. Do not treat it as disposable.

## Production Shape

Run one primary `peekd` process per data directory behind a TLS-terminating reverse proxy:

```sh
peekd \
  --addr 127.0.0.1:7700 \
  --data /var/lib/peek \
  --base-url https://peek.example.com \
  --trusted-proxy
```

Use `https://` for production `--base-url`. Peek uses the base URL for generated links, OAuth callbacks, secure cookies, CLI login approval URLs, and redirects.

Horizontal scaling is not the default model. Peek stores mutable state in SQLite plus uploaded objects, so replication should be an explicit architecture project rather than a process-manager setting.

## Docker

Run the published image with a persistent `/data` volume:

```sh
docker run -d --name peek --restart unless-stopped \
  -p 7700:7700 \
  -v peek-data:/data \
  -e PEEK_BASE_URL=https://peek.example.com \
  ghcr.io/puemos/peek:latest
```

The image is scratch-based, runs as uid `65532`, exposes port `7700`, and stores server state under `/data`.

## Proxy And Base URL

Caddy example:

```text
peek.example.com {
    reverse_proxy 127.0.0.1:7700
}
```

Set `--trusted-proxy` only when the proxy is controlled by you and overwrites or controls `X-Forwarded-For`. When it is not set, Peek ignores forwarded IP headers for analytics and audit fields.

Keep `/metrics` protected at the proxy or network layer.

## Smoke Test

After setup or upgrade:

```sh
peekd healthcheck --addr https://peek.example.com
peek login --host https://peek.example.com
printf '<!doctype html><h1>Peek smoke</h1>' > /tmp/peek-smoke.html
peek upload /tmp/peek-smoke.html --visibility public
peek list
```

Open the uploaded page, add a comment, and confirm the dashboard shows the upload and stats.
