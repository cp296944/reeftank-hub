# K7 Pi Bridge — Development Plan

Companion to [DESIGN.md](DESIGN.md) (architecture). This document is the
**process**: what "1:1" means measurably, in what order we build, and how we
prove it.

Requirements from you:

1. **Online update (OTA)** — in from the start.
2. **1:1 feature parity** with everything
   [bitbarista/k7-led-controller](https://github.com/bitbarista/k7-led-controller)
   does.

---

## 0. "1:1" — made measurable

The shared web UI (`shared-ui/index.html`, 149 KB) is **capability-driven**: it
asks `GET /api/capabilities` and shows or hides whole feature panels based on 18
boolean flags. The ESP32 firmware sets all 18 to `true`; the desktop `pc-bridge`
sets 9 to `false` and the UI hides those controls.

**So "1:1" is not a vague goal. It is: 18/18 capabilities `true`, and every
endpoint behind them implemented.** When we flip the last flag, the same
unmodified HTML renders the complete ESP32 feature set. **Zero frontend work.**

### Capability ledger

| # | capability | pc-bridge (Go) | ESP32 | we must build |
|---|-----------|:---:|:---:|:---:|
| 1 | `read_lamp` | ✅ | ✅ | — |
| 2 | `push_schedule` | ✅ | ✅ | — |
| 3 | `manual_preview` | ✅ | ✅ | — |
| 4 | `profiles` | ✅ | ✅ | — |
| 5 | `community_presets` | ✅ | ✅ | — |
| 6 | `community_presets_browse` | ✅ | ✅ | — |
| 7 | `backup_restore` | ✅ | ✅ | — |
| 8 | `fixed_lunar` | ✅ | ✅ | — |
| 9 | `siesta_baked_schedule` | ✅ | ✅ | — |
| 10 | `smooth_ramp` | ❌ | ✅ | **●** |
| 11 | `tracked_lunar` | ❌ | ✅ | **●** |
| 12 | `acclimation` | ❌ | ✅ | **●** |
| 13 | `seasonal_daylength` | ❌ | ✅ | **●** |
| 14 | `feed_mode` | ❌ | ✅ | **●** |
| 15 | `maintenance_mode` | ❌ | ✅ | **●** |
| 16 | `logs` | ❌ | ✅ | **●** |
| 17 | `persistent_controller_clock` | ❌ | ✅ | **●** |
| 18 | `setup_portal` | ❌ | ✅ | **◐** adapt |

**9 free. 9 to build.** (`setup_portal` is the one that isn't a literal port —
the ESP32's Wi-Fi AP onboarding becomes a Pi network/lamp settings page.)

### Endpoint ledger

- shared UI references **37** `/api/*` paths
- `pc-bridge` implements **21**
- ESP32 `ApiServer.cpp` implements **35**

**Delta = the ~19 endpoints we add**, and they map 1:1 onto the 9 missing
capabilities:

| capability | endpoints to add |
|---|---|
| `smooth_ramp` | `/api/ramp/start` `/api/ramp/stop` `/api/ramp/status` `/api/ramp/tick` |
| `feed_mode` | `/api/feed/start` `/api/feed/stop` `/api/feed/status` |
| `maintenance_mode` | `/api/maintenance/start` `/api/maintenance/stop` `/api/maintenance/status` |
| `acclimation` | `/api/acclimation/config` `/api/acclimation/status` |
| `seasonal_daylength` | `/api/seasonal/config` `/api/seasonal/status` |
| `tracked_lunar` | (extends existing `/api/lunar/*`) |
| `persistent_controller_clock` | `/api/time` |
| `logs` | `/api/logs` `/api/warnings/status` |
| runtime state | `/api/output/status` `/api/wifi/signal` |

This ledger is the **definition of done**. It goes in CI as a checklist test.

---

## 1. Three leverage points

Everything below is designed around these:

**① Reuse, don't rewrite.**
`pc-bridge` is a standalone Go module that already gives us 21 endpoints, 9
capabilities, the tested lamp protocol (`internal/k7tcp`, 271 lines) and the
embedded shared UI. We import it and extend. We never touch it → the fork stays
mergeable with upstream, and a PR back to bitbarista stays possible.

**② The ESP32 on your bench is the reference oracle.**
We already have bitbarista's firmware compiled, flashed and talking to your real
K7 Pro. **Do not reflash it back to ESPHome.** It is the ground truth: for every
endpoint we implement, we hit the same URL on the ESP32 and on pi-bridge and diff
the JSON. That is the strongest possible parity proof — better than reading C++.

**③ The mock lamp removes all hardware blocking.**
`tools/mock_k7pro_lamp.py` in the repo is a lamp simulator. We develop and test
the entire protocol layer, effects engine, and API on **Windows**, with no Pi and
no lamp powered on. It also runs in CI.

---

## 2. OTA is the delivery pipeline, not just a feature

You asked for online update up front. The reason it must be **Phase 1** is
bigger than the end-user feature:

> Once OTA works, every subsequent increment reaches the Pi by `git tag`.
> No SD card removal, no `scp`, no manual restarts — for the rest of the project.

So the build order is deliberately: **thin daemon + OTA + CI first**, then pour
features in through the pipe we just built. During development we ship
`hub-v0.2`, `hub-v0.3`, … to the Pi automatically; you get `v1.0` when the
capability ledger reads 18/18.

---

## 3. Phases

Each phase has a hard exit criterion. Nothing proceeds until the previous one is
green.

### Phase 0 — Foundation (dev machine only, ~no risk)

| step | detail |
|---|---|
| 0.1 | Fork `bitbarista/k7-led-controller` → rename `cp296944/reeftank-hub`; clone to `D:\HomeAssistant\K7\` (full clone, replacing the shallow one); `git remote add upstream` the original |
| 0.2 | Install Go on Windows; verify `GOOS=linux GOARCH=arm64 go build` cross-compiles |
| 0.3 | Run `tools/mock_k7pro_lamp.py`; point `pc-bridge` at it; confirm the shared UI loads and Read works entirely offline |
| 0.4 | Write `pi-bridge/docs/API.md` — the extracted 37-endpoint contract with request/response shapes, from `ApiServer.cpp` + `server.go` + the UI's own `fetch()` calls |
| 0.5 | Write `tools/parity_check.py` — hits a list of endpoints on two base URLs (ESP32 vs pi-bridge) and diffs normalised JSON |
| 0.6 | Scaffold `pi-bridge/` Go module with `replace` → `../pc-bridge` |

**Exit:** `parity_check.py` runs against the ESP32 and prints a 35-row report
(all "missing on pi-bridge" — that's the burndown list).

### Phase 1 — Pi base + OTA  → tag `hub-v0.1`

| step | detail |
|---|---|
| 1.1 | Flash Pi OS Lite 64-bit; hostname `k7-pi-wifi-controller`, user `k7pi`, SSH on |
| 1.2 | `deploy/setup-network.sh` — NetworkManager: wlan0 → `K7_Pro42113` static `192.168.4.200/24` `never-default`; eth0 DHCP; nftables |
| 1.3 | Minimal daemon: `/api/version`, `/healthz`, and `internal/updater` |
| 1.4 | `.github/workflows/pi-bridge.yml` — on tag `hub-v*`: cross-compile arm64, emit binary + `SHA256SUMS`, publish GitHub Release |
| 1.5 | `deploy/install.sh` + `reeftank-hub.service` + update timer; `/opt/reeftank-hub/{releases,current,data}` symlink scheme |
| 1.6 | Prove the loop: tag `hub-v0.2` on the fork → Pi self-updates within the hour → `/api/version` reports `0.2` |

**Exit:** you tag a release on GitHub and the Pi upgrades itself, unattended,
with rollback on failed health check. **From here nobody touches the SD card.**

### Phase 2 — Lamp link + read path  → `hub-v0.3`

| step | detail |
|---|---|
| 2.1 | `internal/lamp` — single TCP connection to `192.168.4.1:8266` behind a mutex, reconnect with backoff, periodic `syncTime` (GMT) |
| 2.2 | `internal/store` — config / state / profiles / backups as JSON in `/opt/reeftank-hub/data` |
| 2.3 | `internal/proxy` — raw `:8266` passthrough on eth0 (so the desktop PC Bridge can also drive the lamp through the Pi) |
| 2.4 | Serve `shared-ui` embedded; wire the 21 pc-bridge endpoints |
| 2.5 | Presets generated from `arduino/src/Presets.h` via their existing `tools/generate_pc_bridge_presets.py` |

**Exit:** `http://k7-pi-wifi-controller.local/` from a LAN PC loads the UI; Read /
Preview / manual / push-native-schedule all work against the real lamp;
`parity_check.py` green on those 21 endpoints. **9/18 capabilities.**

### Phase 3 — The always-on engine  → `hub-v0.4` … `hub-v0.9`

The bulk: port `arduino/src/Effects.cpp` (1073 lines C++) and `Moon.cpp` to Go.
One capability per tag, each shipped to the Pi by OTA and verified before the
next starts:

| tag | capability | notes |
|---|---|---|
| `hub-v0.4` | `persistent_controller_clock` + `/api/time` + `logs` | groundwork: the scheduler tick, ring-buffer log, `/api/output/status`, `/api/wifi/signal` |
| `hub-v0.5` | `smooth_ramp` | interpolate between hourly slots; push `0x1005` only on change, ≥2 min cadence, default **off** (write-wear) |
| `hub-v0.6` | `feed_mode` + `maintenance_mode` | timed overrides (1–60 / 1–180 min) that suspend and restore the scheduler |
| `hub-v0.7` | `tracked_lunar` | real moon phase for 22.63 N / 120.30 E, night window overlay |
| `hub-v0.8` | `acclimation` | multi-day progressive ramp; state survives restart |
| `hub-v0.9` | `seasonal_daylength` | photoperiod drift |

**Exit:** 17/18 capabilities; the UI shows every ESP32 control and they all work.

### Phase 4 — Setup page + hardening  → `hub-v1.0`

| step | detail |
|---|---|
| 4.1 | `setup_portal` equivalent: a settings page for lamp SSID/IP, Wi-Fi status, factory reset, update channel |
| 4.2 | `/api/warnings/status`; diagnostics page; lamp-link health |
| 4.3 | Soak test: 7 days unattended; verify no lamp hammering, no memory growth, clean reconnect after lamp power-cycle |
| 4.4 | `README.md`, and a PR back to bitbarista offering `pi-bridge/` upstream |

**Exit: 18/18 capabilities. `hub-v1.0`.** This is your "初版 with everything".

### Phase 5 — Home Assistant (`hub-v1.1`, via OTA)

Per DESIGN.md §7: `/api/ha/*` REST surface + `custom_components/k7_lamp`.
Not part of the 1:1 goal — it is *beyond* what bitbarista offers.

---

## 4. How we prove parity (the verification harness)

Three independent checks, all automated:

**① Endpoint diff — `tools/parity_check.py`**
Runs the same request against the ESP32 and pi-bridge, normalises volatile
fields (uptime, timestamps, IP), diffs the JSON. Run after every phase.

**② Golden vectors for the effects engine**
The scheduler is a pure function of `(wall clock, config, overrides) → 6 channel
values`. We capture the ESP32's output for a set of fixed inputs (via
`/api/output/status` while stepping its clock) and assert the Go engine produces
byte-identical values. This is how we prove Smooth Ramp / lunar / acclimation are
*actually* 1:1 and not just "looks similar".

**③ Mock lamp in CI**
`tools/mock_k7pro_lamp.py` runs as a service in GitHub Actions; integration tests
drive the full API against it on every push. No hardware needed to catch
regressions.

Plus a **capability-ledger test**: CI parses `arduino/src/ApiServer.cpp` for the
`caps[...]` keys and fails if `pi-bridge` advertises fewer. Upstream can't add a
feature without us noticing.

---

## 5. Staying 1:1 as upstream moves

bitbarista is active (355 commits). A fork that drifts stops being 1:1.

- `git remote add upstream https://github.com/bitbarista/k7-led-controller`
- monthly (or on upstream release): `git fetch upstream && git merge upstream/master`
- because we **never modify** `pc-bridge/`, `shared-ui/`, `arduino/`, or
  `tools/`, these merges are conflict-free by construction — all our code is in
  `pi-bridge/` and `.github/workflows/pi-bridge.yml`
- the capability-ledger CI test fails loudly if upstream adds a 19th flag
- re-run `generate_pc_bridge_presets.py` after merges so presets stay in sync

**This is why "extend, never fork-and-edit" was the right architectural call.**

---

## 6. Risks & mitigations

| risk | mitigation |
|---|---|
| Effects engine subtly wrong → bad light cycle for livestock | golden-vector tests; `dry_run` mode logs intended pushes without sending; Smooth Ramp off by default; the lamp keeps its own native schedule as fallback |
| Lamp write-wear from frequent `0x1005` | push only on change; min cadence configurable; default hourly, not 2-min |
| Pi dies / SD corrupts | lamp still runs the last pushed native schedule (`0x1007`) → tank stays lit; `log2ram`; config backup (currently off — enable later) |
| Bad OTA bricks the service | versioned release dirs + symlink swap + health check + automatic rollback to previous |
| Pi Wi-Fi range to the lamp marginal | verify RSSI in Phase 1 before building anything on top; the Pi can move, it's only carrying one link |
| Two controllers fighting over the lamp | the lamp protocol has no locking — **retire the ESP32 controller from the tank** once pi-bridge takes over (keep it on the bench as the parity oracle only) |
| Scope: ~2500–3500 lines of Go | phased tags; every phase independently useful and shipped |

---

## 7. What I need from you to start Phase 0/1

1. Fork + rename on GitHub (`cp296944/reeftank-hub`), tell me when
   it exists.
2. Flash the microSD (Pi OS Lite **64-bit**), hostname `k7-pi-wifi-controller`,
   user `k7pi`, SSH enabled, Wi-Fi country TW.
3. Boot on Ethernet, confirm SSH, and tell me how I reach it.
4. **Keep the ESP32 flashed with the K7 firmware** — it is the parity oracle.
5. Confirm: Go installed on this Windows box, or shall I set that up?

Then Phase 0 is all dev-machine work and I can run most of it myself.
