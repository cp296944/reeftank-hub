# K7 Pi Bridge — Design

Status: **draft for review** · not yet implemented
Last updated: 2026-09-07

A 24/7 controller for the Noo-Psyche **K7 Pro** aquarium light, running on a
**Raspberry Pi 3B**, reachable from the whole home LAN, with online updates and
(later) Home Assistant integration.

Built by extending the open-source
[bitbarista/k7-led-controller](https://github.com/bitbarista/k7-led-controller)
Go `pc-bridge`. Intended to live in a fork as a new `pi-bridge/` module.

---

## 1. Why a Pi and not the ESP32

The bitbarista ESP32 firmware works (we compiled + ran it on a WROOM-32), but its
design connects the ESP32 **as a Wi-Fi station to the lamp's own AP**
(`192.168.4.1`, PSK `12345678`). One ESP32 radio = one station link, so the
ESP32 is then stranded on the lamp's `192.168.4.0/24` island and unreachable from
the home LAN.

The K7's **LAN mode** (lamp joins the home router) is widely reported as flaky:
AP mode and STA mode are different firmware paths on the lamp, STA mode adds
power-save / DTIM / band-steering / roaming problems the lamp's cheap stack
handles badly, and the vendor tests AP mode far more.

The Pi 3B has **two independent network interfaces** — onboard Wi-Fi and onboard
Ethernet — so it can sit on both networks at once:

```
   K7 Pro AP                Raspberry Pi 3B                home router / LAN
 ┌───────────┐   wlan0    ┌────────────────┐   eth0    ┌──────────────────┐
 │192.168.4.1│◄──Wi-Fi────│ 192.168.4.200  │           │                  │
 │  :8266    │  (STA,     │ + DHCP on eth0 │◄──cable──►│  PC · HA · phones │
 │ PSK 12345678│  "the     │  reeftank-hub  │           │                  │
 └───────────┘   phone")  └────────────────┘           └──────────────────┘
```

- **wlan0 → the lamp's AP**: the Pi is the *only* client on that AP (its happy
  path — deterministic IP, no contention, we disable power-save on our side).
- **eth0 → the LAN**: wired, rock-solid, no Wi-Fi-STA-to-busy-router problems.
- The lamp never has to join the home router. Its Wi-Fi stays in the mode that
  actually works.

If the Pi is off, the lamp keeps running whatever **native schedule** we last
pushed to it (protocol cmd `0x1007`), so a Pi outage ≠ a dark tank.

---

## 2. Hardware

| Item | Choice | Notes |
|------|--------|-------|
| Board | Raspberry Pi 3B v1.2 (BCM2837, Cortex-A53 **arm64**) | already owned |
| Storage | **A1/A2 microSD, 16–32 GB**, SanDisk / Samsung | Pi 3B boots SD natively; USB-boot needs an OTP fuse and is unreliable on 3B |
| OS | **Raspberry Pi OS Lite (64-bit, Bookworm)** | headless; 64-bit → single `linux/arm64` build target |
| Power | 5 V 2.5 A official supply | 24/7 |
| Placement | within solid Wi-Fi range of the lamp | dedicated single-client link, but still 2.4 GHz on a PCB antenna |

### SD-card longevity

Writes from this service are tiny (schedule state every few minutes). Still:

- install **`log2ram`** — journald/logs live in RAM, flushed hourly
- state + profiles JSON is backed up daily to the LAN (rsync/scp target,
  configurable; e.g. into the HA config share)
- OS partition is otherwise read-mostly

---

## 3. Networking

Raspberry Pi OS Bookworm uses **NetworkManager**.

### wlan0 — the lamp link

```
SSID         K7_Pro42113
PSK          12345678
IPv4         static  192.168.4.200/24
gateway      (none)
DNS          (none)
never-default  yes        # nmcli ipv4.never-default yes — must NOT become the default route
autoconnect  yes, high priority
```

The `/24` on wlan0 automatically routes `192.168.4.0/24` (the lamp) out wlan0.

### eth0 — the LAN

```
IPv4         DHCP
             → recommend a DHCP reservation on the router so the Pi's LAN IP is stable
default route via eth0
```

### Firewall (nftables)

- **eth0**: allow the bridge UI/API port (`80`) and the raw proxy port (`8266`)
- **wlan0**: no inbound services — we only *originate* connections to the lamp
- no routing/NAT between the interfaces (this is a proxy, not a router)

### Sanity checks after setup

```bash
ip route                      # default dev eth0 ; 192.168.4.0/24 dev wlan0
ping -c1 192.168.4.1          # lamp, via wlan0
nc -vz 192.168.4.1 8266       # lamp protocol port
curl -s http://k7-pi-wifi-controller.local/api/config
```

---

