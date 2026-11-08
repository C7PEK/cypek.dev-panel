# Discord alerts

*Admin → Alerts* sets one Discord webhook for the whole panel, who to ping,
and the delay. A server can turn alerts off or use its own webhook and ping
(Settings tab, needs the `settings` permission).

## When a message is sent

The monitor runs on every metrics tick (3 s). It reads the shim's
`.homepanel/events.jsonl`, so crashes are noticed even if the panel was
restarting at that moment; crashes up to 10 minutes old are caught up.

| Situation | Message |
|---|---|
| Crash (non-zero exit or signal) and the server is not up `graceSec` after the first crash (default 20 s) | 🔴 down, **with ping**, exit code and last 15 log lines |
| Second crash in the same incident (crash loop), even if systemd is restarting it | 🔴 down, with ping |
| After a down alert, running again for 30 s | 🟢 back up, no ping (option) |
| Crashed once and systemd restarted it; running for 30 s | 🟡 crashed but recovered, no ping (option) |
| Error burst in the console (`errorRegex`), at most every 10 min per server | 🟠 console error, no ping (option, off by default) |

An exit with code 0 counts as down only for Discord bots and "other"
servers. For Minecraft and FiveM it usually means someone typed `stop` in the
console. A stop from the panel never alerts.

## Safety

- Only `https://discord.com/api/webhooks/...` links are accepted (also
  discordapp.com, ptb. and canary.), so the panel cannot be used to POST
  anywhere else.
- Webhooks are encrypted in the database and never returned by the API (only
  `…/abcd`).
- Messages set `allowed_mentions`, so only the configured ping can notify
  anyone. Server names are escaped.
- Log lines in messages can contain whatever the server prints; turn
  "include logs" off if your console shows secrets.
