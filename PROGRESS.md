# ReefTank Hub 開發進度

更新日期：2026-09-22

## 已確認決策

- Hub 使用獨立 repo：`cp296944/reeftank-hub`。
- 開發期間 repo 為 Private；正式使用無 Token GitHub Release OTA 前改為 Public。
- HA 保留設備管理、自動化及告警。
- Google Sheet 保留水質主資料權。
- Hub 與 HA 各自查詢小魚未來水溫。
- 三盞 K7 已連動，只控制一台主燈。
- 滴定先完成軟體與模擬器，最後才做 BLE 實機驗證。
- Hub 的 K7 共用修改以 PR 同步回舊 K7 repo。

## 實機基線

- Raspberry Pi：`192.168.0.149`
- 目前版本：`pi-v1.0.4`
- Channel：stable
- Auto Update：off
- `/healthz`：正常
- 實機尚未部署 Hub 或執行 bootstrap。

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

## Bootstrap 放行條件

以下全部完成後，才向使用者提供實機 bootstrap 指令：

- [ ] 新 repo CI 能測試及產生 `linux/arm64` binary。
- [ ] `hub-v0.1.0` release pipeline 實際演練成功。
- [ ] 舊 K7 `pi-v1.1.0` 過渡版能備份並切換到 Hub repo。
- [ ] `/opt/k7-pi-bridge/data` → `/opt/reeftank-hub/data` 遷移測試。
- [ ] 新舊服務不會同時占用 port 80 與 8266。
- [ ] Bootstrap 可重跑、失敗復原及回到 `pi-v1.0.4` 演練。
- [ ] K7 排程、profile、寫入次數及設定在轉換後一致。

## 下一步

- [ ] 建立 Hub GitHub Actions 與 release manifest。
- [ ] 完成 K7 共用路徑同步檢查及 PR workflow。
- [ ] 完成舊 repo 過渡版。
- [ ] 在隔離目錄演練 bootstrap／migration／rollback。
- [ ] HA 即時狀態快取與本機歷史資料庫。
- [ ] Google 水質鏡射及獨立水溫查詢。
- [ ] 滴定領域模型、模擬器與完整介面。
