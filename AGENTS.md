# AGENTS.md

## Toolchain
- Go is managed via **mise** (`mise.toml`). Run `mise install` first.
- Activate in a shell: `eval "$(mise activate zsh)"`

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
rm -rf data && PEEK_ADMIN_TOKEN=dev-admin ./bin/peekd --addr :7700 --data ./data --base-url http://localhost:7700
```

## Smoke test
```sh
./bin/peek config set --host http://localhost:7700 --token dev-admin
./bin/peek upload <some.html>
./bin/peek list
./bin/peek stats <slug>
```
