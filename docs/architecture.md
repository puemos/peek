# Architecture

Peek is a small Go application, but the codebase is organized as if it needs to be reviewed, operated, and extended by people who care about clear ownership boundaries.

## Package Boundaries

- `cmd/peekd` is only the server executable entrypoint. It should stay thin and delegate runtime behavior to `internal/peekd`.
- `cmd/peek` is only the CLI executable entrypoint. CLI commands, formatting, config files, and HTTP client behavior live in `internal/cli`.
- `internal/peekd` owns daemon process concerns: flag and environment parsing, structured logging setup, graceful shutdown, healthcheck command behavior, and SQLite backup command behavior.
- `internal/server` owns HTTP routing, request authentication, handler orchestration, browser workflows, API responses, comments, analytics, metrics, OAuth flow coordination, setup bootstrap, and runtime settings.
- `internal/uploads` owns upload creation behavior: HTML sniffing, bcrypt password limit enforcement for upload passwords, slug generation, object-store writes, database insert coordination, quota error mapping, and storage cleanup after failed persistence.
- `internal/db` owns SQLite schema, migrations, persistence rules, transactional constraints, and database-level invariants such as quota checks and last-admin protection.
- `internal/objectstore` owns upload byte storage. The server depends only on its `Storage` interface, while file and S3 implementation details stay outside the HTTP package.
- `internal/web` owns server-rendered templates, template view models, static asset embedding, and HTML rendering configuration.
- `internal/models` holds shared data shapes passed between packages. Keep it boring: no persistence or HTTP behavior belongs there.
- `internal/version` owns build metadata injected by release builds.

## Dependency Direction

The intended direction is executable packages to internal orchestration packages, orchestration packages to domain/runtime packages, and concrete infrastructure hidden behind small interfaces. `internal/server` may depend on `internal/uploads`, `internal/db`, `internal/objectstore`, `internal/models`, and `internal/web`; `internal/uploads` may depend on `internal/db` and `internal/objectstore`; `internal/objectstore` must not depend on `internal/server`; `internal/db` must not depend on HTTP, CLI, or template packages.

## Rendering

Server-side rendering goes through `internal/web.Renderer` and `.gohtml` templates. Handlers build typed view-model aliases in `internal/server/view_models.go` and call `renderHTML`; they should not parse or execute templates directly. Browser JavaScript and CSS are embedded assets with hashed URLs so the server can send immutable cache headers for static files while keeping dynamic HTML non-cacheable.

## Storage

Uploads are created through `internal/uploads.Service`, which coordinates HTML validation, password hashing, slug generation, object storage writes, and database inserts. `internal/objectstore.Storage` is intentionally small: `Save`, `Open`, and `Delete`. S3 endpoint validation lives with S3 transport code, while the server remains responsible for decrypting secret settings before passing them into the storage package.

## Quality Gates

Before committing behavior changes, run `mise run check`. For concurrency-sensitive changes, also run `mise run race`. CI runs vet, build, race-enabled tests, coverage generation, govulncheck, and staticcheck.

## Review Rules

Prefer package-boundary changes that reduce coupling over broad rewrites. Keep handlers thin when possible, push upload creation rules into `internal/uploads`, storage rules into `internal/objectstore`, persistence rules into `internal/db`, and runtime executable concerns into `internal/peekd`. Add tests near the package that owns the invariant.
