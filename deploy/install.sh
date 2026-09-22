#!/usr/bin/env bash
# Install / update ReefTank Hub on a Raspberry Pi (Debian, arm64).
#
# Run from a checkout:   sudo deploy/install.sh [options]
#
#   --binary PATH   install this local binary (bootstrap, before any release exists)
#   --tag TAG       install this release tag from GitHub (default: latest hub-v*)
#   --channel C     stable | prerelease   (default: stable)
#   --no-start      set everything up but don't start the service
#
# Idempotent. Never touches eth0 / sshd. Config and data survive re-runs.
set -euo pipefail

ROOT=/opt/reeftank-hub
USER_NAME=reefhub
REPO=cp296944/reeftank-hub
CHANNEL=stable
BINARY=""
TAG=""
START=1

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

die() { echo "install: $*" >&2; exit 1; }
[[ $EUID -eq 0 ]] || die "run with sudo"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary) BINARY="$2"; shift 2 ;;
    --tag) TAG="$2"; shift 2 ;;
    --channel) CHANNEL="$2"; shift 2 ;;
    --no-start) START=0; shift ;;
    *) die "unknown option: $1" ;;
  esac
done

echo "==> user + directories"
if ! id -u "$USER_NAME" >/dev/null 2>&1; then
  useradd --system --home-dir "$ROOT" --no-create-home --shell /usr/sbin/nologin "$USER_NAME"
fi
mkdir -p "$ROOT"/{releases,data,state}
chown -R "$USER_NAME:$USER_NAME" "$ROOT"

# ---- obtain the binary --------------------------------------------------------
resolve_tag() {
  local api="https://api.github.com/repos/$REPO/releases"
  python3 - "$api" "$CHANNEL" <<'PY'
import json,sys,urllib.request
api,channel=sys.argv[1],sys.argv[2]
def ver(t):
    t=t.removeprefix("hub-v").split("-")[0]
    p=(t.split(".")+["0","0","0"])[:3]
    return tuple(int(x) for x in p)
data=json.load(urllib.request.urlopen(api,timeout=20))
best=None
for r in data:
    t=r["tag_name"]
    if not t.startswith("hub-v") or r["draft"]: continue
    if r["prerelease"] and channel!="prerelease": continue
    names={a["name"] for a in r["assets"]}
    if "reeftank-hub-linux-arm64" not in names or "SHA256SUMS" not in names: continue
    if best is None or ver(t)>ver(best): best=t
print(best or "")
PY
}

install_binary() {  # $1 = tag , $2 = source (local path OR "")
  local tag="$1"
  local src="${2:-}"
  local reldir="$ROOT/releases/$tag"
  mkdir -p "$reldir"
  if [[ -n "$src" ]]; then
    install -m 0755 -o "$USER_NAME" -g "$USER_NAME" "$src" "$reldir/reeftank-hub"
  else
    local base="https://github.com/$REPO/releases/download/$tag"
    tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' RETURN
    curl -fsSL "$base/reeftank-hub-linux-arm64" -o "$tmp/bin"
    curl -fsSL "$base/SHA256SUMS" -o "$tmp/sums"
    ( cd "$tmp" && cp bin reeftank-hub-linux-arm64 && sha256sum -c --ignore-missing sums )
    install -m 0755 -o "$USER_NAME" -g "$USER_NAME" "$tmp/bin" "$reldir/reeftank-hub"
  fi
  ln -sfn "$reldir" "$ROOT/current.tmp"
  mv -Tf "$ROOT/current.tmp" "$ROOT/current"
  echo "$tag" > "$ROOT/state/CONFIRMED"   # a fresh manual install is trusted
  chown -R "$USER_NAME:$USER_NAME" "$ROOT/releases" "$ROOT/state" "$ROOT/current"
}

if [[ -n "$BINARY" ]]; then
  [[ -f "$BINARY" ]] || die "binary not found: $BINARY"
  TAG="${TAG:-$("$BINARY" --version 2>/dev/null | awk '{print $1}')}"
  TAG="${TAG:-hub-vdev}"
  echo "==> installing local binary as $TAG"
  install_binary "$TAG" "$BINARY"
else
  [[ -n "$TAG" ]] || TAG="$(resolve_tag)"
  [[ -n "$TAG" ]] || die "no matching hub-v* release found on channel '$CHANNEL' (use --binary to bootstrap)"
  echo "==> installing release $TAG from GitHub"
  install_binary "$TAG" ""
fi

# ---- config -----------------------------------------------------------------
CFG="$ROOT/data/config.json"
if [[ ! -f "$CFG" ]]; then
  echo "==> writing default config $CFG"
  cat > "$CFG" <<JSON
{
  "lamp_host": "192.168.4.1",
  "lamp_port": 8266,
  "listen": ":80",
  "proxy": ":8266",
  "latitude": 22.63,
  "longitude": 120.30,
  "timezone": "Asia/Taipei",
  "smooth_ramp": false,
  "update_repo": "$REPO",
  "update_channel": "$CHANNEL",
  "update_interval": "1h",
  "install_root": "$ROOT",
  "data_dir": "$ROOT/data",
  "log_level": "info"
}
JSON
  chown "$USER_NAME:$USER_NAME" "$CFG"
fi

# ---- system integration ---------------------------------------------------
echo "==> systemd units + polkit + rollback helper"
install -m 0755 "$here/rollback.sh"                       "$ROOT/rollback.sh"
install -m 0644 "$here/reeftank-hub.service"              /etc/systemd/system/reeftank-hub.service
install -m 0644 "$here/reeftank-hub-rollback.service"     /etc/systemd/system/reeftank-hub-rollback.service
install -d /etc/polkit-1/rules.d
install -m 0644 "$here/49-reeftank-hub.rules"             /etc/polkit-1/rules.d/49-reeftank-hub.rules

systemctl daemon-reload
systemctl enable reeftank-hub.service >/dev/null

if [[ $START -eq 1 ]]; then
  systemctl restart reeftank-hub.service
  sleep 2
  systemctl --no-pager --lines=15 status reeftank-hub.service || true
  echo
  echo "==> $("$ROOT/current/reeftank-hub" --version)"
  IP="$(ip -4 -br addr show eth0 | awk '{print $3}' | cut -d/ -f1)"
  [ -n "$IP" ] && echo "==> LAN UI: http://$IP/"
  HN="$(hostnamectl --static 2>/dev/null || hostname)"
  [ -n "$HN" ] && echo "==> or:     http://$HN.local/"
else
  echo "==> installed (not started; --no-start given)"
fi
