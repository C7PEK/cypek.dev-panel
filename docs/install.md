# Installing and updating

## Requirements

- Debian 12+ or Ubuntu 22.04+ (other systemd distributions may work), amd64 or arm64
- `curl` and `ca-certificates`; `xz-utils` is recommended (FiveM artifacts)
- root for the installer; the panel itself runs as the `homepanel` system user

## Install

```sh
curl -sSL https://panel.cypek.dev/install.sh | sudo bash
```

Options: `--port 8420`, `--version vX.Y.Z` (default: latest release). The
script is idempotent: running it again repairs files and upgrades the binary
without touching your data.

What it does: creates the `homepanel` user and directories, downloads the
release binary for your architecture and verifies it against `SHA256SUMS`,
writes `/etc/homepanel/config.toml` (random secrets, only if missing),
installs the systemd units and the polkit rule (`homepanel install-files`),
enables `homepanel.service` and prints the URL plus the one-time setup token.

Open the URL, paste the token, create the owner account.

## Paths

| Path | Purpose |
|---|---|
| `/usr/local/bin/homepanel` | the binary (panel, shim, CLI) |
| `/etc/homepanel/config.toml` | port, data paths, secrets, `supervisor`, `backups_dir`, `max_upload_mb` |
| `/var/lib/homepanel/panel.db` | SQLite database (backed up before each migration) |
| `/var/lib/homepanel/servers/<id>/` | one directory per server |
| `/var/lib/homepanel/runtimes/` | Java, Node.js, Python downloads shared by servers |
| `/var/lib/homepanel/backups/<id>/` | backups (change with `backups_dir`, e.g. a NAS mount) |
| `/var/lib/homepanel/templates/*.yaml` | your own templates |

## Update

```sh
sudo homepanel update          # downloads the newest release, verifies it, restarts only the panel
sudo homepanel update --check  # just report
```

Game servers run as their own systemd units, so a panel update or restart
never stops them; the panel reattaches to the running consoles afterwards.

## Useful commands

```sh
journalctl -u homepanel -f                       # panel log
journalctl -u 'homepanel-server@<id>' -f         # one server's shim log (console output is in the server dir under logs/)
sudo homepanel reset-password <username>         # lost password
systemctl list-units 'homepanel-server@*'        # running servers
```

## Firewall and router

The panel never changes firewall rules. The server page lists the ports to
forward on your router, and the API (`/api/v1/system/firewall`) returns the
`ufw` / `firewall-cmd` commands for the detected firewall.

## Uninstall

```sh
sudo bash install.sh --uninstall          # asks whether to delete data
sudo bash install.sh --uninstall --purge  # non-interactive, deletes everything
```
