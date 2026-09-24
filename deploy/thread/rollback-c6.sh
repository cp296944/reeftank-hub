#!/usr/bin/env bash
set -euo pipefail

[[ ${EUID} -eq 0 ]] || { echo "Run with sudo." >&2; exit 1; }
[[ ${1:-} == --confirm ]] || { echo "Usage: sudo $0 --confirm" >&2; exit 2; }
root=/opt/reeftank-hub/thread
mapfile -t radios < <(find /dev/serial/by-id -maxdepth 1 -type l -name 'usb-Espressif_USB_JTAG_serial_debug_unit_*-if00' 2>/dev/null | sort)
[[ ${#radios[@]} -eq 1 ]] || { echo "C6 target is not unique." >&2; exit 3; }
backup=$(find "${root}/backups" -maxdepth 1 -type f -name 'c6-*.bin' | sort | tail -1)
[[ -n ${backup} && -f ${backup}.sha256 ]] || { echo "No verified C6 backup found." >&2; exit 4; }
sha256sum -c "${backup}.sha256"
docker stop reeftank-otbr >/dev/null 2>&1 || true
"${root}/tools/bin/python" -m esptool --chip esp32c6 --port "${radios[0]}" --baud 460800 write-flash 0x0 "${backup}"
"${root}/tools/bin/python" -m esptool --chip esp32c6 --port "${radios[0]}" verify-flash 0x0 "${backup}"
echo "C6 backup restored and verified: ${backup}"
