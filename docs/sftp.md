# SFTP

The panel has its own SFTP server: no system `sshd` configuration and no
Linux accounts. It works with WinSCP, FileZilla, Cyberduck, VS Code (SFTP
extensions) and the `sftp` command.

| Setting | Value |
|---|---|
| Host | the panel's address (e.g. `192.168.1.10`) |
| Port | `2022` (`[sftp] listen` in `config.toml`) |
| Login | `<panel username>`, which shows every server you can read as a folder `Name (id)`, or `<panel username>.<server id>`, which opens one server |
| Password | your panel password |
| Protocol | SFTP (not FTP/FTPS) |

The Files tab shows these details with copy buttons and an `sftp://` link.

## Rules

- **Permissions:** reading needs the server's `files.read`. Writing,
  renaming, deleting and creating folders need `files.write`, checked on every
  operation, so revoking a permission takes effect at once.
- **Paths:** every path goes through `files.ResolveSafe`, like the browser
  file manager. `..` cannot leave the server folder and symlinks pointing
  outside are refused. `.homepanel` (run.json, events) is hidden. Creating
  links is refused.
- **Shells:** only the `sftp` subsystem is served. Shells and commands are
  refused.
- **Logins:** they share the web login rate limits (per IP and per account)
  and are written to the audit log as `auth.sftp_login` /
  `auth.sftp_login_failed`. Each session that changed files adds a
  `files.sftp_session` entry with counts. Disabled accounts cannot log in.
- **2FA:** SFTP accepts the panel password even when the account has 2FA
  enabled, because SFTP apps cannot ask for a code (as in
  Pterodactyl/Pelican). This was the owner's choice; SSH keys or a separate
  SFTP password can be added later.
- **Limits:** 64 connections in total, 8 per IP.
- **Host key:** an ed25519 key in `/var/lib/homepanel/sftp_host_ed25519_key`,
  created on first start. Its fingerprint is in the Files tab and in the
  panel log; apps show it on the first connection.

From outside the LAN, forward TCP 2022 on the router only if you need it.
Over Tailscale it works without forwarding.

To turn it off:

```toml
[sftp]
enabled = false
```
