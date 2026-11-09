# Security model and known risks

The panel runs as the unprivileged user `homepanel` and never as root. Every
game server is a `homepanel-server@<id>` systemd unit running as the same user.

## What protects what

| Area | Protection |
|---|---|
| Login | argon2id; limits per address and per address+account; TOTP has its own per-user limit and the password step does not reset it; backup codes are single-use (atomic) |
| Sessions | random 32-byte token stored hashed; `HttpOnly`, `SameSite=Strict`; `__Host-` + `Secure` over HTTPS; **bound to the client address** (IPv6 by /64), because cookies are not port-scoped and any other web service on the panel host could receive them; a password change ends all sessions and revokes API keys |
| CSRF | token on every state-changing request (constant-time compare), `Sec-Fetch-Site` check, JSON-only bodies outside the file routes |
| API keys | scoped keys only reach server routes within their scope (including console input); admin routes need a full `*` key; keys never manage the account (`/auth/*`) |
| Permissions | every server route checks its permission; schedules need the permissions of their actions (console.write, power, backups); changing the start command is admin-only; console input re-checks permission and session per command and drops control characters |
| Secrets | encrypted in the database; masked in variables and in the run config shown by the API (env/args hidden without `settings`); masked in install logs; `.homepanel/` (run.json) is never backed up |
| Browser | strict CSP (no inline scripts), `X-Frame-Options: DENY`, nosniff; file downloads are attachments; no XSS sinks (all text rendered by React); console history cleared on logout |
| Files written by the panel | `internal/safefs` (`os.Root`): writes in server folders never follow links planted by a game; random `O_EXCL` temp names; reads are regular-files-only and size-capped |
| Install programs | npm/pip/venv run inside the server's own unit (its sandbox), never in the panel process; the git remote is always the configured URL |
| Archives | entries checked with `files.ResolveSafe`; hardlinks are copied, existing files are replaced (never written through); uploads may not contain links nor touch `.homepanel` |
| Root helpers | the database installer and self-update create files with `O_EXCL|O_NOFOLLOW` and `fchown`; `/etc/homepanel` is `root:homepanel 0750` |
| phpMyAdmin | PHP's built-in server with a router that serves only the UI entry points and assets |

## Known risks (owner decisions, 2026-11-09)

- **Game servers can read the panel's secrets.** All servers run as
  `homepanel`, and their sandbox (`ProtectSystem=strict`) blocks writes but
  not reads. A malicious plugin, FiveM resource or bot can read:
  - `/etc/homepanel/config.toml` (the encryption key),
  - `panel.db`,
  - `database.json`,
  - the files of other servers.

  It can also send input to other servers' console sockets. The fix would
  be to hide those paths from the server units: `TemporaryFileSystem=` +
  `BindPaths=`, `InaccessiblePaths=/etc/homepanel`, and per-server socket
  directories. It is not applied at the owner's request ("don't touch the
  game servers"). Only run plugins/mods/bots you trust.
- **SFTP accepts the panel password even with 2FA enabled** (owner's
  choice). Use a strong password; consider SSH keys or a separate SFTP
  password later.
- `files.write`, `settings` on a bot (git URL) and `schedules` with
  console.write all mean "can run code as that server". Give them only to
  people you trust with the machine.
- The panel speaks plain HTTP by default. On an untrusted LAN, enable
  `tls.self_signed` or use Tailscale.
- `GET /api/v1/version` is public and shows the build.
