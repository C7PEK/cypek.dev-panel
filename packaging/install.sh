#!/usr/bin/env bash
set -euo pipefail

REPO="${HOMEPANEL_REPO:-C7PEK/cypek.dev-panel}"
BIN=/usr/local/bin/homepanel
CONFIG=/etc/homepanel/config.toml
DATA=/var/lib/homepanel
LOGS=/var/log/homepanel
USER_NAME=homepanel
PORT=8420
REL="${HOMEPANEL_VERSION:-latest}"
BIN_URL="${HOMEPANEL_BIN_URL:-}"
UNINSTALL=0
PURGE=0

say()  { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'USAGE'
panel.cypek.dev installer

  curl -fsSL https://github.com/C7PEK/cypek.dev-panel/releases/latest/download/install.sh | sudo bash

Options (after "bash -s --" when piped):
  --port N          panel port (default 8420)
  --version vX.Y.Z  install a specific release
  --uninstall       remove the panel (asks before deleting data)
  --purge           with --uninstall: also delete servers, worlds and backups

Run it again at any time to repair or upgrade.
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    --port) PORT="$2"; shift 2 ;;
    --version) REL="$2"; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
    --purge) PURGE=1; shift ;;
    --bin-url) BIN_URL="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "run as root (sudo)"
command -v systemctl >/dev/null 2>&1 || die "systemd is required"
[ -d /run/systemd/system ] || die "systemd is not the running init"
command -v curl >/dev/null 2>&1 || die "curl is required (apt install curl ca-certificates)"

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported architecture: $ARCH (amd64 and arm64 only)" ;;
esac

if [ -r /etc/os-release ]; then
  OS_ID="$(. /etc/os-release && echo "${ID:-}")"
  OS_VER="$(. /etc/os-release && echo "${VERSION_ID:-0}")"
  OS_NAME="$(. /etc/os-release && echo "${PRETTY_NAME:-unknown}")"
  case "$OS_ID" in
    debian) [ "${OS_VER%%.*}" -ge 12 ] 2>/dev/null || die "Debian $OS_VER is too old; Debian 12+ is required" ;;
    ubuntu) [ "${OS_VER%%.*}" -ge 24 ] 2>/dev/null || die "Ubuntu $OS_VER is not supported: its polkit is too old to let the panel control game servers. Use Ubuntu 24.04+ or Debian 12+" ;;
    *) warn "untested distribution: $OS_NAME; continuing" ;;
  esac
fi

if [ "$UNINSTALL" -eq 1 ]; then
  say "Stopping the panel and game servers"
  systemctl disable --now homepanel 2>/dev/null || true
  for u in $(systemctl list-units --plain --no-legend 'homepanel-server@*' 2>/dev/null | awk '{print $1}'); do
    systemctl stop "$u" || true
  done
  systemctl stop homepanel-phpmyadmin 2>/dev/null || true
  systemctl disable homepanel-phpmyadmin 2>/dev/null || true
  rm -f /etc/systemd/system/homepanel.service /etc/systemd/system/homepanel-server@.service \
    /etc/systemd/system/homepanel-update.service /etc/systemd/system/homepanel-database-install.service \
    /etc/systemd/system/homepanel-phpmyadmin.service /etc/polkit-1/rules.d/50-homepanel.rules
  rm -rf /etc/systemd/system.control/homepanel-server@*.service.d
  systemctl daemon-reload
  rm -f "$BIN" "$BIN.bak"
  if [ "$PURGE" -eq 0 ] && [ -t 0 ]; then
    read -r -p "Delete all data in $DATA (servers, worlds, backups)? [y/N] " ans
    [ "${ans:-N}" = "y" ] || [ "${ans:-N}" = "Y" ] && PURGE=1
  fi
  if [ "$PURGE" -eq 1 ]; then
    rm -rf "$DATA" "$LOGS" /etc/homepanel
    userdel "$USER_NAME" 2>/dev/null || true
    say "Removed the panel and its data"
  else
    say "Removed the panel; data kept in $DATA and /etc/homepanel"
  fi
  exit 0
