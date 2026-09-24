#!/usr/bin/env bash
set -euo pipefail

[[ ${EUID} -eq 0 ]] || { echo "Run with sudo." >&2; exit 1; }
[[ ${1:-} == --confirm ]] || { echo "Usage: sudo $0 --confirm" >&2; exit 2; }

root=/opt/reeftank-hub/thread
tag=rcp-v0.1.2
asset=reeftank-esp32c6-ot-rcp.bin
expected=341aa96588e0be71090962fcaa4eb083038370d4db312f3955e54d923f53c2d5
mkdir -p "${root}/firmware" "${root}/backups" "${root}/tools"

mapfile -t radios < <(find /dev/serial/by-id -maxdepth 1 -type l -name 'usb-Espressif_USB_JTAG_serial_debug_unit_*-if00' 2>/dev/null | sort)
[[ ${#radios[@]} -eq 1 ]] || { printf 'Expected exactly one Espressif USB JTAG C6, found %d.\n' "${#radios[@]}" >&2; exit 3; }
device=${radios[0]}

curl -fL --retry 4 --connect-timeout 15 \
  "https://github.com/cp296944/reeftank-hub/releases/download/${tag}/${asset}" \
  -o "${root}/firmware/${asset}.download"
printf '%s  %s\n' "${expected}" "${root}/firmware/${asset}.download" | sha256sum -c -
mv "${root}/firmware/${asset}.download" "${root}/firmware/${asset}"

if [[ ! -x ${root}/tools/bin/python ]]; then
  if ! python3 -m venv "${root}/tools"; then
    apt-get update
    apt-get install -y python3-venv
    python3 -m venv "${root}/tools"
  fi
  "${root}/tools/bin/pip" install --disable-pip-version-check 'esptool==5.3.1'
fi
esptool=("${root}/tools/bin/python" -m esptool --chip esp32c6 --port "${device}")
docker stop reeftank-otbr >/dev/null 2>&1 || true
"${esptool[@]}" flash-id

stamp=$(date -u +%Y%m%dT%H%M%SZ)
backup="${root}/backups/c6-${stamp}.bin"
"${esptool[@]}" read-flash 0 ALL "${backup}"
sha256sum "${backup}" > "${backup}.sha256"

"${esptool[@]}" --baud 460800 write-flash 0x0 "${root}/firmware/${asset}"
# write-flash performs an immediate ROM/stub hash verification before reset.
# Do not run a second whole-image verify after boot: the firmware may
# initialise mutable data inside the merged image range, producing a false
# mismatch even though the write-time hash verification succeeded.
printf 'tag=%s\nsha256=%s\ndevice=%s\nflashed_at=%s\nbackup=%s\n' \
  "${tag}" "${expected}" "${device}" "$(date -u +%FT%TZ)" "${backup}" > "${root}/firmware/current"
chmod 0644 "${root}/firmware/current"
echo "C6 RCP flashed and write-hash verified: ${tag}"
