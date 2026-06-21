# AGENTS.md

## Toolchain
- Go is managed via **mise** (`mise.toml`). Run `mise install` first.
- Activate in a shell: `eval "$(mise activate zsh)"`

## Product direction
- Peek is an internal/company self-hosted tool, not a SaaS product. Prefer changes that improve single-organization deployment, admin workflows, OAuth/SSO fit, auditability, backup/restore, retention, and boring operational reliability.
- Do not add billing, public multi-tenancy, public signup/onboarding funnels, marketplace/product-growth flows, or SaaS abstractions unless explicitly requested.

## Editing rule
- Do not manually wrap lines in Markdown files; write prose/notes as natural flowing lines and let the renderer handle wrapping.

## Build
```sh
mise run build
```

## Lint / typecheck
```sh
mise run vet
mise run fmt-check
```

## Test
```sh
mise run test
```

## Local quality gate
```sh
mise run check
```

Before opening a PR or after touching shared server, database, storage, auth, or concurrency behavior:

```sh
mise run check-full
```

## Run (dev)
```sh
mise run run
```

## Smoke test
```sh
./bin/peek login --host http://localhost:7700
./bin/peek upload <some.html>
./bin/peek list
./bin/peek stats <slug>
```
