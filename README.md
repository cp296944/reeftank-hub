# ReefTank Hub

Raspberry Pi 上的海水缸資料與設備中控。Hub 提供單一網頁入口、本機歷史資料、K7 燈具控制、魔點四頭滴定介面，以及 Home Assistant、Google 水質資料和小魚未來水溫整合。

## 系統分工

- **ReefTank Hub**：彙整、歷史保存、網頁操作、K7、滴定機及 OTA。
- **Home Assistant**：設備管理、自動化與通知。Hub 不搬移或取代現有 HA 自動化。
- **Google 試算表**：NO3、PO4、pH、SG、KH、Ca、Mg 與換水紀錄的主資料來源。
- **小魚未來 API**：Hub 與 HA 各自獨立查詢，互不轉傳。

## 網頁入口

- `/`：ReefTank Hub 首頁
- `/K7/`：K7 完整控制（三燈連動、單一主燈）
- `/dosing/`：魔點四頭滴定
- `/power/`：HA 電源監控與設備映射
- `/water/`：水質與換水
- `/system/`：服務狀態、備份與 OTA

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

## 開發

```bash
go test ./...
go run ./cmd/reeftank-hub --listen :8080 --data-dir ./data --proxy=
```

完整決策與階段請見 [PROGRESS.md](PROGRESS.md) 與 `D:\HomeAssistant\REEFTANK\REEFTANK_HUB_DEVELOPMENT_PLAN.md`。

舊 K7 Pi Bridge 文件已保存在 [docs/K7_INHERITED_README.md](docs/K7_INHERITED_README.md)，來源歷史仍完整保留於舊 K7 repository。
