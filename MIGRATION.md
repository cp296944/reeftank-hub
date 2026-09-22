# Repository migration provenance

ReefTank Hub began as the Raspberry Pi bridge inside
`cp296944/k7-led-Raspberry-controller/pi-bridge`.

The local `git subtree` helper was unavailable during extraction, so the
current `pi-bridge` tree was copied into this repository and its Go module was
renamed. Full pre-extraction history remains in the legacy repository.

Source checkpoints:

- `713e892` — K7 Pi Bridge `pi-v1.0.4` baseline.
- `978a67b` — ReefTank Hub shell and safe bootstrap foundation.
- `5882de6` — persistent aquarium equipment mapping.

No APK, device token, HA token, Google key, device serial or live configuration
belongs in this repository.
