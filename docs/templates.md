# Game templates

A template is one YAML file that tells the panel how to install, start and
stop a kind of server. Built-in templates live in `templates/` inside the
binary; your own go to `/var/lib/homepanel/templates/*.yaml` and are loaded
on panel start. A user template with the same `id` as a built-in one
replaces it.

Everything a template runs is executed directly by the panel, never through
a shell: `{{VAR}}` values are inserted as whole arguments, so quotes, `$()`
and `;` in a value are just characters.

## Example

```yaml
id: minecraft-paper            # lowercase letters, digits, dashes
name: Minecraft (Paper)
category: minecraft            # fivem | minecraft | discord-bot | other
description: { pl: "...", en: "..." }

variables:                     # shown in the wizard, validated by the panel
  - key: MC_VERSION            # A-Z, 0-9 and _; used as {{MC_VERSION}}
    label: { pl: Wersja, en: Version }
    type: select               # string | number | bool | select | secret
    source: paper-versions     # dynamic options (see below) or static options: [a, b]
    default: latest
  - key: RAM_MB
    type: number
    default: 2048
    min: 512
  - key: EULA
    type: bool
    consent: true              # must be switched on by the user

runtime:
  kind: java                   # java | node | python | none
  version: auto                # java: auto = what the downloaded server needs, or 21;
                               # node: lts or a major like 26 (NODE_VERSION variable overrides);
                               # python: a minor like 3.12 (PY_VERSION variable overrides)

install:                       # run once, in order, inside the server directory
  - download: { provider: paper, version: "{{MC_VERSION}}", to: server.jar }
  - write: { path: eula.txt, content: "eula=true\n", requireConsent: EULA }
  - properties: { path: server.properties, set: { server-port: "{{PORT}}" } }

start:
  command: "{{JAVA}}"
  args: ["-Xmx{{RAM_MB}}M", "{{JVM_FLAGS...}}", "-jar", "server.jar", "nogui"]
  dir: app                                 # optional working dir inside the server folder
  env: { DISCORD_TOKEN: "{{DISCORD_TOKEN}}" }   # optional environment
stop: { command: stop, timeoutSec: 90 }   # empty command = SIGTERM
readyRegex: 'Done \(\d+\.\d+s\)!'          # when the server counts as "running"

ports:
  - { name: game, port: "{{PORT}}", protocol: tcp }   # tcp | udp | tcp+udp
warnCommand: "say {{message}}"             # used by scheduled restarts
query: { kind: minecraft-ping, port: "{{PORT}}" }
```

## Variables

| type | input | checks |
|---|---|---|
| `string` | text | `maxLength`, `pattern` (regex), `required` |
| `number` | number | `min`, `max` |
| `bool` | switch | `consent: true` makes it mandatory (EULA-style) |
| `select` | list | `options: [...]` or `source:` |
| `secret` | password | stored encrypted, shown masked, passed only where the template uses it |

Builtins you can use but must not declare: `JAVA` (path to the java binary
prepared by the runtime step), `JVM_FLAGS...` (the flag list PaperMC
recommends; spread into separate arguments), `NODE`, `NPM`, `NPX` (Node.js
runtime), `PYTHON`, `PIP` (the server's own `.venv`, created by the python
runtime step), `message` (only in `warnCommand`).

## Install steps

- `download`: fetches one file to `to`.
  - `provider: paper` resolves `version` (`latest` or e.g. `1.21.11`),
    downloads the newest stable build and verifies its SHA-256.
  - `provider: fivem` resolves `version` (`recommended`, `latest` or a
    build number from the changelog API) to an FXServer Linux artifact.
    Cfx.re publishes no checksums; HTTPS only. x86_64 hosts only.
  - `provider: url` downloads a fixed `url` (https only), optionally
    checked against `sha256`.
- `extract`: unpacks `from` (tar.gz, tar.xz, zip) into `to` (`.` = server
  directory), dropping `strip` leading folders (GitHub zips have one). Paths
  that try to escape the destination abort the install. The archive is
  deleted afterwards unless `delete: false`.
- `write`: writes a small text file. With `requireConsent: VAR` the step
  fails unless the user switched that bool on.
- `properties`: sets keys in a `key=value` file, keeping other lines and
  comments. Creates the file if missing.

- `git`: clones `url` (https only) at `branch` into `to`; when the folder
  already holds a repository it fetches and hard-resets to the branch
  instead, which is what "Reinstall" / "Update from git" does. `token` names
  a secret variable used as a GitHub-style access token for private repos.
- `exec`: runs `command` with `args` in `dir` without a shell (no pipes or
  variable expansion), streaming output into the install log. `ifExists`
  skips the step when that file is missing (e.g. `requirements.txt`);
  `timeoutSec` defaults to 900. The runtime bin folders are on PATH.

Paths are relative to the server directory; `..` and absolute paths are
rejected.

## Dynamic option sources

| source | provider |
|---|---|
| `paper-versions` | PaperMC Fill API: all Paper versions, newest first, pre-releases flagged |
| `fivem-artifacts` | Cfx.re changelog API: recommended, latest and still-supported builds |

## Player queries

