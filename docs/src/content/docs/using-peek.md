---
title: Using Peek
description: Upload HTML, share review links, collect comments, and inspect activity.
kicker: peek upload
---

## Upload A Page

Use the CLI when a report or generated page already exists on disk:

```sh
peek upload codebase-health-report.html --visibility public
```

The response includes a URL and a slug. Share the URL with reviewers; keep the slug for CLI commands such as `stats`, `comments`, `visibility`, and `export`.

The dashboard is better when you want to paste HTML, choose visibility visually, copy links, or manage several uploads at once.

<figure>
  <img src="/peek/assets/screenshots/05-dashboard-uploads.png" alt="Peek dashboard with the upload form and uploads table">
  <figcaption>The dashboard combines upload, link copying, stats, and delete actions in one place.</figcaption>
</figure>

## Pick Visibility

Visibility is a review policy, not a storage location.

| Visibility | Who can open it | Good for |
| --- | --- | --- |
| `public` | Anyone with the link | Fast internal sharing where the URL itself is enough control. |
| `password` | Anyone with the link and upload password | Sharing outside the signed-in group without creating accounts. |
| `private` | Signed-in Peek users | Internal reports that should stay behind the company login surface. |

Change visibility after upload when the audience changes:

```sh
peek visibility <slug> public
peek visibility <slug> password --password newpass
peek visibility <slug> private
```

## Review In Context

Open `/p/<slug>`. Peek keeps the uploaded page full-screen and adds a small toolbar for comments, view count, and the comments panel.

Select text when exact wording matters:

<figure>
  <img src="/peek/assets/screenshots/03-text-anchor.png" alt="Selected text inside a shared report with Peek's inline comment button visible">
  <figcaption>Text comments keep feedback attached to the sentence or phrase under review.</figcaption>
</figure>

Use element comments for sections, charts, cards, headings, or layout issues:

<figure>
  <img src="/peek/assets/screenshots/04-element-pin.png" alt="Peek element comment mode with a feedback composer open on a report section">
  <figcaption>Element comments pin feedback to a rendered part of the page.</figcaption>
</figure>

Use a page comment when the note is about the whole artifact rather than one target.

## Read Feedback

The comments panel shows author, timestamp, scope, target text, and body. Anchored comments are clickable, so owners can jump back to the relevant part of the page.

<figure>
  <img src="/peek/assets/screenshots/02-viewer-comments.png" alt="Peek shared-page viewer with the comments panel open">
  <figcaption>The panel is the review thread: every note carries enough context to act on it later.</figcaption>
</figure>

Owners can read comments from the CLI:

```sh
peek comments <slug>
peek export <slug>
```

Use `comments` for quick terminal review. Use `export` when an agent, ticket, or audit trail needs the upload metadata, visits, and comments together.

## Stats

Stats answer a narrow question: did reviewers open the page?

<figure>
  <img src="/peek/assets/screenshots/06-upload-stats.png" alt="Peek stats page with visit totals and recent visits">
  <figcaption>Stats show total visits, unique visitors, and recent visits for one upload.</figcaption>
</figure>

Peek hashes analytics IPs. Treat stats as internal review activity, not a full web analytics product.

## CLI Workflow

Configure once:

```sh
peek login --host https://peek.example.com
```

Common user commands:

```text
peek upload <file.html> --visibility public|password|private
peek list
peek stats <slug>
peek comments <slug>
peek export <slug>
peek visibility <slug> public|password|private
peek delete <slug>
peek delete-all
```

For automation, set `PEEK_HOST` and `PEEK_TOKEN`. Prefer browser login, `--token-stdin`, or `--token-file` over passing a token directly in the command line.
