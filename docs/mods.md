# Minecraft plugins and mods

The **Plugins** / **Mods** tab of a Minecraft server installs add-ons from
[Modrinth](https://modrinth.com) (public API, no key).

## Server type detection

| Found in the server folder | Type | Folder |
|---|---|---|
| `libraries/net/neoforged` / `libraries/net/minecraftforge` | NeoForge / Forge | `mods/` |
| `.quilt`, `quilt-server-launch.jar` | Quilt (also takes Fabric mods) | `mods/` |
| `.fabric`, `fabric-server-launch.jar` | Fabric | `mods/` |
| `velocity.toml` | Velocity (no game version filter) | `plugins/` |
| `purpur.yml` | Purpur (also Paper/Spigot/Bukkit plugins) | `plugins/` |
| `config/paper-global.yml`, `versions/*/paper-*.jar` | Paper (also Spigot/Bukkit plugins) | `plugins/` |
| `spigot.yml` / `bukkit.yml` | Spigot / Bukkit | `plugins/` |

If no file matches, the template id decides; for example, a fresh
`minecraft-paper` server is Paper. The Minecraft version comes from the
template (`MC_VERSION_RESOLVED`), otherwise from `version.json` inside the
server jars, otherwise from the `versions/` or `libraries/net/minecraft/server/`
folders. Admins can override both in the tab; the override is stored in
`.homepanel/mods.json`.

## What happens on install

- The newest compatible **release** is picked, or a specific version the
  user chose. Required dependencies are installed too, unless the project is
  already present.
- The file is downloaded over HTTPS, at most 256 MB, and must match Modrinth's
  SHA-512. The file name must be a plain `*.jar`, and the path goes through
  `files.ResolveSafe`.
- Older files of the same project are removed. Jars added by hand are
  recognised by their SHA-1, so they also get names and update checks.
- Disabling renames `x.jar` to `x.jar.disabled`.
- Changes take effect after a server restart.

Permissions: `files.read` to view and search, `files.write` to change
anything. Installs, updates, removals and toggles are written to the audit log.
