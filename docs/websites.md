# Websites and Cloudflare Tunnel

## Static website

*New server → Websites → Static website.* The site is a server like any
other: console, files, SFTP, backups, autostart and alerts work the same.

- Upload the files to the `public` folder (Files tab or SFTP). Until there
  is an `index.html`, the site shows a short "upload your files" page.
- It listens on the chosen port (default 8080) on every interface, so it is
  reachable at `http://<machine-ip>:8080` on your home network.
- "Single-page app" serves `index.html` for unknown paths, for React Router,
  Vue Router and similar.
- Files and folders starting with a dot are never served, directories are
  not listed, and links pointing outside `public` are refused.

It is served by the panel binary itself (`homepanel static`), inside the
server's systemd sandbox. Each request is logged to the console.

## Cloudflare Tunnel

*Tunnels → New tunnel.* A tunnel is `cloudflared` running as its own
systemd unit, started and stopped separately from the sites it serves. No
router ports, no fixed IP; Cloudflare provides HTTPS.

The panel downloads `cloudflared` from Cloudflare's GitHub releases and
checks it against the SHA-256 listed in the release notes. "Update" in the
tunnel's settings fetches the newest release.

### With your own domain

1. Cloudflare dashboard: Zero Trust, Networks, Tunnels, Create a tunnel,
   type Cloudflared.
2. Copy the token (the long string after `--token`) and paste it when you
   create the tunnel in the panel. It is stored encrypted and passed to
   `cloudflared` as `TUNNEL_TOKEN`, never on the command line.
3. Under Public hostnames add your domain with type HTTP and the site's
   local address, for example `localhost:8080`. The Tunnels page lists the
   addresses of your sites.
4. Start the tunnel. One tunnel can serve several hostnames.

### Quick test tunnel

Leave the token empty and set the site address (default
`http://localhost:8080`). The tunnel prints a random
`https://...trycloudflare.com` address in its console. It changes on every
restart and is meant for testing only.

### Notes

- Do not put the panel itself behind a tunnel yet: sessions are bound to
  the client address, which Cloudflare replaces.
- Game servers are not a good fit: Cloudflare Tunnel needs a client on the
  player's side for non-HTTP traffic.
