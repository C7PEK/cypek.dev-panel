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

The owner can also update from the panel. The button starts
`homepanel-update.service`, a root oneshot unit that runs the same
`homepanel update` and writes the outcome to
`/var/lib/homepanel/update-status.json`. That is the only extra unit polkit
lets the panel start. Releases come from GitHub (`C7PEK/cypek.dev-panel`);
while that repository is private, neither the button nor `homepanel update`
can download anything.

## HTTPS (optional)

The panel serves plain HTTP on the LAN by default. Options in
`/etc/homepanel/config.toml`:

```toml
[tls]
enabled = true
self_signed = true        # generate a certificate for the hostname + LAN IPs
# or your own certificate, e.g. from `tailscale cert <machine>.<tailnet>.ts.net`:
# cert_file = "/etc/homepanel/panel.crt"
# key_file  = "/etc/homepanel/panel.key"
```

The self-signed certificate is stored in `/var/lib/homepanel/tls/` and
renewed when the IPs change or it nears expiry. The browser warns once.
Compare the SHA-256 fingerprint printed at startup (`journalctl -u homepanel`)
with what the browser shows. With HTTPS, session cookies get the `Secure`
flag. For a certificate browsers trust without a warning, use
`tailscale cert` (needs MagicDNS + HTTPS enabled in the tailnet) and point
`cert_file`/`key_file` at its output. The panel user must be able to read
both files.

## Resource use

Measured on Debian 13 (amd64) with two servers configured, no browser
connected: about 26 MB RSS for the panel process (target in the plan: under
60 MB). Game servers are separate units and count separately.

## Useful commands

```sh
journalctl -u homepanel -f                       # panel log
journalctl -u 'homepanel-server@<id>' -f         # one server's shim log (console output is in the server dir under logs/)
sudo homepanel reset-password <username>         # lost password
systemctl list-units 'homepanel-server@*'        # running servers
```

## Firewall and router

The panel never changes firewall rules. The new-server summary and the
server settings list the ports to forward on your router and, when ufw or
firewalld is active, the exact commands to open them. The API is
`/api/v1/system/firewall`. Port conflicts with other servers or other
programs on the machine are detected when a server is created, changed or
started.

## Uninstall

```sh
sudo bash install.sh --uninstall          # asks whether to delete data
sudo bash install.sh --uninstall --purge  # non-interactive, deletes everything
```
