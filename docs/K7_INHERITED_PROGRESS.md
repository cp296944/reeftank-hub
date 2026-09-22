# K7 Pi Bridge — Build Progress (living handoff)

**If you are a resumed/scheduled session: read this whole file, then
[PLAN.md](PLAN.md) and [DESIGN.md](DESIGN.md). Execute the NEXT unchecked
step(s) only. Commit + push after each. Update this file. If blocked, write the
blocker under "BLOCKED" and stop.**

## Hard rules for any session working this

- Work only on branch `dev/pi-bridge` in `D:\HomeAssistant\K7\K7_Pi_Wifi_Controller`.
  (Renamed from `k7-led-controller` in hub-v0.5.5. ESP32 flash scripts are in the
  sibling `..\esp32-flash-experiment\`.) Never commit to `master`. Never `git push --force`.
- Git identity is repo-local: `cp296944` / `cp296944@gmail.com` (already set).
- `gh` CLI is authed as `cp296944` — pushing works.
- Go: `C:\Program Files\Go\bin\go.exe` (1.27.0). arm64 cross-compile verified.
- The Pi: `ssh -i %USERPROFILE%\.ssh\id_ed25519_k7pi k7pi@192.168.0.149`
  (DHCP-reserved). sudo is NOPASSWD. It is dual-homed: eth0 `192.168.0.149`
  (LAN), wlan0 `192.168.4.2` (K7 AP). **The K7 lamp answers at
  `192.168.4.1:8266` and this was verified end-to-end.** Wi-Fi signal is weak
  right now (Pi far from tank) — user will move it later; do NOT tune timeouts
  around the current bad signal, just make them generous + retrying.
- NEVER touch eth0 / sshd config on the Pi (lock-out risk). wlan0 changes only.
- Do not reflash the user's ESP32 — it is the parity oracle.
- Keep upstream files untouched (`pc-bridge/`, `shared-ui/`, `arduino/`,
  `tools/` except NEW files) so `git merge upstream/master` stays clean.
- The shared UI is capability-driven: "1:1" = all 18 capability flags `true`
  and their endpoints implemented. See PLAN.md §0 for the ledger.
- **Every change that diverges from upstream bitbarista (a modified upstream
  file, a new user-visible behaviour, a platform-scope decision) MUST be added
  to the "Exactly what diverges from upstream" section of the top-level
  `README.md` in the same commit.**
- Ship target is **`linux/arm64` only** (all 64-bit Pis). 32-bit Pi models are
  explicitly out of scope (user decision 2026-09-08). Do not add GOARM builds.

## Key facts already established

- Protocol: TCP `192.168.4.1:8266`, frame `AA A5 <cmd hi> <cmd lo> <data> BB`,
  6 channels 0-255, 24 slots. Reference impl:
  `pc-bridge/internal/k7tcp/client.go` (vendored into `pi-bridge/`).
- `pi-bridge/` cannot import `pc-bridge/internal/*` (Go internal rule, separate
  module). Decision: **vendor** `k7tcp/client.go` into
  `pi-bridge/internal/k7tcp/` with a provenance header + a CI sync check.
- The 21 "free" endpoints in `pc-bridge/internal/bridge/server.go` are
  reimplemented in `pi-bridge` (we need all-caps-true + real scheduler + a
  different store anyway).
- Live K7 Pro readAll decoded: 6ch = 50/50/50/50/50/50, 24 slots, name
  `K7_Pro42113`, device `k7pro`.

---

## Milestone checklist

### ReefTank Hub expansion — ACTIVE (2026-09-22)

- [x] User decisions captured in `D:\HomeAssistant\REEFTANK\REEFTANK_HUB_DEVELOPMENT_PLAN.md`: Pi is the aggregate/history/UI tier; HA retains device management, automation and alerts; Google Sheets remains water-quality authority; Pi and HA poll temperature independently; three linked K7 lamps are one control target.
- [x] Real Pi read-only baseline: `192.168.0.149` healthy on `hub-v1.0.4`, stable channel, auto-update off, up to date before Hub work.
- [x] Embedded Hub shell: `/` overview, `/K7/` compatibility entry, stable `/dosing/`, `/power/`, `/water/`, `/system/` routes. Legacy `/static/*` and K7 `/api/*` preserved.
- [x] `GET /api/hub/status` and read-only `GET /api/bootstrap/status`.
- [x] Sudo-only bootstrap installer foundation: idempotent managed files, pre-change backups, helper copy, exact maintenance systemd unit, narrow polkit start rule, BlueZ install/enable, atomic marker.
- [x] New tests pass; existing command/httpapi/piweb tests pass; linux/arm64 cross-build passes. Full Windows `go test ./...` still has the pre-existing updater symlink privilege failure; CI target is Linux.
- [ ] Maintenance request validator and root helper apply flow (do not expose install/apply over HTTP).
- [x] Persistent 18-outlet logical equipment map and API. Current user-facing names are seeded independently of HA entity IDs; assignment to an occupied entity swaps the two devices. `/power/` includes the responsive mapping editor and critical-device labels.
- [ ] HA WebSocket client and live entity-state cache.
- [ ] Local time-series database.
- [ ] Google Sheets mirror and independent Xiaoyu temperature poller.
- [ ] Dosing domain model + simulator + full UI before real BLE verification.


### Phase 0 — Foundation
- [x] Fork cloned to `D:\HomeAssistant\K7\k7-led-controller`, branch `dev/pi-bridge` pushed, `upstream` remote added
- [x] Actions workflow token perms → write (for OTA releases)
- [x] Go 1.27 verified, arm64 cross-compile verified
- [x] `pi-bridge/` Go module scaffold (go.mod zero-dep, cmd/reeftank-hub/main.go serves /healthz + /api/version, internal/{config,version} done, other internal/ dirs stubbed)
- [x] Vendor `k7tcp/client.go` → `pi-bridge/internal/k7tcp/` + provenance header + UPSTREAM_SHA
- [x] `pi-bridge/docs/API.md` — endpoint/capability ledger (compact; per-endpoint shapes filled in as Phase 2/3 implements them)
- [x] `tools/parity_check.py` — diff endpoint JSON between two base URLs
- [x] `tools/check_k7tcp_sync.py` — CI guard that vendored k7tcp matches upstream (passing)
- note: `go vet ./...` trips on the vendored upstream file (IPv6 `%s:%d` nit); CI vets our packages only

### Phase 1 — Pi base + OTA  ✅ DONE (tags `hub-v0.1.0`, `hub-v0.2.0`)
- [x] `internal/config` — JSON file < env < flags, zero-dep (config.toml → config.json)
- [x] minimal daemon: `/api/version`, `/healthz`, `/api/capabilities`, `/api/update/{status,apply}`, slog
- [x] `internal/updater` — GitHub releases API, semver pick, SHA256 verify, atomic symlink swap, restart, ConfirmAfterStart; unit tests
- [x] `.github/workflows/pi-bridge.yml` — check job (sync/vet/test/build) + release job on `hub-v*` (arm64, ldflags stamp, SHA256SUMS+version.json, `gh release create`)
- [x] `pi-bridge/deploy/` — install.sh (idempotent, --binary bootstrap), uninstall.sh, reeftank-hub.service (reefhub user, CAP_NET_BIND_SERVICE, OnFailure), reeftank-hub-rollback.service + rollback.sh (loop-guarded), polkit rule
- [x] deployed to Pi (`192.168.0.149:80`), reachable from Windows LAN
- [x] **exit gate PASSED on real hardware:** tagged hub-v0.1.0→hub-v0.2.0, `POST /api/update/apply` → Pi downloaded+verified+swapped+restarted → `/api/version` = hub-v0.2.0. Rollback drill: broken release → crash-loop → systemd OnFailure → rollback.sh reverted to hub-v0.2.0, service healthy.
- NOTE: routing NOT modified (eth0 wins by metric 100<600); wlan0 never-default deferred. Deploy never touches eth0/sshd.
- NOTE: install.sh cosmetic bug — "LAN UI" line prints `http:/…/24` (harmless)

**Pi is currently running hub-v0.2.0 as a systemd service.**

### Phase 2 — Lamp link + read path  ✅ DONE (tag `hub-v0.3.0`)
- [x] `internal/httpapi` — vendored+adapted `pc-bridge/internal/bridge/server.go` (+66 lines, header-documented). Embeds shared-ui, serves `/` + all 21 upstream endpoints. `New(Options)` injects identity + the 18 caps; `DefaultCapabilities()` = 9 true / 9 false; `SetCapability()` for Phase 3.
- [x] `internal/proxy` — raw `:8266` passthrough (one client at a time, idle timeout, optional scheduler gate)
- [x] presets served from vendored `presets.json` (generated from `Presets.h`)
- [x] `tools/check_httpapi_sync.py` — advisory drift report
- [~] `internal/lamp` (mutexed conn) + `internal/store` — DEFERRED to Phase 3: httpapi opens a fresh k7tcp conn per call (same as pc-bridge). Phase 3 introduces `internal/lamp` as the single owner shared by scheduler+httpapi+proxy.
- [x] **exit gate PASSED:** OTA'd Pi to hub-v0.3.0. `/api/lamp/read` via the real Pi service returns the real K7 Pro (name `K7_Pro42113`, its actual 24-slot reef schedule). `/api/preview` + `/api/hand` reach the lamp (tested vs mock). Proxy `:8266` listening. UI serves (`/static/index.html`). Caps 9 true / 9 false.
- NOTE: browser render check of the UI still pending (interrupted); endpoints all verified.

**Pi is currently running hub-v0.3.0.** Releases: hub-v0.1.0, hub-v0.2.0, hub-v0.3.0.
KNOWN NIT: `go vet` can't run on cmd/httpapi (they import vendored k7tcp which has
an upstream `%s:%d` IPv6 printf nit) — CI vets config/version/updater/proxy only.
Candidate upstream PR: `net.JoinHostPort` in k7tcp connect().

### Phase 2.5 — pi-bridge UX layer  ✅ DONE (tag `hub-v0.4.0`)  [user requests]
- [x] `internal/piweb` — one middleware over httpapi: serves `/pi/*`, injects
  `<script src="/pi/overlay.js">` into HTML (upstream files untouched),
  intercepts `/api/profiles*`, rewrites `/api/push` for `schedule_shift_minutes`
- [x] `assets/overlay.js` + `dict-zh-Hant.json` (105 terms) — zh-Hant translation
  of basic UI text (exact-match only, proper nouns left alone; 中/EN toggle) +
  an Updates widget (`/api/update/status` + `/api/update/apply`) + version footer
- [x] `internal/profiles` — per-lamp profile store `data/profiles/<lampID>/*.json`
  (lampID = lamp MAC via `ip neigh`, else lamp name, else default); one-time
  migration from the legacy store.json map; survives OTA. tests.
- [x] FIX #4: `/api/push` now honours `schedule_shift_minutes` (rotates the 24
  rows) — upstream pc-bridge silently ignored it
- [x] getters added to vendored httpapi: `LampName()`, `LegacyProfiles()`
- [x] tests: piweb shift-rotation + HTML injection; profiles per-lamp isolation + migrate
- answers given to user: #4 Shift = photoperiod time-shift (was upstream no-op);
  #5 Base/Effective Today/Play-day chart modes (Effective needs Phase 3 overlays)

### Phase 3 — Always-on engine  (tags `hub-v0.5`..`0.9`)  ← the big port of Effects.cpp + Moon.cpp
- [x] **v0.5.0** `persistent_controller_clock` + `logs` + engine tick + `/api/time` + `/api/output/status` + `/api/wifi/signal` + `/api/logs`  → **11/18**
  - `internal/engine/model.go` — PURE port of Effects.cpp math: interpolate,
    EffectiveSchedule (seasonal+UI shift resample + acclimation scale),
    applySiesta/applyMaster/applyLunar, LunarWindow + clampWindowToNight,
    Compute() = restoreScheduledOutputNow. `moon.go` = Moon.cpp. 8 golden tests.
  - `internal/engine/engine.go` — tick loop (60s + Kick on push/master), diffs
    vs lastSent, `Hand()` on change, tracks OutputStatus. Override hook (feed/maint).
  - `internal/lamp` — single mutexed connection owner, generous timeouts, Health()
  - `internal/ringlog` — bounded log + slog.Handler wrapper
  - `internal/piapi` — middleware: the 4 new endpoints + engine.Provider (reads
    httpapi StateSnapshot). Kicks engine on /api/push, /api/master.
  - vendored httpapi getters added: StateSnapshot(), Device()
  - verified vs mock: push schedule → engine kicked → output `[50,30,...]` sent within 2s
- [ ] v0.6.0 `smooth_ramp` (`/api/ramp/start|stop|status|tick`) — default OFF, engine interval → 2min when active, push-on-change
- [ ] v0.7.0 `feed_mode` + `maintenance_mode` (`/api/feed/*`, `/api/maintenance/*`) — timed engine.Override; buildMaintenanceChannels (MINI/PRO tables in Effects.cpp:79)
- [ ] v0.8.0 `tracked_lunar` — flip flag; LunarWindow already ports trackMoonrise. Add `/api/lunar/*` to piapi? (fixed lunar is in vendored httpapi; tracked just needs the cap on + engine already does moon math)
- [ ] v0.9.0 `acclimation` (`/api/acclimation/config|status`) + `seasonal_daylength` (`/api/seasonal/config|status`) — piapi gets its own small JSON store for these 2 configs; engine.Config already has the fields + math
- [ ] golden-vector tests vs ESP32 `/api/output/status` (needs the ESP32 powered + on the lamp AP — user's bench unit)
- [ ] **exit gate:** 17/18 caps, UI shows every control, all work

NOTE for v0.6-0.9: piapi already has the Provider + engine wiring. Each tag =
add endpoint handlers to piapi + flip the cap in main.go + (for accl/seasonal)
a tiny config store. engine.Config fields + math are ALL already there.
NOTE: `internal/lamp` gate is used by engine + proxy; vendored httpapi still
opens its own per-call conns (brief, low collision risk). Unify if it bites.

### Phase 4 — Setup page + hardening  (tag `hub-v1.0`)
- [x] `setup_portal` — ⚙ settings modal + `/api/setup*` (hub-v1.0.0). **18/18.**
- [~] `/api/warnings/status` — real feed done (hub-v0.9.10); diagnostics *page* still TODO
- [ ] 7-day soak on the Pi
- [x] `README.md` rewrite + "Exactly what diverges from upstream" section (0.9.6+)
- [ ] optional: PR `pi-bridge/` back to bitbarista
- [x] **exit gate: 18/18 — `hub-v1.0.0`.**

### Phase 5 — Home Assistant (tag `hub-v1.1`)
- [ ] `/api/ha/*` REST surface
- [ ] `homeassistant/k7_lamp/` custom integration (light, 6×number, switch, select, buttons, binary_sensor, sensor)
- [ ] install into `D:\HomeAssistant\custom_components\k7_lamp\`

### User-requested features (queue — slot into a tag when reached)
- [x] **FEAT-A done (hub-v0.5.5)**: overlay.js injects a collapsible "逐時數值表" —
  self-contained 24×6 editable % grid + ±1h rotate + ±1% power (per checked
  channel) + 從裝置載入 (`/api/state`) + 套用到燈 (POSTs the grid to `/api/push`).
- [x] **FEAT-B done (hub-v0.5.5)**: working dir renamed
  `k7-led-controller` → `K7_Pi_Wifi_Controller`; ESP32 flash scripts moved to
  `..\esp32-flash-experiment\`; `_archive` copy path is relative (unchanged);
  scheduled task SKILL.md path updated (task itself is OFF — user disabled it).
- [x] hub-v0.5.5 also: auto-update toggle (`auto_update` config, default off; UI
  "Auto" checkbox + `POST /api/update/config`); Wi-Fi signal indicator in topbar;
  fixes B (install.sh URL), C (httpapi lamp gate), E (wlan0 never-default via NM
  dispatcher, reboot-persistent).
- [x] UX-1 (hub-v0.5.1): move 檢查更新 + language INTO the `.topbar` (after versionChip);
  language is a `<select>` dropdown (LANGS array, easy to add locales). Removes
  the bottom-right floating bar.
- [x] UX-2 (hub-v0.5.1): Shift discoverability — overlay overrides `changeShift`
  to jump the chart to "Effective Today" + toast "按 Push 生效". (Shift math
  already works: `/api/push` with `schedule_shift_minutes` → piweb rotates the
  24 rows. Verified on Pi: +6h moved an 08-16 band to 14-22.) Root cause of
  "光譜不會移動": upstream Base chart mode never renders the shift, and
  explicit-apply (hub-v0.4.1, user-requested) means it needs a Push.
  (FEAT-A + FEAT-B are DONE — see the top of this section.)

---

## Current state (2026-09-09 — hub-v1.0.4 merged, PR #17; == origin/master a389395)

**Releases hub-v1.0.2 … hub-v1.0.4 all published. 18/18. Pi still on hub-v1.0.1
until the user OTAs — then: 📊 monitor, "✓ no issues" Checks, daily+reconnect
clock sync + drift check, model auto-detect, ⚙ ramp cadence, OEM presets,
faster ReadAll, one process clock.**

### NEXT
- User OTAs to hub-v1.0.4, relocates the Pi, runs the 7-day soak
  (`/api/diag` now has CPU/RAM/temp/disk; soak.log has them too).
- **Phase 5 — Home Assistant** (`hub-v1.1`): `/api/ha/*` + `custom_components/
  k7_lamp/` → `D:\HomeAssistant\custom_components\`. DESIGN.md §7.

## Current state (2026-09-09 — hub-v1.0.4 in progress)

### hub-v1.0.4 — in-UI system monitor + non-empty "Checks"
User asked for a Pi resource monitor in the controller UI, and noted the
"檢查" card is still blank (it's blank because there are genuinely no warnings —
confirmed on the real Pi: `/api/warnings/status` → `{count:0,items:[]}`).
- `/api/diag` snapshot extended with OS metrics: `cpu_count`, `load[3]`,
  `mem_total_kb`/`mem_avail_kb`, `disk_total_kb`/`disk_free_kb`, `soc_temp_c`,
  `lamp_ok`/`lamp_last_ok_at`. `cmd/reeftank-hub/sysinfo.go` (/proc, /sys reads)
  + `sysinfo_linux.go` / `sysinfo_other.go` (build-tagged `syscall.Statfs`).
  soak.log line gained `load1`, `mem_avail_kb`, `temp_c`.
- overlay.js: **📊 button** in the top bar → `openSysMon()` modal — CPU load,
  RAM (bar), SoC temp (colour-coded), disk (bar), Go process, engine state,
  lamp link; polls `/api/diag?lines=1` every 5 s.
- overlay.js: `mountChecksPlaceholder()` wraps `window.renderWarnings` so an
  empty warnings list shows a green "✓ 目前無異常" instead of nothing.
- Verified on the real Pi scratch port: temp 49.4°C, load 0.00, RAM 21%,
  disk 9% — all rendering with bars; "檢查" shows the all-clear.

## Current state (2026-09-09 — hub-v1.0.3 merged, PR #16; == origin/master ad34966)

**Both APK-teardown batch PRs landed (hub-v1.0.2 #15, hub-v1.0.3 #16). Releases
published. 18/18 capabilities. Pi is still on hub-v1.0.1 until the user OTAs.**

### NEXT
- User OTAs the Pi to hub-v1.0.3, relocates it near the tank, runs the 7-day
  soak. Check `curl <pi>/api/diag` (rss / goroutines / lamp_fails) + soak.log.
  If they turn Smooth Ramp on for part of it, also watch `w_auto` growth.
- **Phase 5 — Home Assistant** (`hub-v1.1`): `/api/ha/*` REST + `custom_components/
  k7_lamp/` → `D:\HomeAssistant\custom_components\`. DESIGN.md §7.
- deferred polish: #5 (255→100 clamp), #6 (inter-op gap, soak-gated), #9 (demo).

### hub-v1.0.3 — time-sync B + drift check + model detect + ramp cadence + OEM presets
- **time-sync B**: engine `Run` — 6h ticker → `untilNextDaily(4, tz)` (one sync
  at ~04:00 local) + `MaintNow()` (fired by `lamp.SetOnReconnect` when the link
  recovers) + startup. `maintenance()` = SyncTime then, if wired, `driftCheck()`.
- **drift check** (main.go `eng.SetDriftCheck`): after a sync, `api.ReadAll()`
  the lamp schedule, compare channels 2..7 vs `StateSnapshot().Schedule`; if
  they differ AND `LastPushedAt != "" && !scheduleAllZero` → `api.Republish()`.
  Never pushes a blank over a real schedule.
- `lamp.do()` tracks a recovery (`consecFail`>0 then a success) and fires
  `onReconnect` async (goroutine, no gate re-entry).
- **gap #3 model detect**: `httpapi.deviceFromLampName` (k7m*→mini, k7_/k7p*→
  pro); `saveStateFromLamp` auto-corrects `config.Device` + logs. table test.
- **⚙ Smooth Ramp cadence**: `EffectsStore.Ramp.IntervalMin`; `POST
  /api/ramp/config {interval_min}` (2–60, default 10); `applyRampCadence` uses
  it; `/api/ramp/status` returns `interval_min`. Overlay ⚙ modal gained the
  field (saved alongside lamp/location/channel).
- **OEM factory curves**: `internal/httpapi/presets-oem.json` (NEW, hand-
  transcribed from `../noo-psyche_APK_analysis.md` §5 — K7 SPS/LPS/SL + X4 for
  Mini). `handlePresets` merges `preset:oem-*` into the per-device catalog.
- **#8 threat model**: README Notes — `:8266` + the HTTP API are unauth
  plaintext; don't port-forward; wlan0 never-default limits blast radius.
- Verified on the real Pi scratch port: `/api/presets` shows `oem-*`,
  `/api/ramp/config` persists interval_min, model detect leaves k7pro alone,
  no spurious drift re-push.
- tests: `untilNextDaily`, `deviceFromLampName`, schedule helpers,
  (piapi rampConfig via existing patterns).

### NOT done from the batch (deferred): #5 (255→100 clamp, cosmetic), #6
(inter-op gap — only if soak shows spikes), #9 (demo mode — low value).

## Current state (2026-09-09 — hub-v1.0.2 in progress)

Context: user ran a ~10h soak on hub-v1.0.1 — RSS flat at 13MB, goroutines pinned
at 16, zero lamp fails, zero journal warnings. Then had another chat teardown
the OEM Android APK (`../noo-psyche_APK_analysis.md`,
`../noo-psyche_vs_pi-bridge.md`) → confirmed `k7tcp` is a correct impl, surfaced
robustness gaps. User picked a batch of fixes.

### hub-v1.0.2 — timezone + ReadAll robustness (gap-analysis #1, #2, #4)
- **#1 timezone**: `main.go` now does `time.Local = tz` (+ `os.Setenv("TZ")`).
  Before: engine scheduled in `config.Timezone` but `k7tcp.SyncTimeLocal` /
  `PushSchedule` sent `time.Now()` = the OS zone → on a stock UTC Pi the whole
  photoperiod ran 8h off. `resolveTimezone()` extracted + tested. piapi.warnings
  adds a "時區不一致" warning if the OS zone still ≠ config.
  `TestTimezonePinnedForLampClock`.
- **#2/#4 ReadAll**: new `internal/k7tcp/readrobust.go` (a NEW file — client.go
  stays byte-identical to upstream, sync check unaffected). `ReadAllRobust`:
  up to 4 attempts, each returns the instant a decodable frame is in hand.
  `frameStart()` locates the real frame past the lamp's 4-byte write-ack
  ("AB AA A5 A1") — verified by a raw-socket probe of the real K7 Pro, whose
  frame is `AB AA 10 08 <6 manual> <count=24> …` (byte 2-3 is the echoed cmd,
  NOT "A5 <type>"; the count byte at +10 is the discriminator). `lamp.ReadAll`
  and `httpapi.handleLampRead` both route through it.
  **Measured on the real Pi+lamp: `/api/lamp/read` 5.0s → 1.05s, no more 502s.**
  Tests: clean / ack-then-frame / coalesced / junk-then-frame / bounded-retry.
- `routes()` gained `resolveTimezone`; piapi.Deps gained `TZName`.

### NEXT (this session, batched by the user) — hub-v1.0.3+
- time-sync **B**: drop the 6h ticker → daily (04:00) + on-reconnect + startup;
  the daily sync also **reads back the lamp schedule and re-pushes if it drifted**
  from what pi-bridge expects (the OEM-teardown "drift check" idea).
- gap #3: derive `k7pro`/`k7mini` from `LampState.Name` prefix in
  `saveStateFromLamp`; auto-correct or warn.
- ⚙ settings: add a **Smooth Ramp send-interval** field (currently hardcoded
  `rampOnInterval = 10m`).
- ship the OEM factory curves (`../noo-psyche_APK_analysis.md` §5) as
  `preset:k7-sps-factory` / `-lps-` / `-sl-`.
- README: `:8266` proxy is unauthenticated on the LAN — threat-model note (#8).
- (deferred/skip) #5 clamp 255→100 cosmetic, #6 inter-op gap, #9 demo mode.

## Current state (2026-09-08 — hub-v1.0.1 merged)

**PR #14 merged. origin/master == origin/dev/pi-bridge == c68b176. Release
`hub-v1.0.1` published. 18/18.** This session: hub-v0.9.6 → hub-v1.0.1 (8 feature
releases + PROGRESS commits).

### NEXT
- **7-day soak** — user relocates the Pi near the tank, updates to hub-v1.0.1,
  runs it a week. Review afterwards: `curl <pi>/api/diag` or
  `cat /opt/reeftank-hub/data/soak.log` — watch `rss_kb` (leak), `w_auto`
  (hammering), `lamp_fails`/`lamp_consec_fail` (reconnect health).
- **Phase 5 = Home Assistant** (`hub-v1.1`): `/api/ha/*` REST + `custom_components/
  k7_lamp/` → `D:\HomeAssistant\custom_components\`. DESIGN.md §7 has the shapes.

### hub-v1.0.1 — write-counter persistence + soak monitor + update-apply fix
Three things, all from user reports / the soak plan:
1. **今日上傳次數 was resetting on every restart/OTA.** It was in-memory in the
   engine + httpapi. New `internal/tally` — one shared `*tally.Counter`
   (`data/writes.json`, day-bucketed, atomic write, flushed every 5 min + on
   SIGTERM). engine `SetTally`, httpapi `Options.Tally`, piapi `Deps.Tally`;
   `/api/output/status` reads it. Removed `engine.WritesToday` /
   `httpapi.ManualWritesToday`. tests in `internal/tally`.
2. **Soak monitor** (Phase 4). `cmd/reeftank-hub/diag.go` — `diagAPI` writes an
   hourly line to `data/soak.log` (ts, ver, uptime, rss_kb [Linux only],
   heap_kb, goroutines, gc, engine live, lamp_ops/fails/consec_fail, w_auto,
   w_manual) and serves `GET /api/diag` = `{now: <snapshot>, log: <tail>}`
   (`?lines=N`). `internal/lamp` gained `Stats()` (ops/fails/consec_fail;
   engine + proxy conns only — httpapi opens its own, not counted).
3. **`/api/update/apply` was too strict.** 0.9.8 required `tag` to exactly match
   the available release → "available update is hub-v1.0.0, not the requested
   hub-v0.9.10" when the page's cached status was stale. Now: only
   `{confirm:true}` is required; `tag` is advisory; a newer release than the one
   named is installed, and the response's `applying` says what. overlay polls
   `res.applying` instead of the stale `d.available`.
- `routes()` refactored to `(…, ui, extra ...func(*http.ServeMux))`; main passes
  `setup.register, diag.register`.
- Verified vs mock: 3×/api/hand → `writes_today.manual:3`; `/api/diag` snapshot
  + soak.log startup line; `apply {confirm:true, tag:"hub-v0.5.0"}` →
  `{applying:"hub-v1.0.0", requested:"hub-v0.5.0"}` (no more 409).

## Current state (2026-09-08 — hub-v1.0.0 MERGED → 18/18 🎉)

**PR #13 merged. origin/master == origin/dev/pi-bridge == bf76ddb. Release
`hub-v1.0.0` published (arm64 binary). ALL 18 capability flags true — full 1:1
with the ESP32 firmware.** User applies the OTA from the UI (confirmation
dialog). This session shipped hub-v0.9.6 → hub-v1.0.0.

### NEXT — Phase 4 tail, then Phase 5
- 7-day unattended soak on the Pi (no lamp hammering — check the 今日上傳次數
  counter stays sane; clean reconnect after a lamp power-cycle; no RSS growth in
  `systemctl status` / `/proc/<pid>/status`). Needs real time on real hardware.
- (minor) a dedicated diagnostics *view* — the warnings feed already backs the
  inline "Checks" card; a fuller page is optional.
- **Phase 5 = Home Assistant (`hub-v1.1`)**: `/api/ha/*` REST surface +
  `custom_components/k7_lamp/` (light, 6×number, switch, select, buttons,
  binary_sensor, sensor) → install into `D:\HomeAssistant\custom_components\`.
  See DESIGN.md §7 for the entity list + endpoint shapes.

### hub-v1.0.0 — `setup_portal` + settings page → **18 / 18 capabilities**
- `caps["setup_portal"] = true` in main.go (main loop now sets ALL 18 true).
- `cmd/reeftank-hub/setup.go` — `setupAPI` registered by `routes()`:
  - `GET /api/setup` — aggregate `{lamp, location, update, wifi, system}`
  - `POST /api/setup` — `{location:{lat,lon,timezone}, update:{channel}}` →
    validated, written to `config.json`; response `{ok, restart_required:[…]}`
    (timezone + channel need a restart — the daemon reads config.json at boot).
    Lat/lon are stored but nothing consumes them yet (moon math is date-only).
  - `POST /api/setup/restart` — `{confirm:true}` → `systemctl restart reeftank-hub`
  - `POST /api/setup/factory-reset` — `{confirm:true}` → wipe `store.json`,
    `effects.json`, `profiles/` (keeps `config.json`), then restart
- `routes()` signature gained a `*setupAPI` param; `main_test.go` updated + new
  `TestSetupGetAndPost` / `TestSetupFactoryResetNeedsConfirm`.
- overlay.js: **⚙ button** in the top bar → a settings modal (lamp model/IP/port
  via existing `/api/config`; timezone/lat/lon + update channel via `/api/setup`;
  Restart + Factory-reset buttons, both `window.confirm()` gated). `modalShell`
  helper extracted. ~24 dict terms added.
- Lamp host/port/device stay on `/api/config` (unchanged, upstream).
- Verified vs mock in browser: `/api/capabilities` → all 18 true; ⚙ opens the
  modal; saving channel+tz persists to config.json and reports
  `restart_required:[timezone, update_channel]`.

### Phase 4 remaining after this: 7-day soak, diagnostics *page* (feed is done).
### Phase 5 = HA (`hub-v1.1`).

## Current state (2026-09-08 — hub-v0.9.10 merged)

**PR #12 merged. origin/master == origin/dev/pi-bridge == fcfc1f2. Release
`hub-v0.9.10` published.**

### hub-v0.9.10 — "檢查" (Checks) panel is now a real warnings feed
User asked what the empty "檢查" card at the bottom does. It's the shared UI's
`#warningsList`, fed by `GET /api/warnings/status` (`{items:[{level,message}]}`);
upstream pc-bridge never implemented it and 0.9.8's stub used the wrong key
(`warnings` not `items`), so it was always empty.

`piapi.warnings` now reports live conditions:
- clock not set (`engine.ClockSane()`) — error
- lamp never contacted / last contact failed (`lamp.Health()`) — warn
- Wi-Fi to the lamp < 35% (`iw dev <wlan> link`) — warn
- **schedule all-zero while in auto mode** (would leave the tank dark) — warn
  (this is exactly the confusion the user hit earlier)
- last engine write to the lamp failed — warn

`piapi_test.go` covers the dark-schedule + no-lamp-contact cases. This partly
does Phase 4's "`/api/warnings/status` real warnings feed"; a diagnostics *page*
(vs this inline panel) is still Phase 4.

## Current state (2026-09-08 — hub-v0.9.9 merged)

**PR #11 merged. origin/master == origin/dev/pi-bridge == 929312e. Release
`hub-v0.9.9` published. User applies the OTA from the UI (confirmation dialog).**

Session recap — this run shipped hub-v0.9.6 … hub-v0.9.9:
- 0.9.6: dead Read buttons, missing 光譜數值表 (`window.chart` is let-scoped →
  `Chart.getChart`), README "diverges from upstream" section + hard-rule,
  arm64-only decision.
- 0.9.7: Smooth Ramp = the live-driver switch (engine dormant when off; lamp
  runs its own 0x1007 schedule), Push pre-bakes today's effect snapshot when
  ramp off, value table always-open, top-bar 今日上傳次數 counter.
- 0.9.8: `/api/update/apply` needs `{confirm:true,tag:...}` + overlay confirm
  dialog (user saw "auto-updated" — journal proved the loop honoured
  auto_update:false; it was POSTs to the endpoint), hourly chart gridlines
  (draw plugin, NOT options mutation → that recurses Chart.js v4), value-table
  column alignment, fixed a `_sync`↔`buildGrid` infinite recursion that broke
  the table on some loads, `/api/warnings/status` stub (200 empty).
- 0.9.9: Shift ◀▶ rotates the real Base schedule in place (stays on Base, no
  double-shift); value-table ±1h/±1% kept per user.

### hub-v0.9.9 — Shift now moves the Base schedule for real

### hub-v0.9.9 — Shift now moves the Base schedule for real
User: "BASE 的 SHIFT 是壞的,每次按 SHIFT 他就自動跳到 Effective Today".
- The `◀ ▶` Shift buttons now call a real `rotateScheduleHours(delta)` in
  overlay.js: reads all 6 channel columns off the chart, writes them back
  rotated `new[h]=old[h-delta]` through the page's own `dragData.onDrag` (so
  `scheduleBase` — what Push sends — actually moves). Chart **stays on Base**;
  the old wrap's forced `setChartMode('effective')` is gone.
- `#shiftVal` is now a running readout (`data-k7pi` attr holds the net hours);
  resets to `+0h` after Push. `dayShift` (the upstream `let`) is never touched →
  `signedShiftMinutes()` stays 0 → piweb does NOT rotate again server-side → no
  double-shift. The 0.9.7 post-push `readFromDevice()` for shift is removed
  (moot now, and it risked clobbering on a flaky lamp read).
- Removed the `chartEffectiveValueAtMins` wrap (it compensated for the old
  parameter model; with a real rotation Effective Today already reflects it via
  the rotated `scheduleBase`).
- Value-table `⟲ -1h / ⟳ +1h / ±1%` buttons **kept** (user asked) — same
  mechanism, but per-checked-channel for fine manual nudging.
- Verified vs mock in browser: Shift +3h → chart stays on Base, all 6 channels
  rotate `new[h]==old[h-3]`, label `+3h`; Push → server stores the rotated rows
  once (`schedule_shift_minutes:0`), label back to `+0h`. No console errors.
- Note for the user: Windows Firewall prompts for `reeftank-hub.exe` are from
  local `go run` test instances (localhost only — safe to Deny); not the Pi.

## Current state (2026-09-08 — hub-v0.9.8 merged)

**PR #10 merged. origin/master == origin/dev/pi-bridge == c9a9bd5. Release
`hub-v0.9.8` published (arm64 binary + SHA256SUMS). User applies the OTA from the
UI — now with a confirmation dialog.**

### hub-v0.9.8 — 3 user reports + a value-table crash fix

### hub-v0.9.8 — 3 user reports + a value-table crash fix
1. **"it auto-updated again without me ticking Auto"** — investigated on the Pi:
   `config.json` has `auto_update:false` and the hourly loop honoured it (journal:
   `"update available (auto_update off — apply from the UI)"`). The updates all
   came from `POST /api/update/apply` (no timer, no cron, shared-ui doesn't call
   it — only the overlay's green "立即更新" button does). Hardened anyway:
   `/api/update/apply` now requires `{"confirm":true,"tag":"<exact target>"}` —
   a bare/replayed POST is 400'd; wrong tag is 409'd. Overlay's "立即更新" now
   shows a `window.confirm()` and sends the confirm+tag body.
2. **hourly X-axis gridlines** — `hourGridPlugin` (a draw-time Chart.js plugin,
   registered via `Chart.register`, NOT an options mutation — mutating
   `chart.options.scales` in Chart.js v4 recurses the proxy setters and throws
   `Maximum call stack size exceeded`). Rules every non-4h hour.
3. **value-table number alignment** — `table-layout:fixed`, equal 79px channel
   columns, centered input `<td>`s. Header text and cell values now share a
   center-x.
- **BUG FIXED (was breaking the value table since 0.9.7 on some loads):** the
  grid's `_sync` could call `buildGrid` synchronously while `buildGrid` calls
  `_sync` at its end → infinite recursion → `Maximum call stack size exceeded`
  in Chart.js internals → `start()` aborted before `mountValueTable`. `_sync`
  now defers the rebuild (`setTimeout`, `_rebuilding` guard). Boot loop
  rewritten: every step wrapped in try/catch and retried until it takes hold
  (was: stopped as soon as `installExplicitApply.done`).
- `/api/warnings/status` now returns `200 {warnings:[],count:0}` (was 404 —
  Phase 4 replaces it with a real feed) to stop the shared UI logging a 404 on
  every poll.
- Verified vs mock in browser: grid mounts, axis plugin registered, columns
  aligned, zero console errors; `/api/update/apply` gate tested via curl +
  `main_test.go`.

## Current state (2026-09-08 — hub-v0.9.7 merged)

**PR #9 merged. origin/master == origin/dev/pi-bridge == 65ff115. Release
`hub-v0.9.7` published by CI. User applies the OTA from the UI himself.**

### hub-v0.9.7 — engine model change + more UX (this session, after 0.9.6)
User asked for a different engine model + several UX fixes. Built:
- **Smooth Ramp is now the "who drives the lamp" switch** (`engine.SetLive`):
  - OFF (default) → engine **dormant**, sends nothing. `/api/push` writes the
    full 24-slot 0x1007 schedule once; the lamp runs it itself.
  - ON → engine is the live driver, tick **10 min** (was 60s), 0x1005 on change.
  - Feed/Maintenance overrides still drive 0x1005 regardless; when a timed
    override ends while dormant the engine calls `RepushFn` (= `httpapi.Server.
    Republish`) to re-arm the lamp's own 0x1007 schedule. `rampStop` re-arms too.
- **Push pre-bakes the full "today snapshot"** when ramp is off (`piapi.
  prebakePush`): runs `engine.Compute()` hour-by-hour so acclimation, seasonal
  shift, tracked lunar, siesta, master are all folded into the 24 rows, then
  adds `"prebaked":true` so `httpapi.handlePush` sends them verbatim instead of
  baking a second time. (User's Q1 choice: snapshot only at Push, engine
  doesn't keep them current when dormant.)
- **Shift fix**: overlay's `pushSchedule` wrap re-reads after a shifted push, so
  the Base chart shows the rotated schedule and the `+Nh` counter resets →
  fixes "Shift only shows in Effective Today" + the double-shift-on-re-Push bug.
- **Spectrum value table** is always open under the chart (no collapse) — user
  wants chart + table visible together for tuning.
- **Today's lamp-write counter** in the top bar: `📤 今日 自動 X · 手動 Y`
  (`/api/output/status` now returns `live` + `writes_today:{auto,manual,date}`;
  engine counts its writes, httpapi counts push/hand/preview/re-arm; reset at
  local midnight, server-local date).
- `engine.New` now takes an `engine.Lamp` interface (satisfied by `*lamp.Lamp`)
  so tests can inject a fake — new tests: dormant-vs-live, override re-arm,
  write counting. New `piapi_test.go`: prebake bakes acclimation, no-ops when
  ramp on / manual mode.
- Verified locally vs mock in the browser: table always-open, write chip,
  ramp on→live:true + auto count, ramp off→live:false + re-arm (+manual count),
  shift→push→shift resets. (Mock's readAll returns an empty schedule so it
  can't validate the shift row-rotation round-trip — real lamp returns the
  stored schedule; piweb rotation is unit-tested.)
- **KNOWN pre-existing issue (not fixed, both off for this user):** if siesta or
  lunar are enabled in `state` AND smooth ramp is ON, the engine may
  double-apply them (httpapi baked them into `state.Schedule`, engine's Config
  re-applies). Untangle when it bites.

## Current state (2026-09-08 — end of session)
- **All merged to master through PR #7** (hub-v0.9.5 + PROGRESS). origin/master ==
  origin/dev/pi-bridge == 079ef0b was the baseline for this session.
- **hub-v0.9.6 merged (PR #8, origin/master == origin/dev/pi-bridge == 9d93517).**
  Release `hub-v0.9.6` published by CI. User applies the OTA from the UI himself.
  Three user-reported fixes:
  1. **Read buttons were no-ops on the Pi.** Root cause: upstream
     `readControllerState()` only does a live `/api/lamp/read` when platform is
     `pc_bridge`; on `reeftank_hub` it just reloads the local `/api/state` cache.
     Fix: `overlay.js` wraps `window.readFromDevice` to `GET /api/lamp/read`
     first (Option A, user-chosen — matches pc-bridge Read, overwrites unsaved
     chart edits). Both Read buttons + the value-table "從裝置讀取" go through it.
  2. **光譜數值表 never appeared (hub-v0.9.5 regression).** The 0.9.5 rework read
     `window.chart`, but upstream declares `chart` with `let` in a classic
     `<script>` → not a window property → `mountValueTable()`'s guard always
     bailed. Fix: `liveChart()` helper resolves via `Chart.getChart('schedChart')`
     (Chart.js v4 registry). Header given a surface bg for discoverability.
  3. README: new **"Exactly what diverges from upstream"** section (full
     file-level + behaviour-level list) + a Hard-rule to keep it current.
- Diagnostic finding (NOT changed — user will drive the UI himself): the Pi's
  `②/api/state.schedule` is all-zeros and so is the lamp's stored native
  schedule, so the engine is holding output at `[0,0,0,0,0,0]`. The user's real
  evening curve lives only in the browser profile `saved:K7_Pro42113`; the
  engine reads `state.Schedule`, never a profile. User will apply the profile +
  Push from the UI to repopulate both ② and ①.
- Engine behaviour clarified for the user: the tick loop runs 24/7 regardless of
  Smooth Ramp; ramp only sets cadence (5min off / 1min on); `step()` sends
  `lamp.Hand()` (0x1005 live) only when the computed output changes vs
  `e.lastSent`. Native `0x1007` schedule (via Push) is the Pi-down fallback only.
- **Capabilities: 17 / 18** — only `setup_portal` left (Phase 4).
- The always-on engine drives the real K7 Pro 24/7: schedule interpolation,
  smooth-ramp cadence, feed/maintenance timed overrides, tracked lunar,
  acclimation, seasonal shift. Feed was verified changing the physical lamp.
- auto_update OFF by default (manual via the UI button). wlan0 never-default
  hardened (NM dispatcher, reboot-persistent). Weak signal (~-74 dBm) — user
  relocates the Pi to the tank later.
- master protected (no force-push / deletion). README fork banner is the only
  diff vs upstream. **D (golden-vs-ESP32) DROPPED** per user.
- Working dir: `D:\HomeAssistant\K7\K7_Pi_Wifi_Controller`; ESP32 flash scripts
  in `..\esp32-flash-experiment\`. Scheduled resume task: OFF (user disabled).

### hub-v0.9.6 — DONE
- build/vet/test/arm64/k7tcp-sync all green ✅
- committed, pushed, tagged `hub-v0.9.6`, CI green, PR #8 merged, dev synced ✅
- verified locally against `tools/mock_k7pro_lamp.py` in the in-app browser:
  both Read buttons issue `GET /api/lamp/read` (200); 光譜數值表 mounts under the
  chart in Auto mode, expands, and live-links to the chart datasets when a
  preset is loaded.
- NOT deployed to the Pi scratch port — the JS fixes are fully browser-side and
  the real lamp currently has an all-zero schedule anyway, so a scratch-port
  engine would only risk dual lamp control for no extra signal. User applies the
  OTA + repopulates the schedule from the UI (his stated workflow).

### DONE (was "open feature request"): 引擎託管 / 燈自主 切換
Implemented as hub-v0.9.7 — Smooth Ramp is the switch (see above).

### NEXT (when the user says go): Phase 4 = `hub-v1.0.0` → 18/18
- `setup_portal` cap + a settings page: lamp host/port, wifi status, factory
  reset, update channel, lat/lon, timezone
- `/api/warnings/status` real warnings feed + a diagnostics view
- 7-day unattended soak (no lamp hammering, clean reconnects, no mem growth)
- then Phase 5 = HA (`hub-v1.1`): `/api/ha/*` REST + `custom_components/k7_lamp/`

### Phase 3 — v0.6-0.9 ✅ DONE (tag `hub-v0.9.0`) → **17/18**
- `internal/piapi/effects.go` — EffectsStore (data/effects.json) + all endpoints:
  - `smooth_ramp`: /api/ramp/{start,stop,status,tick}. Engine gets SetInterval();
    ramp ON → 60s tick, OFF → 5min. Engine already interpolates+diffs+push-on-change.
  - `feed_mode` + `maintenance_mode`: /api/{feed,maintenance}/{start,stop,status}.
    engine.Override (timed full-output replacement). Channel tables from Effects.cpp
    (feedPro {80,10,40,5,10,0} ch[3]=intensity; maintenancePro {100,30,55,15,40,5}×intensity).
  - `acclimation` + `seasonal_daylength`: /api/{acclimation,seasonal}/{config,status}.
    engine.Config already had the fields+math; Provider now merges them from EffectsStore.
- main.go: ALL caps true except setup_portal. Engine default interval 5min (ramp off).
- vendored k7tcp: 1 documented patch (net.JoinHostPort — silences Go 1.27 vet);
  check_k7tcp_sync.py applies the same transform to upstream before diffing.
- engine tests: SetInterval clamp, Override active/expired/nil, step-applies-override.
- verified vs mock: 17 caps, ramp cadence flips, feed/maint override the output with
  the right channels + countdown, acclimation current_percent, seasonal shift.

### NEXT: Phase 4 = hub-v1.0 (setup_portal → 18/18) then soak; Phase 5 = HA.
OLD NOTES (kept):
### (was) NEXT: hub-v0.6.0 = `smooth_ramp`
- add `/api/ramp/start|stop|status|tick` to `internal/piapi` (POST start/stop/tick, GET status)
- ramp state (on/off, last_tick) persisted in a small piapi JSON store under DataDir
- when ramp ON: `engine.SetInterval(2*time.Minute)` (add that method) so the tick
  loop pushes interpolated values every ~2 min instead of 60s; when OFF back to 60s.
  Engine ALREADY interpolates + diffs + push-on-change, so "smooth ramp" ≈ just
  the faster cadence. Default OFF (flash write-wear — Effects.cpp comment).
- flip `caps["smooth_ramp"] = true` in main.go
- `/api/ramp/status` shape (from shared-ui `DEFAULT_STATUS.ramp`): `{active:bool, last_tick:<epoch or iso>}`
- UI has a consent modal for ramp (`#rampConsentModal`) — it POSTs /api/ramp/start after consent; just need the endpoint to 200
- deploy hub-v0.6.0, verify `/api/ramp/*` + that output changes more often with it on
Then v0.7 (feed+maintenance), v0.8 (tracked_lunar — likely just cap flip + maybe
`/api/lunar/*` passthrough since fixed lunar is already in vendored httpapi and
engine does moon math), v0.9 (acclimation+seasonal — piapi gets a config store;
engine.Config already has the fields+math). See Phase 3 checklist notes.
- Real lamp verified: MAC `4a:55:19:ec:b0:49`, profiles migrated to
  `data/profiles/mac-4a_55_19_ec_b0_49/` (user's `BRS_AB`, `K7_Pro42113`).
- User confirmed the UI renders + works in a browser.

## BLOCKED
_(none)_

## Session log
- 2026-09-07/08 — Phase 0 done (scaffold, vendored k7tcp, config, version,
  API.md, parity tooling).
- 2026-09-08 (later) — **hub-v0.9.6**: fixed dead Read buttons (overlay wraps
  readFromDevice → /api/lamp/read), fixed missing 光譜數值表 (window.chart is
  let-scoped → use Chart.getChart), README divergence section + Hard-rule.
- 2026-09-08 (later still) — **hub-v0.9.7**: Smooth Ramp = live-driver switch
  (engine dormant when off), Push pre-bakes today's effect snapshot when ramp
  off, Shift re-reads after Push (fixes double-shift + visibility), value table
  always open, top-bar 今日上傳次數 counter (auto/manual). engine.Lamp interface
  for testability. See "Current state" above for detail.
- 2026-09-08 — **Phase 1 done.** updater + CI + deploy scripts. Deployed to Pi.
  OTA self-update AND rollback both verified on real hardware end-to-end.
  Pi running hub-v0.2.0. Next: **Phase 2** — vendor+adapt the pc-bridge HTTP
  server into `internal/httpapi`, embed shared-ui, `internal/lamp` (mutexed
  conn), `internal/store`, `internal/proxy` (raw :8266), wire the 21 free
  endpoints, flip 9 capability flags true. Exit: UI loads on LAN, Read/Preview/
  manual/push work vs real lamp, parity_check green on those 21.
