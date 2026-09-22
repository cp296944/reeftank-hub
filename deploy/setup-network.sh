#!/usr/bin/env bash
# Harden the Pi's dual-homed networking for the K7 bridge.
#
#   wlan0  -> the lamp's AP. Keep the route to 192.168.4.0/24 but make sure
#             wlan0 NEVER carries a default route. If eth0 goes down the Pi then
#             simply has no internet (correct) instead of black-holing every
#             packet through the lamp's AP, which has none.
#   eth0   -> left completely alone. This script never touches eth0 or sshd, so
#             it cannot lock you out.
#
# Debian 13 Pi OS keeps the wlan0 NetworkManager connection in a *volatile*
# /run directory (regenerated from netplan on every boot), so a one-off
# `nmcli con mod` doesn't stick. The durable mechanism is a NetworkManager
# dispatcher script that drops wlan0's default route whenever it comes up.
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "run with sudo" >&2; exit 1; }

WLAN="${1:-wlan0}"
DISPATCH="/etc/NetworkManager/dispatcher.d/90-k7-${WLAN}-noroute"

echo "==> removing any earlier netplan drop-in"
rm -f "/etc/netplan/90-k7-${WLAN}-noroute.yaml"
command -v netplan >/dev/null && netplan generate 2>/dev/null || true

echo "==> installing dispatcher $DISPATCH"
install -d /etc/NetworkManager/dispatcher.d
cat > "$DISPATCH" <<EOF
#!/bin/sh
# Managed by reeftank-hub. Keep $WLAN off the default route (see setup-network.sh).
IFACE="\$1"
ACTION="\$2"
[ "\$IFACE" = "$WLAN" ] || exit 0
case "\$ACTION" in
  up|dhcp4-change|connectivity-change)
    ip route del default dev $WLAN 2>/dev/null || true
    ;;
esac
exit 0
EOF
chmod 755 "$DISPATCH"

echo "==> applying now (nmcli, this session)"
CON="$(nmcli -t -f NAME,DEVICE con show --active | awk -F: -v d="$WLAN" '$2==d{print $1; exit}')"
if [[ -n "${CON:-}" ]]; then
  nmcli con mod "$CON" ipv4.never-default yes ipv4.routes "" ipv4.gateway "" ipv4.route-metric -1 || true
  nmcli con down "$CON" >/dev/null 2>&1 || true
  sleep 1
  nmcli con up "$CON" >/dev/null 2>&1 || true
  sleep 5
fi
ip route del default dev "$WLAN" 2>/dev/null || true

echo
echo "==> result:"
ip route | sed 's/^/    /'
echo
if ip route | grep -qE "^default .*dev ${WLAN}"; then
  echo "    ⚠ ${WLAN} still shows a default route"
  exit 1
fi
echo "    ✓ ${WLAN} carries no default route; eth0 is the only one"
ping -c1 -W2 192.168.4.1 >/dev/null 2>&1 && echo "    ✓ lamp 192.168.4.1 still reachable" || echo "    ⚠ lamp not answering ping"
