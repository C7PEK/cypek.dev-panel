# panel.cypek.dev

A web panel for game servers and Discord bots on one home server: a mini PC, an old laptop, a Raspberry Pi or a cheap VPS.

One binary, no Docker, no PHP, no separate daemon. Every game server runs as its own systemd unit, so restarting or updating the panel never kicks players.

## Install

Debian 12+ or Ubuntu 24.04+, amd64 or arm64.

```sh
curl -fsSL https://github.com/C7PEK/cypek.dev-panel/releases/latest/download/install.sh | sudo bash
```

Logged in as root on a minimal system without `curl` or `sudo` (for example a fresh Proxmox LXC):

```sh
apt update && apt install -y curl && curl -fsSL https://github.com/C7PEK/cypek.dev-panel/releases/latest/download/install.sh | bash
```

At the end the installer prints the panel address and a one-time setup token. Open the address, paste the token and create your account.

| | |
|---|---|
| Update | Settings in the panel, or `sudo homepanel update` |
| Repair | run the install command again |
| Other port | `... \| sudo bash -s -- --port 9000` |
| Uninstall | `... \| sudo bash -s -- --uninstall` |
| Forgot password | `sudo homepanel reset-password <user>` |

More in [docs/install.md](docs/install.md).

## What it runs

| Category | Templates |
|---|---|
| Minecraft, plugins | Paper, Purpur, Folia |
| Minecraft, mods | Forge, NeoForge, Fabric, Quilt |
| Minecraft, other | Vanilla, Velocity proxy, Bedrock |
| FiveM | FXServer, FXServer with txAdmin |
| Discord bots | Node.js and Python, from your own files or a git repository |

Java, Node.js and Python are downloaded by the panel, nothing has to be installed on the system. Templates from Pterodactyl, Pelican and PufferPanel can be imported, and you can write your own in YAML ([docs/templates.md](docs/templates.md)).

## Features

- Live console with command history, search and command hints
- File manager with an editor, upload, download from a link (Google Drive, Dropbox, OneDrive, GitHub) and unzip
- Built-in SFTP on port 2022 for WinSCP or FileZilla ([docs/sftp.md](docs/sftp.md))
- Mods and plugins from Modrinth in one click ([docs/mods.md](docs/mods.md))
- Scheduled restarts with in-game warnings, commands and backups
- Backups to any folder, including a mounted NAS, with retention
- MariaDB and phpMyAdmin for servers that need a database ([docs/database.md](docs/database.md))
- Discord alerts when a server crashes and does not come back ([docs/alerts.md](docs/alerts.md))
- Users, roles and per-server permissions, 2FA, API keys and an audit log
- Servers that switch off after a set number of days (handy when you lend one to a friend)
- Port conflict checks and the firewall command to open a port
- English, Polish and German

## Development

Go 1.27+ and Node 22+.

```sh
make dev       # backend on http://127.0.0.1:8420 with hot reload for the UI
make build     # bin/homepanel
make test      # go vet, go test, tsc, oxlint
make release   # linux amd64 and arm64 binaries with SHA256SUMS in dist/
```

Dev mode keeps everything in `./.devdata/` and works without systemd.

Layout: `cmd/homepanel` (CLI), `internal/` (Go packages), `web/` (React, Vite, Tailwind, shadcn/ui), `templates/` (game templates), `packaging/` (installer, systemd units, polkit rule), `docs/`.

## Credits

Discord, FiveM, Node.js and Python icons come from [Simple Icons](https://simpleicons.org) (CC0 1.0). The trademarks belong to their owners. This project is not affiliated with Mojang, Microsoft, Cfx.re or Discord.

## License

MIT, see [LICENSE](LICENSE).
