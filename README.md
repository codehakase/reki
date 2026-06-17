# Reki

A minimalist, local-first shell history tool — fast, durable, SQLite-backed
command history with rich search. Optional, opt-in end-to-end-encrypted sync
across machines. The local tool is fully useful with no network and no accounts.

## Setting up

```sh
make build      # -> bin/reki
make test       # go test -race ./...
make cross      # static binaries for linux/amd64, linux/arm64, darwin/arm64
```

Requires Go 1.25+. Builds are pure-Go (`CGO_ENABLED=0`).

## Commands

```
reki init             Create the local DB and install the shell hook + Ctrl+R binding
reki import <shell>   Import an existing HISTFILE (zsh|bash|fish)
reki s <query>        Launch the fuzzy-search TUI
reki stats            Show basic analytics over local history
reki forget           Delete entries locally (tombstoned)
reki register         Create a relay account
reki login            Log in to a relay account
reki key              Print the encryption key to copy to another machine
reki sync             Push/pull encrypted deltas to/from the relay
```

