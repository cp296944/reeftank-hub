# pi-bridge

A 24/7 controller for the Noo-Psyche **K7 Pro** aquarium light that runs on a
**Raspberry Pi** and is reachable from your whole home LAN — with over-the-air
self-update.

## Why this exists

The K7's own **LAN mode** (lamp joins your router) is unreliable; its **AP mode**
(you connect directly to the lamp's hotspot) works but strands you on the lamp's
private network. The bitbarista ESP32 controller has the same limitation — one
Wi-Fi radio, so once it joins the lamp's AP it can't also be on your LAN.

A Raspberry Pi has **two network interfaces**, so it can sit on both at once:

```
   K7 Pro AP                 Raspberry Pi                 home router / LAN
 ┌───────────┐   wlan0     ┌──────────────┐   eth0     ┌──────────────────┐
 │192.168.4.1│◄── Wi-Fi ───│  pi-bridge   │◄── cable ──►│ PC · phones · HA │
 │  :8266    │  (the "phone")│             │            │                  │
 └───────────┘             └──────────────┘            └──────────────────┘
```

The lamp stays in the mode that actually works (its own AP, one client), and
you reach the full controller UI at `http://k7-pi-wifi-controller.local/` from
anywhere on your LAN.

## Relationship to bitbarista/k7-led-controller

This is an **additive module** in a fork. It reuses upstream's tested lamp
protocol (`internal/k7tcp`, vendored + CI-synced) and serves upstream's
**unmodified** shared web UI. Goal: **1:1 feature parity** — all 18 capability
flags the ESP32 firmware advertises, implemented on the Pi. See
[docs/API.md](docs/API.md) for the endpoint/capability ledger and
[../PLAN.md](../PLAN.md) for the build plan.

Upstream files are never edited, so `git merge upstream/master` stays
conflict-free.

## Status

Under active development — see [../PROGRESS.md](../PROGRESS.md). Milestones:

| tag | what |
|---|---|
| `hub-v0.1` | Pi networking, daemon skeleton, **OTA self-update + CI release pipeline** |
| `hub-v0.3` | lamp link, raw 8266 proxy, shared UI on the LAN, the 9 "free" endpoints |
| `hub-v0.4`–`0.9` | the always-on engine (port of `arduino/src/Effects.cpp`): smooth ramp, feed/maintenance, tracked lunar, acclimation, seasonal shift |
| `hub-v1.0` | settings page, hardening, 18/18 capabilities |
| `hub-v1.1` | Home Assistant REST integration (`homeassistant/k7_lamp/`) |

## Build

```bash
cd pi-bridge
go build ./cmd/reeftank-hub                       # host
GOOS=linux GOARCH=arm64 go build ./cmd/reeftank-hub   # Raspberry Pi (arm64)
```

Zero third-party dependencies — one static binary, which is what makes OTA
trivial.

## Install on the Pi

```bash
curl -fsSL https://raw.githubusercontent.com/cp296944/reeftank-hub/master/pi-bridge/deploy/install.sh | sudo bash
```

(Details and `uninstall.sh` under [deploy/](deploy/).)

## License

MIT, same as upstream.