## 4. Software architecture

**Language: Go.** Extend bitbarista's `pc-bridge`. Rationale: its
`internal/k7tcp` (271 lines) is a tested implementation of the lamp protocol, and
`internal/bridge` (1157 lines) already serves the shared web UI and the full
HTTP API the UI expects. A single static `linux/arm64` binary deploys by copy.

### Module layout in the fork

```
k7-led-controller/                     (fork of bitbarista/…)
├── pc-bridge/                          UNCHANGED upstream desktop bridge
│   └── internal/k7tcp/                 ← protocol, imported by pi-bridge
├── pi-bridge/                          NEW — this project (own go.mod)
│   ├── cmd/reeftank-hub/main.go
│   ├── internal/
│   │   ├── lamp/        thin wrapper over pc-bridge/internal/k7tcp + a mutex
│   │   ├── scheduler/   port of arduino/src/Effects.cpp  (the 24/7 engine)
│   │   ├── moon/         port of arduino/src/Moon.cpp
│   │   ├── httpapi/     LAN HTTP server: shared UI + /api/* + HA REST
│   │   ├── proxy/       raw TCP 8266 → lamp passthrough
│   │   ├── store/       state + profiles + config on disk
│   │   └── updater/     GitHub-release self-update
│   ├── deploy/          systemd units, install.sh, nmcli setup, log2ram
│   └── README.md
├── homeassistant/k7_lamp/              NEW — HA custom integration (REST client)
└── .github/workflows/pi-bridge.yml     NEW — cross-compile + release arm64 binary
```

`pi-bridge` imports `github.com/bitbarista/k7-led-controller/pc-bridge/internal/k7tcp`
via a `replace` directive (local path in the fork). `pc-bridge` itself is not
touched → a clean PR back to bitbarista is possible.

### Processes on the Pi

One binary, several goroutines:

| goroutine | job |
|-----------|-----|
| **scheduler** | every tick, compute target per-channel levels from the active schedule + smooth-ramp interpolation + lunar overlay + any active Feed/Maintenance override; push to the lamp (`0x1005` hand-luminance) when values change |
| **http** | serve `:80` on eth0 — shared UI, `/api/*`, HA REST |
| **proxy** | `:8266` on eth0 → forward raw bytes to `192.168.4.1:8266` (so the desktop PC Bridge, or any protocol client, can reach the lamp through the Pi) |
| **updater** | periodic + on-demand check of the fork's latest GitHub release |
| **lamp keeper** | owns the single TCP connection to the lamp behind a mutex; reconnect w/ backoff; periodic `syncTime` (GMT) and `readAll` |

