---
title: User Manual
description: A compact task manual for Peek users, admins, and operators.
kicker: man peek
---

## Start Here

| I want to... | Start with |
| --- | --- |
| Share HTML for review | Upload and share |
| Leave feedback | Review and comment |
| Manage sign-in | Admin tasks |
| Run the service | Operator tasks |

## Upload And Share

Upload from the CLI:

```sh
peek upload report.html --visibility public
```

Or use the dashboard when you want to paste HTML, copy existing links, or inspect past uploads.

Choose `public` for link-accessible internal pages, `password` for link sharing with a shared secret, and `private` for pages that require a Peek account.

## Review And Comment

Open the shared URL. Use:

| Action | Best for |
| --- | --- |
| Select text, then comment | Exact wording, claims, or table values. |
| Click `Comment`, then click an element | Layout, charts, sections, cards, or generated UI. |
| Page comment | General feedback about the whole artifact. |

Open the comments panel to read the thread. Click anchored comments to jump back to their target.

## Owner Tasks

Useful CLI commands:

```text
peek list
peek stats <slug>
peek comments <slug>
peek export <slug>
peek visibility <slug> public|password|private
peek delete <slug>
```

Use `peek export <slug>` when you need to hand the review record to an issue, an agent, or an internal audit trail.

## Admin Tasks

Admins manage users, invites, OAuth, token login, limits, retention, and storage from the dashboard.

For company sign-in, enable Google or GitHub OAuth, configure the provider credentials, and invite users by email. OAuth signup is invite-only unless the verified email already belongs to an existing account.

Create automation tokens only for jobs that need API access:

```text
peek token create --name ci-reports
peek token list
peek token revoke <id>
```

## Operator Tasks

Run Peek behind HTTPS with a stable base URL:

```sh
peekd \
  --addr 127.0.0.1:7700 \
  --data /var/lib/peek \
  --base-url https://peek.example.com \
  --trusted-proxy
```

Back up SQLite with:

```sh
PEEK_DATA=/var/lib/peek peekd backup /backups/peek-$(date +%F).db
```

If you use file storage, back up the data directory. If you use S3-compatible storage, back up or lifecycle-manage the bucket as well as the SQLite database.
