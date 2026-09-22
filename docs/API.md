# K7 controller HTTP API — contract

The shared web UI (`shared-ui/index.html`) drives every K7 controller — ESP32
firmware, desktop `pc-bridge`, and this `pi-bridge` — through one HTTP API. The
UI reads `GET /api/capabilities` and shows/hides feature panels by 18 boolean
flags. **"1:1 with bitbarista" = advertise all 18 `true` and implement every
endpoint behind them.**

Sources reconciled: `arduino/src/ApiServer.cpp` (35 routes),
`pc-bridge/internal/bridge/server.go` (21 routes), and the `fetch()` calls in
`shared-ui/index.html` / `mobile.html` (37 paths).

Legend: **F** free (pc-bridge already implements) · **P2/P3/P4** pi-bridge phase.

## Meta

| Endpoint | Method | Phase | Notes |
|---|---|---|---|
| `/api/version` | GET | F | `{bridge, platform, transport, firmware}` |
| `/api/capabilities` | GET | F→P3 | `{platform, transport, capabilities:{…18 bools}}` |
| `/api/config` | GET/POST | F | `{host, port, device}` — lamp connection |
| `/api/devices` | GET | F | static per-model channel names |
| `/api/state` | GET | F | local (non-lamp) state snapshot + `first_run` |

## Lamp I/O

| Endpoint | Method | Phase | Notes |
|---|---|---|---|
| `/api/lamp/read` | GET/POST | F | live TCP `readAll` (0x1008) → `LampState`, persists to store |
| `/api/preview` | POST | F | `{channels:[6]}` → 0x1006 fire-and-forget |
| `/api/hand` | POST | F | `{channels:[6]}` → 0x1005 with ack |
| `/api/push` | POST | F | push native 24-slot schedule → 0x1007 (+ mode, +time) |
| `/api/master` | GET/POST | F | `{value}` 0–200 master brightness scalar |

## Presets / profiles / backup

| Endpoint | Method | Phase | Notes |
|---|---|---|---|
| `/api/presets` | GET | F | built-ins, generated from `arduino/src/Presets.h` |
| `/api/community-presets` | GET | F | proxied fetch of the community list |
| `/api/profiles` | GET/POST | F | list / save named profile (local) |
| `/api/profiles/{name}` | GET/DELETE | F | one profile |
| `/api/backup` | GET/POST | F | export / import full store |

## Siesta + fixed lunar (free)

| Endpoint | Method | Phase | Notes |
|---|---|---|---|
| `/api/siesta/status` | GET | F | baked-into-schedule midday dip |
| `/api/siesta/schedule` | POST | F | apply siesta to the working schedule |
| `/api/lunar/status` | GET | F | current phase/illumination |
| `/api/lunar/schedule` | GET/POST | F | fixed lunar night-window config |
| `/api/lunar/start` `/api/lunar/stop` | POST | F/P3 | fixed on pc-bridge; **tracked** needs P3 engine |

## Always-on engine — pi-bridge Phase 3 (the 9 flags to earn)

| Endpoint | Method | Phase | Capability |
|---|---|---|---|
| `/api/ramp/start` `/stop` `/status` `/tick` | POST/GET | P3 | `smooth_ramp` |
| `/api/feed/start` `/stop` `/status` | POST/GET | P3 | `feed_mode` |
| `/api/maintenance/start` `/stop` `/status` | POST/GET | P3 | `maintenance_mode` |
| `/api/acclimation/config` `/status` | GET/POST | P3 | `acclimation` |
| `/api/seasonal/config` `/status` | GET/POST | P3 | `seasonal_daylength` |
| `/api/time` | GET/POST | P3 | `persistent_controller_clock` |
| `/api/output/status` | GET | P3 | live computed channel output (also the golden-vector probe) |
| `/api/wifi/signal` | GET | P3 | link RSSI/quality |
| `/api/warnings/status` | GET | P3/P4 | live warnings feed (clock, lamp link, Wi-Fi, dark schedule, failed write) — done hub-v0.9.10 |
| `/api/logs` | GET | P3 | ring-buffer log (`logs` capability) |
| `/api/diag` | GET | P4 | soak-test snapshot (RSS, heap, goroutines, GC, lamp-op health, today's writes) + tail of `data/soak.log` (hourly). `?lines=N`. hub-v1.0.1 |

`/api/output/status` `writes_today:{auto,manual,date}` is now backed by
`data/writes.json` — it survives a restart / OTA within the same local day
(hub-v1.0.1). `/api/update/apply` needs `{confirm:true}`; `tag` is an advisory
hint — if a newer release landed since, that newer one is installed and the
response's `applying` names it.

## Setup (`setup_portal`) — done hub-v1.0.0

The ESP32's Wi-Fi AP onboarding has no shared-UI panel; on pi-bridge it becomes
a settings modal (⚙ in the top bar) over these endpoints:

| Endpoint | Method | Notes |
|---|---|---|
| `/api/setup` | GET | aggregate: `{lamp, location, update, wifi, system}` |
| `/api/setup` | POST | `{location:{latitude,longitude,timezone}, update:{channel}}` → `config.json`; returns `{ok, restart_required:[…]}` (timezone / channel need a restart) |
| `/api/setup/restart` | POST | `{confirm:true}` → `systemctl restart reeftank-hub` |
| `/api/setup/factory-reset` | POST | `{confirm:true}` → wipe `store.json` + `effects.json` + `profiles/` (keeps `config.json`), then restart |

Lamp host/port/device stay on `/api/config` (GET/POST), as upstream.

## Capability flags (the ledger) — **18/18 as of hub-v1.0.0**

```
read_lamp  push_schedule  manual_preview  profiles  community_presets
community_presets_browse  backup_restore  fixed_lunar  siesta_baked_schedule
smooth_ramp  tracked_lunar  acclimation  seasonal_daylength  feed_mode
maintenance_mode  logs  persistent_controller_clock  setup_portal
```

`tools/parity_check.py --ref <esp32> --new <pi-bridge>` diffs the read endpoints
and is the objective done-check per phase.
