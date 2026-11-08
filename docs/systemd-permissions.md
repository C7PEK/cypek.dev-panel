# How the panel controls game servers without root

The panel runs as the system user `homepanel`. Every game server is an
instance of the template unit `homepanel-server@<id>.service` (see
`packaging/systemd/homepanel-server@.service`). The panel talks to systemd over
the system D-Bus and needs permission for exactly these calls:

| Call | polkit action | Unit name passed to polkit? |
|---|---|---|
| StartUnit / StopUnit / RestartUnit / KillUnit / ResetFailedUnit | `org.freedesktop.systemd1.manage-units` | yes (`unit` detail) |
| SetUnitProperties (MemoryMax, CPUQuota; `runtime=false`) | `org.freedesktop.systemd1.manage-units` | yes |
| GetUnitProperties, GetServiceProperty (status, cgroup stats) | none (read-only) | – |
| EnableUnitFiles / DisableUnitFiles / RevertUnitFiles | `org.freedesktop.systemd1.manage-unit-files` | **no** |

Verified on Debian 13 (systemd 257, polkit 126) by running the supervisor
integration test as `homepanel` (`internal/supervisor/systemd_integration_test.go`).

## Decision: no `manage-unit-files`

Because systemd does not attach the unit name to `manage-unit-files`, a polkit
rule granting it to `homepanel` would let the panel enable or disable *any*
unit on the host. We do not want that. Consequences:

- **Autostart at boot is done by the panel**, not by `systemctl enable`.
  `homepanel.service` is enabled; on start it starts every server flagged
  `autostart` (`cmd/homepanel/serve.go`, `autostartServers`). On a plain panel
  restart servers are already running and are left alone.
- **Persistent limits** written with `SetUnitProperties(runtime=false)` live in
  `/etc/systemd/system.control/homepanel-server@<id>.service.d/`. Removing them
  would need `RevertUnitFiles`, so deleting a server resets them to unlimited
  instead (a harmless drop-in stays behind).

The polkit rule (`packaging/polkit/50-homepanel.rules`) therefore grants only
`manage-units` for units whose name starts with `homepanel-server@`.

## Hosts with polkit 0.105 (Ubuntu 22.04)

polkit 0.105 has no JavaScript rules and its `.pkla` files cannot look at the
unit name. Granting `manage-units` wholesale there would let `homepanel` stop
`sshd`. Options considered:

1. **`.pkla` granting `manage-units` to `homepanel` for everything.** One line,
   but too broad. Rejected.
2. **sudoers line for `systemctl`.** sudoers wildcards also match spaces, so
   `systemctl start homepanel-server@*` would accept
   `systemctl start homepanel-server@x.service sshd.service`. Rejected.
3. **Tiny root helper (chosen for M6).** `homepanel systemd-helper <verb> <id>
   [args]` is a subcommand of the same binary. It validates `verb` against an
   allow-list (`start stop restart kill reset-failed set-limits`) and `id`
   against `^[a-z0-9]{4,32}$`, then performs the D-Bus call as root. sudoers
   gets exactly one line:

   ```
   homepanel ALL=(root) NOPASSWD: /usr/local/bin/homepanel systemd-helper *
   ```

   Argument injection is harmless because the helper, not `systemctl`, parses
   the arguments. The supervisor gains a `viaHelper` mode that execs
   `sudo -n /usr/local/bin/homepanel systemd-helper …` instead of calling
   D-Bus directly. `install.sh` picks the mode: polkit ≥ 0.106 → rules file,
   otherwise → sudoers line. Status and stats stay direct (read-only D-Bus).

Until the helper exists, Ubuntu 22.04 users can run the panel with
`supervisor = "local"` in `config.toml` (servers as detached processes, no
sandbox, no limits) or upgrade to 24.04.
