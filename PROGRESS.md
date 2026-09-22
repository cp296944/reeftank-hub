# ReefTank Hub 開發進度

更新日期：2026-09-22

## 已確認決策

- Hub 使用獨立 repo：`cp296944/reeftank-hub`。
- Repo 已改為 Public，讓 Raspberry Pi 可無 Token 下載經雜湊驗證的 Release。
- HA 保留設備管理、自動化及告警。
- Google Sheet 保留水質主資料權。
- Hub 與 HA 各自查詢小魚未來水溫。
- 三盞 K7 已連動，只控制一台主燈。
- 滴定先完成軟體與模擬器，最後才做 BLE 實機驗證。
- Hub 的 K7 共用修改以 PR 同步回舊 K7 repo。

## 實機基線

- Raspberry Pi：`192.168.0.149`
- 目前版本：`hub-v0.4.0`（2026-09-22 網頁 OTA 成功）
- Channel：stable
- Auto Update：off
- `/healthz`：正常
- Hub bootstrap、K7 資料遷移與隔離回滾演練已完成；服務已切換為 `reeftank-hub`。

## 本版完成範圍

- [x] Phase 0：K7 相容、設定與資料遷移、舊 OTA 過渡與回滾基線。
- [x] Phase 1：共用 Hub 外殼、響應式頁面與六個固定路由。
- [x] Phase 2：SQLite migration、來源狀態、設備映射、HA／水溫歷史、備份、匯出與損壞復原。
- [x] Phase 3：HA REST + WebSocket 即時訂閱、18 路插座映射／控制、關鍵設備確認、stale 保護、原始歷史回補與 Hub 本機趨勢。
- [x] Phase 5：小魚未來獨立查詢、最後成功值／過期／延遲與 SQLite 永久保存。
- [x] Phase 8（現有資料範圍）：首頁彙整水溫、HA 健康、總功率、月用電與來源狀態；Google 水質與滴定清楚標示尚未完成。
- [x] 實機 OTA 演練發現 11 MB SQLite 版本超過舊 30 秒下載限制；下載期限延長為 5 分鐘，逾時時舊版保持運作且不會半套切換。
- [x] Phase 4 軟體：Google 水質最新值／全歷史鏡射、斷線快取、手動同步、七項水質頁面與寫入後回讀流程。Google Apps Script 新版需重新部署才會開放 Hub 寫入。
- [x] Phase 6：K7 transport 預設休眠，開啟 K7 頁面時建立租約，頁面離開後逾時休眠。
- [x] Phase 7 軟體：四泵頭設定、校正、容器、液量、每日總量、分次／星期排程、預估消耗、手動模擬、稽核及四類故障注入；BLE capability 維持未驗證。
- [x] Phase 8：首頁整合 HA、電力、生命維持設備、獨立溫度、水質及滴定來源狀態。
- [x] Phase 9：GitHub SHA-256 Release、OTA 前 SQLite 備份、獨立 release 目錄、原子 symlink、健康確認及既有自動回滾測試；`hub-v0.4.0` 實機 OTA 成功且保留 136,621 筆 HA 樣本。
- [ ] Phase 10：BLE transport 必須在使用者與實機旁依安全順序驗證；目前所有 capability 均正確標示 `ble_verified=false`。

## 已完成

- [x] Hub repo 建立。
- [x] 現有 Pi Bridge 程式搬入並保留來源 commit 紀錄。
- [x] Go module 與 command 改名為 `reeftank-hub`。
- [x] Hub `/`、`/K7/`、`/dosing/`、`/power/`、`/water/`、`/system/` 路由。
- [x] 舊 K7 API 與頁面相容入口。
- [x] Bootstrap 狀態、可重跑安裝基礎、備份、受限 systemd/polkit 維護閘門。
- [x] 維護請求固定路徑、時效、nonce 與 action 白名單。
- [x] 18 路邏輯設備名稱及 HA entity 映射資料層。
- [x] `/power/` 設備映射介面；占用中的實體以交換方式重新配置。
- [x] `/power/` 依現有 HA 儀表板分成 `3A31 主機`、`3F2D 副機`、`BFB9 擴充` 三條六孔延長線；資料模型同時保存各排插的 LED、電流、瓦數、今日、本月與運行時間實體。
- [x] HA 憑證安全入口：固定使用 root-only `/etc/reeftank-hub/ha.env`，systemd 載入 `HA_URL`/`HA_TOKEN`；Token 永不序列化到一般設定、資料備份或 Git。
- [x] HA 即時電力 API 與插座控制白名單：三條排插、18 路電壓／電流／瓦數／今日／本月、LED、總計及水溫；7 天原始歷史查詢。
- [x] OTA、檢查更新、自動更新及版本歷程移至 Hub 首頁右上角；Hub 內的 K7 設定頁不再顯示系統更新控制。
- [x] `/power/` 儀表板依現行 HA 畫面重建為三排插詳情、三組能耗總計與瓦數／電流／水溫三張 7 天趨勢圖。

## Bootstrap 放行條件

以下全部完成後，才向使用者提供實機 bootstrap 指令：

- [x] 新 repo CI 能測試及產生 `linux/arm64` binary（首次 main build 2026-09-22 通過）。
- [x] `hub-v0.1.0` release pipeline 實際演練成功，公開資產下載及 SHA-256 驗證通過。
- [x] 舊 K7 `pi-v1.1.0` 過渡版已發布，能備份並切換到 Hub repo。
- [x] `/opt/k7-pi-bridge/data` → `/opt/reeftank-hub/data` 隔離遷移測試通過。
- [x] 服務切換順序固定為停止 K7 後才啟用 Hub，不會同時占用 port 80 與 8266。
- [x] 失敗復原會依 K7 `state/PREVIOUS` 原子切回 `pi-v1.0.4`；成功、失敗及不安全目標測試通過。
- [x] `config.json`、`effects.json`、`store.json`、`writes.json`、`soak.log` 與完整 `profiles/` 都列入遷移及備份測試。

## 下一步

- [x] 建立 Hub GitHub Actions 與 release manifest。
- [x] 建立 K7 allowlist 同步 workflow；未設定專用 Token 時確認為只讀跳過。
- [ ] 完成 K7 共用路徑同步檢查及 PR workflow。
- [x] 完成並發布舊 repo `pi-v1.1.0` 過渡版。
- [x] 在隔離目錄演練 bootstrap／migration／rollback，並唯讀確認實機回滾資產存在。
- [x] HA WebSocket 即時快取、可取得的 Recorder 原始歷史回補與本機永久資料庫。
- [ ] Google 水質鏡射（Phase 4）。
- [x] 獨立小魚未來水溫查詢與歷史（實機需設定 `XIAOYU_URL`）。
- [ ] 滴定領域模型、模擬器與完整介面。
