# AGENTS.md

## Toolchain
- Go is managed via **mise** (`mise.toml`). Run `mise install` first.
- Activate in a shell: `eval "$(mise activate zsh)"`

## Editing rule
- Do not manually wrap lines in Markdown files; write prose/notes as natural flowing lines and let the renderer handle wrapping.

## Build
```sh
go build -o bin/peekd  ./cmd/peekd
go build -o bin/peek ./cmd/peek
```

## Lint / typecheck
```sh
go vet ./...
```

## Run (dev)
```sh
rm -rf data && ./bin/peekd --addr :7700 --data ./data --base-url http://localhost:7700
```

## Smoke test
```sh
./bin/peek login --host http://localhost:7700
./bin/peek upload <some.html>
./bin/peek list
./bin/peek stats <slug>
```
