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
  kind: java                   # java | none  (node and python are coming)
  version: auto                # auto = what the downloaded server needs; or a number like 21

install:                       # run once, in order, inside the server directory
  - download: { provider: paper, version: "{{MC_VERSION}}", to: server.jar }
  - write: { path: eula.txt, content: "eula=true\n", requireConsent: EULA }
  - properties: { path: server.properties, set: { server-port: "{{PORT}}" } }

start:
  command: "{{JAVA}}"
  args: ["-Xmx{{RAM_MB}}M", "{{JVM_FLAGS...}}", "-jar", "server.jar", "nogui"]
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
recommends; spread into separate arguments), `message` (only in
`warnCommand`).

## Install steps

- `download`: `provider: paper` resolves `version` (`latest` or e.g.
  `1.21.11`), downloads the newest stable build and verifies its SHA-256.
  More providers (FiveM artifacts, git, direct URL) are planned.
- `write`: writes a small text file. With `requireConsent: VAR` the step
  fails unless the user switched that bool on.
- `properties`: sets keys in a `key=value` file, keeping other lines and
  comments. Creates the file if missing.

Paths are relative to the server directory; `..` and absolute paths are
rejected.

## Dynamic option sources

| source | provider |
|---|---|
| `paper-versions` | PaperMC Fill API: all Paper versions, newest first, pre-releases flagged |

## How a template becomes a running server

1. The wizard validates the values and creates the server as *pending*.
2. The installer runs the steps, then prepares the runtime (Java is
   downloaded from Adoptium into `/var/lib/homepanel/runtimes/java-<n>` once
   and shared by all servers that need that version).
3. `start`/`stop` are rendered into `.homepanel/run.json`; the server is *ready*.
4. Changing variables later re-runs the `write`/`properties` steps and
   re-renders `run.json` without downloading anything. "Reinstall"
   re-downloads the server files but leaves everything else in the directory.