Only **one** party may drive the lamp at a time (protocol has no locking). The
scheduler holds the lamp mutex; the HTTP API and proxy contend for the same
mutex. When "auto" is on, the scheduler wins; manual API pushes temporarily
suspend it (like the ESP32's behaviour).

---

## 5. The scheduling engine (port of `Effects.cpp`)

Feature parity targets with the ESP32 firmware:

| feature | source | keep? |
|---------|--------|-------|
| 24-slot hourly schedule | Effects.cpp | ✅ |
| Smooth Ramp (interpolate between hourly points, push every ~2 min) | Effects.cpp | ✅ |
| Lunar simulation (moon-phase night light, adjustable window) | Moon.cpp + Effects.cpp | ✅ |
| Feed Mode (timed white boost 1–60 min) | Effects.cpp | ✅ |
| Maintenance Mode (timed inspection light 1–180 min) | Effects.cpp | ✅ |
| Acclimation (multi-day progressive brightness) | Effects.cpp | ✅ |
| Seasonal Shift (photoperiod drift) | Effects.cpp | ✅ |
| Presets (Fish Only / LPS / SPS / Mixed …) | Presets.h | ✅ generated from `Presets.h` (there's already a `tools/generate_pc_bridge_presets.py`) |
| per-channel brightness caps | Effects.cpp / ApiServer.cpp | ✅ |

The engine is deterministic given (wall clock, config, overrides). Wall clock
from the Pi (NTP over eth0 — much better than the ESP32's guesswork). Sun/moon
math needs **latitude/longitude** (config); `Moon.cpp` is only 29 lines so the
port is trivial, sun times via a small Go SPA implementation or the `sampa`/
`sunrise` libs.

Write-wear note carried over: default Smooth Ramp **off**; when on, only push on
change, min 2-minute cadence (configurable).

---

## 6. HTTP API & web UI

- Serve the **unmodified** `shared-ui/index.html` + `mobile.html` (embed at build
  time, same as pc-bridge does).
- Implement the same `/api/*` contract as `pc-bridge/internal/bridge/server.go`
  (`/api/capabilities`, `/api/config`, `/api/presets`, `/api/lamp/read`,
  `/api/state`, `/api/profiles`, `/api/backup`, `/api/preview`, `/api/hand`,
  `/api/push`, …) **plus** the always-on endpoints the ESP32's `ApiServer.cpp`
  adds (feed, maintenance, effect configs, logs).
- **Flip the capability flags on**: unlike the PC bridge, this box *is* always
  on, so `smooth_ramp`, `acclimation`, `seasonal_shift`, `feed_mode`,
  `maintenance_mode`, `lunar`, `logs` are all advertised → the shared UI shows
  the full control set with no code change.
- Bind to `0.0.0.0:80` (not localhost). Configurable via `listen`; the systemd
  unit grants `CAP_NET_BIND_SERVICE` for the sub-1024 port.

---

## 7. Home Assistant integration (REST, deferred — after M2)

Decision: **REST + a small custom integration**, not MQTT.

### Daemon side

A stable read/write JSON surface (subset, versioned):

```
GET  /api/ha/state      → {connected, auto, channels:[…6], preset, active_effect,
                            feed:{active,ends_at}, maintenance:{…}, moon_phase, …}
POST /api/ha/auto       {enabled:bool}
POST /api/ha/channels   {channels:[…6]}          # manual override, 0–255 or 0–100%
POST /api/ha/preset     {id:"mixed"}
POST /api/ha/feed       {minutes:10}
POST /api/ha/maintenance{minutes:30}
```

### HA side — `homeassistant/k7_lamp/` custom integration

Minimal, polls `/api/ha/state` (~10 s), thin:

| entity | type |
|--------|------|
| `light.k7_pro` | master on/off + brightness (scales all channels) |
| `number.k7_royal_blue` … ×6 | per-channel 0–100 % |
| `switch.k7_auto_schedule` | scheduler on/off |
| `select.k7_preset` | preset picker |
| `button.k7_feed` / `button.k7_maintenance` | timed modes (default durations; configurable) |
| `binary_sensor.k7_lamp_connected` | lamp link health |
| `sensor.k7_moon_phase` | 0–100 % |

Config: single host field (the Pi's LAN IP). Installed into
`D:\HomeAssistant\custom_components\k7_lamp\` (HACS-compatible layout so it can be
published later).

---

## 8. Online updates (OTA) — "download & install only"

**No compiling on the Pi.** Build in CI, ship a binary, self-replace.

### Build (fork's GitHub Actions — `.github/workflows/pi-bridge.yml`)

On a version tag (`hub-v*`):

1. `GOOS=linux GOARCH=arm64 go build ./pi-bridge/cmd/reeftank-hub`
2. produce `reeftank-hub-linux-arm64`, `SHA256SUMS`, `version.json`
   (`{version, min_os, notes}`)
3. attach as **GitHub Release assets** on the fork

### Pi side — `internal/updater`

```
/opt/reeftank-hub/
├── releases/
│   ├── hub-v1.0.0/reeftank-hub
│   └── hub-v1.1.0/reeftank-hub
├── current  → releases/hub-v1.1.0        (symlink)
└── data/    (state, profiles, config)   — never touched by updates
```

- checks `https://api.github.com/repos/<you>/k7-led-controller/releases/latest`
- if newer than running version: download asset → verify SHA256 against
  `SHA256SUMS` → unpack into `releases/<tag>/` → flip `current` symlink →
  `systemctl restart reeftank-hub`
- **atomic** (symlink swap) and **rollback-able** (previous release dir kept; a
  failed health check after restart reverts the symlink)
- trigger: hourly timer **and** a `POST /api/update/check` + `POST /api/update/apply`
  exposed as an "Updates" panel in the diagnostic UI
- transport security: HTTPS to GitHub + SHA256 from the same release. (Optional
  later: `minisign` signature with a public key baked into the binary.)

systemd runs `/opt/reeftank-hub/current/reeftank-hub`; the updater only needs
write access to `/opt/reeftank-hub` and permission to restart its own unit
(`systemctl` polkit rule or `Type=notify` + `RuntimeDirectory`).

---

## 9. Deployment

`pi-bridge/deploy/install.sh` (run once over SSH, idempotent):

1. `apt install -y log2ram nftables` (NetworkManager already present)
2. write NetworkManager connections for wlan0 (static, never-default) + eth0
3. create `/opt/reeftank-hub/{releases,data}`, `reefhub` system user
4. fetch the latest release binary (same path as the updater)
5. install `reeftank-hub.service` + `reeftank-hub-update.timer`
6. install nftables ruleset
7. `systemctl enable --now reeftank-hub`

Config file `/opt/reeftank-hub/data/config.toml`:

```toml
lamp_host   = "192.168.4.1"
lamp_port   = 8266
listen      = ":80"
proxy       = ":8266"
latitude    = 22.63           # Kaohsiung — for sun/moon
longitude   = 120.30
timezone    = "Asia/Taipei"
smooth_ramp = false
update_repo = "cp296944/reeftank-hub"
update_channel = "stable"
backup_target  = ""           # off for now
```

---

## 10. Milestones

You want a **minimal v1 deployed first, then every later feature delivered by
OTA**. So the OTA mechanism ships *inside* v1 — before the big feature work.

| version | Deliverable | Definition of done |
|---------|-------------|--------------------|
| **v1.0 (M1 + OTA)** | Pi networking · bridge daemon · shared UI on the LAN · raw `:8266` proxy · **self-update client baked in** · CI release pipeline · `install.sh` | From a LAN PC: `http://k7-pi-wifi-controller.local/` loads the UI; Read / Preview / manual output / push-native-schedule all work; `nc <pi> 8266` reaches the lamp; survives reboot. Tagging `hub-v1.0.1` in the fork → the Pi picks it up within the hour. |
| **v1.1 (M2)** — via OTA | Scheduling engine | Auto mode drives the lamp on a 24-slot schedule; Smooth Ramp, lunar, Feed, Maintenance work from the UI; presets from `Presets.h`. Unattended for a week. |
| **v1.2 (M3)** — via OTA | HA integration | `custom_components/k7_lamp` gives the entities in §7; toggling `light.k7_pro` changes the tank. |
| later — via OTA | acclimation, seasonal shift, backups, polish | |

v1.0 already does everything the upstream desktop `pc-bridge` does (read,
preview, manual, profiles, backups, **push native schedule** so the lamp runs a
day/night curve on its own). The always-on engine is the v1.1 add.

---

## 11. What you need to do (not automatable from here)

1. Flash **Raspberry Pi OS Lite 64-bit** to the microSD (Raspberry Pi Imager —
   set hostname, enable SSH, Wi-Fi country, your LAN user in the imager).
2. First boot on **Ethernet**, confirm SSH in, give me the way in
   (`ssh k7pi@<ip>`), or run the scripts yourself.
3. **Fork** `bitbarista/k7-led-controller` to your GitHub (`cp296944`) — the OTA
   build + release lives there.
4. Confirm the K7 Pro AP SSID is exactly `K7_Pro42113` and the Pi's spot has
   decent signal to it.
5. Decide the backup target for the daily state/profile copy (or skip).

---

## 12. Decisions (confirmed 2026-09-07)

| item | value |
|------|-------|
| hostname / mDNS | **`k7-pi-wifi-controller`** → `k7-pi-wifi-controller.local` (hyphens, not underscores — underscores are invalid in hostnames) |
| Linux login user | **`k7pi`** |
| GitHub fork | **`cp296944/reeftank-hub`** (fork renamed on GitHub; `pi-bridge` imports `k7tcp` by local `replace`, so the rename doesn't matter) |
| location | **Kaohsiung** — `latitude = 22.63`, `longitude = 120.30`, `timezone = "Asia/Taipei"` |
| web port | **`:80`** — see below |
| daily backup | **off** for now (`backup_target = ""`), revisit later |
| rollout | minimal **v1.0 first**, all later features via OTA (§10) |

### Why `:80`

A web server listens on a numbered TCP *port*. Browsers assume **80** for
`http://`, so:

- on **:80**  → you open `http://k7-pi-wifi-controller.local/`  (no port to type)
- on **:8787** → you open `http://k7-pi-wifi-controller.local:8787/`

`:80` costs one extra line of setup (Linux won't let a non-root program use ports
below 1024 without `CAP_NET_BIND_SERVICE`, which the systemd unit grants). Since
this Pi is dedicated to the K7 bridge and nothing else will want :80, **:80 is
the nicer choice** — clean URL, easy to remember. `config.toml` keeps it
changeable (`listen = ":80"`).

## 13. What you do (not automatable from here)

1. Flash **Raspberry Pi OS Lite 64-bit** with Raspberry Pi Imager. In the imager
   gear/⚙ settings: hostname `k7-pi-wifi-controller`, enable SSH (password or your
   key), username `k7pi`, set Wi-Fi country = TW (leave Wi-Fi network blank — we
   configure wlan0 later), locale Asia/Taipei.
2. Boot the Pi on **Ethernet**, confirm `ssh k7pi@k7-pi-wifi-controller.local`
   works, tell me you're in (or run the scripts yourself).
3. **Fork** `bitbarista/k7-led-controller` on GitHub, rename it to
   `k7-led-Raspberry-controller`.
4. Confirm the K7 Pro AP is `K7_Pro42113` and the Pi's location has decent
   signal to it.
