# HomePanel

A simple web panel for running game servers and bots (FiveM, Minecraft, Discord bots)
on a single homelab machine. One static Go binary, SQLite, embedded React UI, servers
run as systemd units so they survive panel restarts and updates.

> Working name. Early development — see `CLAUDE.md` for the architecture and roadmap.

## Development

Requirements: Go 1.27+, Node 22+ with npm.

```sh
make dev          # Go backend on http://127.0.0.1:8420 (--dev) + Vite with hot reload
make build        # frontend + backend -> bin/homepanel
make test         # go vet + go test -race, tsc, oxlint
make release      # linux/amd64 + linux/arm64 binaries + SHA256SUMS in dist/
```

In dev mode all data lives in `./.devdata/` (config, SQLite, server dirs). On first run
the panel prints a one-time setup token; open the URL from the banner and create the
owner account with it. The token is also saved as `.devdata/setup-token`.

Other commands:

```sh
bin/homepanel serve --config /etc/homepanel/config.toml   # production
bin/homepanel reset-password <username> [--dev]          # set a new password from the terminal
bin/homepanel version
```

## Layout

See `CLAUDE.md` §3. Short version: `cmd/homepanel` (CLI entry), `internal/*` (Go packages),
`web/` (Vite + React + Tailwind + shadcn/ui), `templates/` (game templates), `packaging/`
(install script, systemd units, polkit rule).

## License

MIT, see `LICENSE`.
