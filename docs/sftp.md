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

## Big files: download from a link instead

SFTP itself is fast (about 170–230 MB/s upload and 400 MB/s download
measured locally). When uploads take ages, the bottleneck is the path from
your computer to the server: home upload speed, Wi-Fi, or a Tailscale relay.
For big worlds or modpacks use **Files → Download from link**. The server
downloads the file itself, at the home connection's download speed (about
10 MB/s measured on the dev machine).

| Source | Works | Notes |
|---|---|---|
| Dropbox | yes | the normal share link (`dl=0` is changed to `dl=1`) |
| Google Drive | yes | share as "Anyone with the link"; single files only, not folders; large files skip the virus-scan page |
| OneDrive | yes | public share links (TODO(verify): shares API) |
| GitHub | yes | `blob` links become raw links; release assets are direct |
| Direct links | yes | any `https://` or `http://` link that serves the file itself |
| MEGA | no | files are decrypted in the browser |
| WeTransfer, MediaFire | no | they show a web page first; for MediaFire copy the address of the download button instead |

- **Safety:** only public internet addresses are allowed, so the panel can
  never be told to fetch from the LAN or localhost.
- **Size:** a file may use the free disk space minus 1 GB.
- **No overwriting:** an existing name gets " (2)" and so on.
- **Jobs:** at most 2 jobs per server and 4 in total, each cut off after 6
  hours. Jobs can be cancelled.
- **Extraction:** "Extract after download" unpacks .zip, .tar(.gz/.xz/.zst)
  in the background, with the same rules as the file manager (no links, never
  into `.homepanel`, stops when the disk would fill). Extracting an existing
  archive also runs in the background now.
