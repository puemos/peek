---
name: peek
description: Share an HTML file to a self-hosted peek server and get a live preview URL, then read the comments reviewers leave on it. Use when the user wants to share, preview, or get a link to an HTML page, upload HTML for review, or read feedback/comments on a shared page. Triggers on "share html", "preview html", "upload html", "publish page", "get a link to this page", "read comments", "peek". For running or administering a peek server, use the peek-server skill instead.
---

# peek — Share HTML and collect feedback

peek is a self-hosted HTML sharing server. You upload an HTML file and get a
unique, shareable URL. Reviewers open the link and leave comments pinned to a
specific element, anchored to selected text, or on the whole page. This skill
covers the **consumer** side: installing the `peek` CLI, uploading pages, and
reading the comments back.

> Running or administering the server (tokens, passwords, deployment) is covered
> by the **peek-server** skill.

## When to use this skill

- The user asks to **share**, **preview**, or **get a link to** an HTML file or snippet.
- You (the agent) generate HTML and want to give the user a live link to view it.
- The user wants to **read the comments / feedback** left on a page they shared.
- The user mentions **peek** or a self-hosted HTML sharing server.

Do NOT use this skill for deploying production apps, hosting static sites
long-term, or serving non-HTML content.

## Install the CLI

```sh
curl -fsSL https://raw.githubusercontent.com/puemos/peek/main/install.sh | sh
```

This downloads a prebuilt `peek` binary for your OS/arch and installs it
(`/usr/local/bin`, falling back to `~/.local/bin`). Overrides:

- `PEEK_VERSION=v0.1.0` — install a specific release instead of the latest.
- `PEEK_INSTALL_DIR=~/bin` — choose the install directory.

Verify with `peek version`.

## Configure

Point the CLI at a peek server and authenticate. If the server supports browser
login, this opens the browser approval flow. Otherwise, paste an access token at
the hidden prompt:

```sh
peek login --host https://peek.example.com
```

Browser login saves a normal Peek API token locally after approval. For
token-only servers, use `peek login --token-stdin` or `peek login --token-file
<path>`. For automation, set `PEEK_TOKEN` per command; OAuth-enabled servers
require the browser flow for human CLI login. The token never lands in your
shell history unless you explicitly use `--token`. Config is saved to
`<user-config-dir>/peek/config.json` (`~/.config/peek` on Linux,
`~/Library/Application Support/peek` on macOS). You can also set `PEEK_HOST` and
`PEEK_TOKEN` per command (handy for CI/agents).

If the user doesn't have a server, token, or invite yet, point them to the
**peek-server** skill.

## Core operations

### Upload an HTML file and get a share link

```sh
peek upload path/to/page.html
```

Output includes the shareable URL and the slug:

```
uploaded: https://peek.example.com/p/PhiUs-lMbZE_Sw
slug:     PhiUs-lMbZE_Sw
```

### Upload with password protection

```sh
peek upload page.html --password s3cret
```

### Upload raw HTML without a file

Write to a temp file, then upload:

```sh
cat > /tmp/preview.html << 'EOF'
<!doctype html>
<html><body><h1>Preview</h1></body></html>
EOF
peek upload /tmp/preview.html
```

Or POST it straight from memory:

```sh
echo '<!doctype html><html><body><h1>Hello</h1></body></html>' | \
  curl -s -X POST -H "Authorization: Bearer $PEEK_TOKEN" \
  -H "Content-Type: text/html" \
  --data-binary @- "$PEEK_HOST/api/upload?filename=preview.html" | jq -r .url
```

### Read the comments on a page

```sh
peek comments <slug>
```

Prints every comment with its author, the on-page element/text it's anchored to,
and the body. You can only read comments for **your own** uploads.

### List your uploads

```sh
peek list
```

### View visit analytics

```sh
peek stats <slug>
```

Total visits, unique visitors, and a recent-visit log.

### Delete an upload

```sh
peek delete <slug>
```

### Export or delete all owned upload data

```sh
peek export <slug>   # JSON export with upload metadata, visits, and comments
peek delete-all      # delete every upload owned by the current token
```

## Reading feedback in the browser

Open the share URL (`/p/<slug>`) and click the comments panel. Reviewers select text, click an element, or leave a page-level note; each anchored comment gets a numbered on-page pin. The panel has an **Export** button with **Markdown** and **JSON** options; Markdown copies the whole thread with the element/quote it's anchored to, ready to paste into an issue or hand to an agent, while JSON copies the visible comments array for structured reuse.

## Agent workflow

When you generate HTML and want to share it:

1. Write the HTML to a temp file.
2. Run `peek upload /tmp/preview.html`.
3. Extract the URL from the output (the `uploaded:` line) and give it to the user.
4. Later, run `peek comments <slug>` to read the feedback they collected.

```sh
peek upload /tmp/preview.html | grep '^uploaded:' | awk '{print $2}'
```

If `peek` isn't configured, fall back to curl with env vars:

```sh
curl -s -X POST \
  -H "Authorization: Bearer ${PEEK_TOKEN}" \
  -H "Content-Type: text/html" \
  --data-binary @/tmp/preview.html \
  "${PEEK_HOST}/api/upload?filename=preview.html"
```

The JSON response has the shape `{"slug":"...","url":"https://..."}`.

## Security notes for agents

- Never put the token in a URL or commit it to a repo. Use `peek login`, the
  config file, or the `PEEK_TOKEN` env var.
- Uploaded HTML is sandboxed in an opaque-origin iframe — it cannot touch the
  server or other visitors.
- Only token holders can upload, and `peek comments` only returns comments for
  uploads you own.
- Use `peek export <slug>` when the user asks for a portable data copy of one
  upload, and `peek delete-all` only when they explicitly want to remove all
  uploads owned by the configured token.