fi

say "Installing panel.cypek.dev ($ARCH, $REL)"
if ! id "$USER_NAME" >/dev/null 2>&1; then
  useradd --system --home-dir "$DATA" --shell /usr/sbin/nologin "$USER_NAME"
fi
mkdir -p "$DATA" "$LOGS" /etc/homepanel

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
if [ -n "$BIN_URL" ]; then
  say "Fetching binary from $BIN_URL"
  curl -fsSL "$BIN_URL" -o "$TMP/homepanel"
else
  if [ "$REL" = "latest" ]; then
    BASE="https://github.com/$REPO/releases/latest/download"
  else
    BASE="https://github.com/$REPO/releases/download/$REL"
  fi
  say "Downloading homepanel-linux-$ARCH"
  curl -fsSL "$BASE/homepanel-linux-$ARCH" -o "$TMP/homepanel" || die "download failed (is there a release at https://github.com/$REPO/releases ?)"
  curl -fsSL "$BASE/SHA256SUMS" -o "$TMP/SHA256SUMS" || die "could not download SHA256SUMS"
  say "Verifying checksum"
  EXPECTED="$(awk -v f="homepanel-linux-$ARCH" '$2==f || $2=="*"f {print $1}' "$TMP/SHA256SUMS")"
  [ -n "$EXPECTED" ] || die "homepanel-linux-$ARCH is not listed in SHA256SUMS"
  ACTUAL="$(sha256sum "$TMP/homepanel" | awk '{print $1}')"
  [ "$EXPECTED" = "$ACTUAL" ] || die "checksum mismatch: expected $EXPECTED, got $ACTUAL"
fi
install -m 755 "$TMP/homepanel" "$BIN"

say "Writing config, systemd units and polkit rule"
"$BIN" init-config --port "$PORT"
"$BIN" install-files
chown root:"$USER_NAME" /etc/homepanel
chmod 750 /etc/homepanel
chown "$USER_NAME:$USER_NAME" "$CONFIG"
chmod 600 "$CONFIG"
chown -R "$USER_NAME:$USER_NAME" "$DATA" "$LOGS"
chmod 750 "$DATA"
systemctl daemon-reload

PK="$(pkaction --version 2>/dev/null | awk '{print $NF}' || true)"
if [ -z "$PK" ]; then
  warn "polkit is not installed; installing polkitd"
  export DEBIAN_FRONTEND=noninteractive
  apt-get install -y -qq polkitd >/dev/null 2>&1 || apt-get install -y -qq policykit-1 >/dev/null 2>&1 || warn "could not install polkit"
  PK="$(pkaction --version 2>/dev/null | awk '{print $NF}' || true)"
fi
case "$PK" in
  0.105*|0.10[0-4]*) warn "polkit $PK is too old: game servers will not start. Debian 12+ or Ubuntu 24.04+ is needed" ;;
esac
command -v xz >/dev/null 2>&1 || warn "xz is not installed; FiveM artifacts will extract slowly (apt install xz-utils)"

say "Starting the panel"
systemctl enable --now homepanel
systemctl restart homepanel
sleep 2
if ! systemctl is-active --quiet homepanel; then
  journalctl -u homepanel -n 20 --no-pager || true
  die "homepanel.service failed to start"
fi

IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
echo
say "panel.cypek.dev is running"
echo "    URL:          http://${IP:-<this-machine-ip>}:$PORT"
if [ -f "$DATA/setup-token" ]; then
  echo "    Setup token:  $(cat "$DATA/setup-token")"
  echo "    (needed once to create the owner account)"
fi
echo "    SFTP:         port 2022 (login and password as in the panel)"
echo "    Logs:         journalctl -u homepanel -f"
echo "    Update:       in the panel (Settings) or: sudo homepanel update"
echo
echo "    Open the URL in a browser and create the owner account with the setup token."
