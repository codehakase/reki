# Reki

A minimalist, local-first shell history tool for one installation: fast,
durable, SQLite-backed command history with rich search and no network or
account requirement.

## Setting up

```sh
make build      # -> bin/reki
make test       # go test -race ./...
make cross      # static binaries for supported Linux and macOS targets
```

Requires Go 1.27+. Builds are pure-Go (`CGO_ENABLED=0`).

## Commands

```
reki init                         Create the local DB and install the Zsh capture hook
reki import <shell>               Import an existing HISTFILE (zsh|fish)
reki s [query]                    Launch the fuzzy-search TUI
reki stats                        Show basic analytics over local history
reki forget <selector>            Permanently delete matching entries locally
reki export [--format json|jsonl] Write all history to stdout
reki doctor                       Report local installation health
```

