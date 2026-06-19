---
name: peek
description: Share HTML files and get a live preview URL using a self-hosted peek server. Use when the user wants to share, preview, or get a link to an HTML page, upload HTML to a self-hosted server, or when an agent needs to publish generated HTML for review. Triggers on "share html", "preview html", "upload html", "publish page", "get a link to this page", "peek".
---

# peek — Share HTML via a self-hosted server

peek is a self-hosted HTML sharing server. You upload an HTML file and get a
unique, shareable URL. Pages can be password-protected and support
element-anchored comments. It ships with a CLI (`peek`) and a web GUI.

## When to use this skill

Use this skill when:

- The user asks to **share**, **preview**, or **get a link to** an HTML file or snippet.
- You (the agent) generate HTML and want to give the user a live link to view it.
- The user mentions **peek**, `peek`, or a self-hosted HTML sharing server.

Do NOT use this skill for deploying production apps, hosting static sites
long-term, or serving non-HTML content.

## Prerequisites

The user must have a running peek server and the `peek` CLI installed. If
they don't, point them to the setup steps in the "Server setup" section below.

The CLI needs to be configured once with the server host and an access token:

```sh
peek config set --host <server-url> --token <token>
```

Configuration is saved to `~/.config/hn/config.json`. The host and token can
also be set via `PEEK_HOST` and `PEEK_TOKEN` environment variables.

## Core operations

### Upload an HTML file and get a share link

```sh
peek upload path/to/page.html
```

Output includes the shareable URL and the slug:
```
uploaded: http://localhost:7700/p/PhiUs-lMbZE_Sw
slug:     PhiUs-lMbZE_Sw
```

### Upload with password protection

```sh
peek upload page.html --password s3cret
```

### Upload raw HTML from stdin (no file needed)

If you generated HTML in memory and want to share it without saving to disk:

```sh
echo '<!doctype html><html><body><h1>Hello</h1></body></html>' | \
  curl -s -X POST -H "Authorization: Bearer $PEEK_TOKEN" \
  -H "Content-Type: text/html" \
  --data-binary @- "$PEEK_HOST/api/upload" | jq -r .url
```

Or write to a temp file first, then `peek upload`:

```sh
cat > /tmp/preview.html << 'EOF'
<!doctype html>
<html><body><h1>Preview</h1></body></html>
EOF
peek upload /tmp/preview.html
```

### List all uploads

```sh
peek list
```

### View visit analytics

```sh
peek stats <slug>
```

Shows total visits, unique visitors, and a recent-visit log (with visitor name
if they identified themselves, plus hashed IP and user agent).

### Delete an upload

```sh
peek delete <slug>
```

### Add or remove password protection

```sh
peek password <slug> --set newpass    # protect
peek password <slug> --clear          # remove protection
```

### Create a new user token (admin only)

```sh
peek token create --name "Alice"
peek token list
```

## Web GUI

The server also has a browser-based dashboard at `/dashboard`. Users log in
with their token and can upload (file or paste HTML), list, delete, and view
stats without the CLI. Direct non-technical users there if they prefer a GUI.

## Server setup (if the user doesn't have one yet)

```sh
# Clone and build
git clone https://github.com/puemos/peek.git
cd peek
mise install
go build -o bin/peekd ./cmd/peekd
go build -o bin/peek ./cmd/peek

# Start the server (first run prints the admin token)
PEEK_ADMIN_TOKEN=<choose-a-secret> ./bin/peekd \
  --addr :7700 --data ./data --base-url http://localhost:7700

# Configure the CLI
./bin/peek config set --host http://localhost:7700 --token <admin-token>
```

The admin token is stored in the SQLite DB on first run. After that, the
`--admin-token` flag is ignored. Admins can create additional tokens via
`peek token create`.

## Agent workflow example

When you generate HTML and want to share it:

1. Write the HTML to a temp file.
2. Run `peek upload /tmp/preview.html`.
3. Extract the URL from the output (the line starting with `uploaded:`).
4. Give the URL to the user.

```sh
peek upload /tmp/preview.html | grep '^uploaded:' | awk '{print $2}'
```

If `peek` is not configured, fall back to curl with env vars:

```sh
curl -s -X POST \
  -H "Authorization: Bearer ${PEEK_TOKEN}" \
  -H "Content-Type: text/html" \
  --data-binary @/tmp/preview.html \
  "${PEEK_HOST}/api/upload?filename=preview.html"
```

The JSON response has the shape `{"slug":"...","url":"http://..."}`.

## Security notes for agents

- Never put the token in a URL or commit it to a repo. Use the config file or env var.
- Uploaded HTML is sandboxed in an iframe — it cannot harm the server.
- Only token holders can upload. There is no anonymous upload.
