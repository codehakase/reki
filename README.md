# Seki

A minimalist, local-first shell history tool — fast, durable, SQLite-backed
command history with rich search. Optional, opt-in end-to-end-encrypted sync
across machines. The local tool is fully useful with no network and no accounts.

## Setting up

```sh
make build      # -> bin/seki
make test       # go test -race ./...
make cross      # static binaries for linux/amd64, linux/arm64, darwin/arm64
```

Requires Go 1.25+. Builds are pure-Go (`CGO_ENABLED=0`).

## Commands

```
seki init             Create the local DB and install the shell hook + Ctrl+R binding
seki import <shell>   Import an existing HISTFILE (zsh|bash|fish)
seki s <query>        Launch the fuzzy-search TUI
seki stats            Show basic analytics over local history
seki forget           Delete entries locally (tombstoned)
seki register         Create a relay account
seki login            Log in to a relay account
seki key              Print the encryption key to copy to another machine
seki sync             Push/pull encrypted deltas to/from the relay
```

