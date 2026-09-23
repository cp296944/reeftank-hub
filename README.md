# ReefTank Hub

Raspberry Pi 上的海水缸資料與設備中控。Hub 提供單一網頁入口、本機歷史資料、K7 燈具控制、魔點四頭滴定介面，以及 Home Assistant 與小魚未來水溫整合。

## 系統分工

- **ReefTank Hub**：彙整、歷史保存、網頁操作、K7、滴定機及 OTA。
- **Home Assistant**：設備管理、自動化與通知。Hub 不搬移或取代現有 HA 自動化。
- **Hub SQLite**：NO3、PO4、pH、SG、KH、Ca、Mg 與換水紀錄的主資料來源。
- **小魚未來 API**：預設由 Hub 與 HA 各自查詢；可在系統頁手動改由 HA 提供，且不會自動切換。

## 網頁入口

- `/`：ReefTank Hub 首頁
- `/K7/`：K7 完整控制（三燈連動、單一主燈）
- `/dosing/`：魔點四頭滴定
- `/calculator/`：獨立滴定計算工具（只計算）
- `/power/`：HA 電源監控與設備映射
- `/water/`：水質與換水
- `/system/`：服務狀態、備份與 OTA
- `/thread/`：ESP32-C6 RCP、OTBR 與 HA Matter 狀態

## Raspberry Pi 目標

- Raspberry Pi OS 64-bit / `linux/arm64`
- LAN：`eth0`
- K7 AP：`wlan0`，燈具 `192.168.4.1:8266`
- 安裝根目錄：`/opt/reeftank-hub`
- systemd：`reeftank-hub.service`
- Release：`hub-v*`
- OTA 資產：`reeftank-hub-linux-arm64`

目前 Raspberry Pi 仍安全運行舊 K7 `pi-v1.0.4`。`hub-v0.1.0` 與過渡版
`pi-v1.1.0` 已發布，公開下載、雜湊、資料遷移及隔離回滾演練均已通過；
實機只會在使用者手動確認 OTA 與 bootstrap 後切換。

## K7 雙專案同步原則

ReefTank Hub 內的 K7 共用程式是日後功能開發來源。`sync/k7-paths.txt` 明確列出允許同步回 `cp296944/k7-led-Raspberry-controller` 的路徑。同步流程只建立舊專案的 PR，不直接推送到舊專案預設分支，也不把 Hub、滴定、HA 或資料庫程式帶回 K7 專案。

跨專案自動 PR 需要專用 fine-grained token，權限僅限舊 K7 repo 的 Contents 與 Pull requests。未設定 token 時，CI 只做差異檢查，仍可使用本機同步工具。

## Home Assistant 憑證

HA 位址與 Long-Lived Access Token 只放在 Raspberry Pi 的
`/etc/reeftank-hub/ha.env`。此檔案為 root-only、由 systemd 載入，不會寫入
`config.json`、Hub 資料備份或 Git repository。環境變數名稱為 `HA_URL` 與
`HA_TOKEN`；應使用專屬 HA 使用者所建立的 Token。

## 小魚未來獨立水溫

在同一個 root-only 環境檔設定 `XIAOYU_URL`。網址包含設備序號，因此視為
敏感資料，不寫入一般設定、網頁回應、備份或 Git。Hub 每 60 秒直接查詢、
保留最後成功值及延遲／過期狀態，並將歷史永久保存到 SQLite。系統頁可手動
選擇 Hub 直連或 HA 來源；失敗時不會暗中切換。

## 本機水質資料

既有 Excel 的 73 筆水質與換水紀錄會在升級時冪等匯入 SQLite。之後由
`/water/` 直接新增資料，並與水溫歷史集中顯示；重啟與 OTA 不會重複匯入。

## 魔點四頭滴定

目前提供完整軟體模擬模式：四泵頭、校正、容器／液量、手動滴定、每日總量、
分次與星期排程、讀回確認、操作稽核及故障注入。所有 BLE capability 明確標成
尚未實機驗證，避免模擬成功被誤認為設備已執行。獨立計算模組依原 Excel
參數進行雙向換算；計算結果不會啟動、設定或傳送到滴定機。

Hub 首頁右上角集中管理檢查更新、立即 OTA、自動更新與版本歷程；K7
頁面只保留燈具本身的語言、監視與設定。電源頁透過 HA API 顯示三條
排插的即時狀態、18 路明細、總能耗及 7 天趨勢，控制操作只允許目前
設備映射中的插座與三條排插 LED。

HS300的高頻路徑由Hub直接向排插讀取，不改動HA整合的60秒週期。每條排插可
設定輪詢秒數，每個插座可獨立開關；短時間電流／功率動作會保存為本機事件，
用於補水與捲棉的今日次數及30日圖表。

Thread方案使用ESP32-C6作USB OpenThread RCP，樹莓派執行OTBR，IKEA
Matter-over-Thread裝置仍由HA Matter管理。首次刷寫與組網必須在C6實機接上後
驗證，未驗證前Hub只顯示真實狀態，不會提供看似成功的危險刷寫操作。

## 開發

```bash
go test ./...
go run ./cmd/reeftank-hub --listen :8080 --data-dir ./data --proxy=
```

完整決策與階段請見 [PROGRESS.md](PROGRESS.md) 與 `D:\HomeAssistant\REEFTANK\REEFTANK_HUB_DEVELOPMENT_PLAN.md`。

舊 K7 Pi Bridge 文件已保存在 [docs/K7_INHERITED_README.md](docs/K7_INHERITED_README.md)，來源歷史仍完整保留於舊 K7 repository。