| kind | how |
|---|---|
| `minecraft-ping` | Server List Ping on the game port (`players.online/max`, up to 12 sample names) |
| `fivem-http` | `players.json` + `info.json` on the game port |

## How a template becomes a running server

1. The wizard validates the values and creates the server as *pending*.
2. The installer runs the steps, then prepares the runtime (Java is
   downloaded from Adoptium into `/var/lib/homepanel/runtimes/java-<n>` once
   and shared by all servers that need that version).
3. `start`/`stop` are rendered into `.homepanel/run.json`; the server is *ready*.
4. Changing variables later re-runs the `write`/`properties` steps and
   re-renders `run.json` without downloading anything. "Reinstall"
   re-downloads the server files but leaves everything else in the directory.

## Shell start, scripts and config files

These exist mainly for templates imported from other panels, but any
template may use them.

- **`start.shell`** (instead of `start.command`/`args`) runs the command line
  with `/bin/bash -c`. Every variable is exported to the environment and
  `{{KEY}}` becomes `"${KEY}"` (`{{KEY...}}` becomes an unquoted `${KEY}`
  that bash splits into words). Values are never pasted into the code, so
  `$(...)` or backticks in a value stay plain text.
- **`script`** install step: `{content, timeoutSec}` (default timeout 30 min).
  It runs in the server's own systemd unit, `homepanel-server@<id>`, with the
  same sandbox as the game: the panel user, no root, write access only to the
  server folder. A wrapper runs first. It:
  - skips `apt`/`apk`/`yum`/... calls (no root),
  - maps `java17`, `java21`, ... to the prepared Java,
  - rewrites `/mnt/server` and `/home/container` to the server folder,
  - notes which common tools (curl, jq, unzip, ...) are missing.

  `homepanel fetch mojang|paper <version> <file>` and
  `homepanel fetch fabric-installer <file>` download server jars with
  checksum checks. The panel shows a warning before such a template is used.
- **`configFiles`**: `[{path, parser, set}]` with `parser` one of
  `properties`, `ini`, `yaml`, `json` (dotted keys such as `server.port`) or
  `file` (the key is a line prefix and the value the whole new line). They are
  applied after install and again before every start, so files follow the
  variables.
- **`hidden: true`** keeps a variable out of the wizard; it still gets its
  default.
- **Builtins**: `{{SERVER_DIR}}` (absolute server folder), `{{RUNTIME_BIN}}`
  and `{{JAVA_HOME}}`. `runtime.version` may be a placeholder such as
  `{{JAVA_VERSION}}`.
- **`stop.command: "^C"`** sends SIGINT instead of console input (also
  accepted: `SIGTERM`, `SIGKILL`).

## Importing from Pterodactyl, Pelican and PufferPanel

*Admin → Templates → Import* accepts a pasted file, an uploaded file or an
https link. GitHub `blob` links are rewritten to their raw form. Links that
resolve to a private or loopback address are refused. The result is a normal
YAML template that you can review and edit before saving. It is stored in
`/var/lib/homepanel/templates/<id>.yaml`, and everything the converter could
not map is listed as a warning.

| Egg / template field | Becomes |
|---|---|
| `variables[].env_variable`, `rules` | variables: `in:` → select, `integer`/`numeric` → number with `min`/`max`, `boolean` → select 0/1 (or bool), `regex:` → pattern; `user_viewable: false` → hidden; names with token/password/secret → secret |
| `{{server.build.default.port}}`, `{{server.build.memory}}`, `{{env.X}}` | `{{SERVER_PORT}}`, `{{SERVER_MEMORY}}`, `{{X}}` (port and memory become wizard fields) |
| `docker_images` (yolks `java_21`, `nodejs_20`, `python_3.12`, `eclipse-temurin:17`, ...) | `runtime`; several versions give a `JAVA_VERSION` / `NODE_VERSION` / `PYTHON_VERSION` select, defaulting to the egg's first image. Other images (steamcmd, wine, mono) run directly on the host and need those programs installed |
| `scripts.installation.script` | a `script` install step |
| `startup` / Pelican `startup_commands` | `start.shell` (`/home/container` → server folder) |
| `config.stop`, `config.startup.done`, `config.files` | `stop`, `readyRegex`, `configFiles` (no xml parser, no wildcard keys) |
| `features: [eula]` | an EULA consent switch plus `eula.txt` |
| PufferPanel `data` | variables (an empty select option shows as `none`) |
| PufferPanel `install` ops | one bash script: `command`, `download`, `writefile`, `move`, `mkdir`, `extract`, `sleep`, `mojangdl`, `paperdl`, `fabricdl`; `javadl` sets the runtime. `if` expressions are translated to bash conditions with `env == 'host'`. Unsupported ops (forgedl, neoforgedl, steamgamedl, ...) stop the install with a clear message only when that branch is reached |
| PufferPanel `run.command` list with `if` | an if/elif chain in `start.shell` |

Not converted: Pterodactyl `file_denylist`, log settings, PufferPanel `pre`/`post`
run steps and container-only environments. Package installs inside scripts
are skipped, so install tools such as `jq` and `unzip` on the host
(`sudo apt install jq unzip`). The import dialog says which ones are missing.
