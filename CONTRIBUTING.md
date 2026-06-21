# Contributing

Peek is a Go-first internal company tool. Contributions should make it easier for one organization or trusted team to run a self-hosted HTML review server with clear admin controls, secure defaults, auditability, backup/restore, retention, and simple operations.

## Product Scope

Treat Peek as internal infrastructure, not a SaaS product. Prefer pragmatic self-hosted workflows over product-growth features. Do not add billing, public tenant onboarding, marketplace concepts, public signup funnels, or broad multi-tenant abstractions unless the project direction explicitly changes.

## Architecture

Keep executable entrypoints thin. `cmd/peekd` delegates daemon behavior to `internal/peekd`; `cmd/peek` delegates CLI behavior to `internal/cli`.

Keep HTTP orchestration in `internal/server`, upload creation rules in `internal/uploads`, persistence rules in `internal/db`, storage backends in `internal/objectstore`, rendering and embedded assets in `internal/web`, and shared data shapes in `internal/models`.

When adding behavior, put the invariant in the package that owns it and test it there. Handlers should translate HTTP concerns into package calls; they should not become the place where storage, persistence, rendering, and validation rules are reimplemented.

## Quality Gates

Install the toolchain with:

```sh
mise install
```

Run the normal local gate before committing:

```sh
mise run check
```

Run the full gate before opening a PR or after touching auth, storage, database, settings, rendering, background workers, or package boundaries:

```sh
mise run check-full
```

Focused commands are available when iterating:

```sh
mise run fmt-check
mise run vet
mise run test
mise run race
mise run build
```

## Review Bar

A good change is small enough to review, has a clear owner package, includes tests near the invariant it changes, and does not make internal deployment or operations more surprising. Prefer boring Go, explicit errors, narrow interfaces, and standard library behavior where it fits.
