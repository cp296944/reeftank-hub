# ReefTank Hub 開發進度

更新日期：2026-09-24

## 已確認決策

- Hub 使用獨立 repo：`cp296944/reeftank-hub`。
- Repo 已改為 Public，讓 Raspberry Pi 可無 Token 下載經雜湊驗證的 Release。
- HA 保留設備管理、自動化及告警。
- Hub SQLite 為水質與換水正式主資料，Google Sheet 退出日常流程。
- 水溫來源由設定頁手動選擇 Hub 直連或 HA，不做自動切換。
- 三盞 K7 已連動，只控制一台主燈。
- 滴定先完成軟體與模擬器，最後才做 BLE 實機驗證。
- Hub 的 K7 共用修改以 PR 同步回舊 K7 repo。

## 實機基線

- Raspberry Pi：`192.168.0.149`
- 已確認實機可由 Hub 網頁 OTA；目前實機基線為 `hub-v0.9.1`。
- Channel：stable
- Auto Update：off
- `/healthz`：正常
- Hub bootstrap、K7 資料遷移與隔離回滾演練已完成；服務已切換為 `reeftank-hub`。

## 本版完成範圍

- [x] 下一版：修正舊滴定狀態 `audit:null` 導致介面無法載入，後端遷移與前端相容。
- [x] K7 改為明確手動連線、實際讀取驗證、健康狀態及中斷控制；開頁不再自動喚醒。
- [x] Excel 73 筆水質／換水資料內建冪等匯入，本機新增、修改、刪除 API 與最新值／歷史頁面。
- [x] 水溫併入水質頁；首頁依每個參數最後一筆非空值顯示數值與個別測量時間。
- [x] 溫度來源手動設定與失敗退避；不做自動切換。
- [x] 電力歷史依期間自動分桶，首頁顯示三條排插本月總耗電。
- [x] Excel 滴定參數、雙向計算與每日上限整合；只計算，不連動滴定機。
- [x] 首頁及模組頁改為與 K7 一致的高密度深色控制台風格。
- [x] 直接輪詢三台HS300；每台排插可設定秒數、每個插座可獨立啟用，補水／捲棉以電壓在線且電流或功率跨越門檻的完整週期計次。
- [x] 首頁顯示補水、捲棉今日次數，電源頁提供兩張30日每日次數圖。
- [x] 滴定計算工具自滴定頁獨立成模組，加入PO4、NO3及AF KH配方依據；計算結果不會操作滴定機。
- [x] CYD改為只讀Hub `/api/panel/status`，保留ESPHome加密API與OTA，不再依賴Google試算表水質資料。
- [x] 建立ESP32-C6 OpenThread RCP韌體、OTBR部署資產、狀態API及獨立Thread／Matter管理頁。
- [x] Thread維護軟體閘門：唯一C6 USB識別、整顆Flash備份、固定韌體雜湊、刷寫讀回驗證、回滾、OTBR安裝／修復／重啟與受限Hub控制。
- [ ] Thread實機驗收：C6首次刷寫、OTBR組網及HA Matter配對（`hub-v0.9.2` 發布後執行）。

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
- [x] Google水質鏡射已退役；Hub SQLite為正式資料來源。
- [x] 獨立小魚未來水溫查詢與歷史（實機需設定 `XIAOYU_URL`）。
- [x] 滴定領域模型、模擬器與完整介面（BLE實機傳輸仍屬Phase 10）。
- [x] 建立ESP32-C6 RCP `rcp-v0.1.2`，Release資產SHA-256固定並由刷寫器強制驗證。
- [x] 建立Thread維護systemd／polkit閘門及Hub一鍵刷寫、回滾、OTBR安裝與重啟介面。

## 2026-10-02 JEBAO LAN 實機整合

- 新增 `/jebao/` 與首頁入口；只讀 UDP 探索及 TCP 狀態，每 30 秒讀取，不提供設備控制。
- 登錄 MDP-10000、GMP-30 ×2、DLW-20；實機 MAC 驗證、未知模型拒絕、斷線保留舊值並標示過期。
- `go test ./...`、JavaScript 語法、Linux arm64 建置通過；瀏覽器頁面已驗證。
- 已部署 Pi 192.168.0.149：hub-v0.12.0-jebao.1；備份 hub-20261002T053937Z.db，保留 hub-v0.12.0 回滾。
- Pi 三台成功且下一輪 last_success 前進：MDP 83%、GMP（E1:00）35%、DLW 40%；另一台 GMP（C7:88）未回應。設定百分比不是實測流量，尚待 App 核對。
- 已保存三台 IP 至 data/jebao.json；實機輸出在工作區 JEBAO/REEFHUB_LAN_STATUS_20261002.json。
- 未發布 GitHub release；原 auto_update=true 保留。未來較新正式版本若不含此次修改，可能覆蓋監控功能。
- 既有 Hub 資料庫健康檢查偶發 503，原 0.12.0 日誌亦有 database locked／OTA health failure；JEBAO 讀取不依賴此資料庫，三台仍持續更新。此既有問題另待排查，不宣稱全服務健康已穩定。

## 2026-10-02 主馬速度控制

- 使用者回報一次性 LAN 80% 測試在 App 看起來成功；是否使用行動網路未明，因此只記錄 App 核對成功，不宣稱排除 App 本地連線後的雲端驗證。
- 新增 MDP-10000 主馬速度輸入與套用，POST /api/hub/jebao/return-pump/speed；MAC + product key 核對、1–100 整數、關閉／餵食／排程拒絕、單欄位 flag、ACK 與讀回確認、不自動重送。
- 造浪仍只讀，沒有啟停／模式／餵食／排程控制。寫入不依賴資料庫。
- 全套 go test ./... 與 JS 語法檢查、arm64 建置通過；部署 hub-v0.12.0-jebao.2，保留 jebao.1 回滾，資料庫未更動。
瀏覽器實測：套用主馬速度 80% → 設備 ACK → 讀回 80% → 頁面顯示確認成功；截图 JEBAO/REEFHUB_CONTROL_20261002.png。
2026-10-02：依使用者確認將 wavemaker-1 名稱改為 06-GMP30R、wavemaker-2 改為 13-GMP30L，MAC 配對不變；部署 jebao.3 並以實機 API 驗證名稱。

## 2026-10-02 主馬餵食控制（待實機測試）
新增開始／結束餵食按鈕與 POST /api/hub/jebao/return-pump/feeding，JSON {"enabled":true} 或 false。只寫 FeedSwitch id=2，沿用 FeedTime，不寫速度／模式／排程；開始時拒絕關機或排程啟用，結束允許清除餵食旗標。ACK + 讀回確認，不自動重送。AutoMode 可由設備自行變化。未宣稱實際停轉／降速、倒數、恢復與雲端行為已驗證。
完整測試、模擬開始／結束握手與單一欄位寫入測試通過。部署 hub-v0.12.0-jebao.4；僅驗證頁面與本地狀態，未觸發實機餵食，待使用者回家自行測試。

2026-10-02：Hub 首頁與 dosing、calculator、power、jebao、water、thread、system 全面套用黑底專業控制台視覺；K7 維持獨立既有介面。新增日覽／夜覽／依裝置時間自動切換，選擇保存在瀏覽器本機。Jebao 頁面使用四欄設備網格，支援 3–4 台造浪與主馬配置，平板兩欄、手機一欄。本機測試 8 頁 × 6 寬度、主題保存、斷線表單及 JS 語法通過，arm64 建置後直接部署 `hub-v0.12.0-jebao.5-local-ui`，未上傳 GitHub；`.4` 保留為回滾目標。既有 SQLite `/healthz` 仍回 503，頁面、storage API、Jebao API 與服務維持可用，需另案處理。

## 2026-10-02 Cockpit 實作與 Jebao 歷史

- 依實際瀏覽器驗收重建首頁為高密度水族控制台：即時水溫與能源曲線、最新水質、生命維持設備、四台 Jebao 摘要及模組入口；全 Hub 頁面共用日／夜設計，K7 資產與功能未更動。
- Jebao 四台設備各有連線、設定值、模式、IP、最後回報、故障與 24 小時／7 天／30 天歷史曲線；主馬原有速度與餵食控制保留，造浪維持唯讀。
- 新增 `jebao_samples`，每 30 秒寫入全部四台設備，包含離線／舊資料；新增 `/api/hub/jebao/history` 與 storage status 樣本數。Retention、備份及健康 schema 檢查涵蓋新資料表。
- 將 `/healthz` 改為核心 schema 唯讀檢查，避免 HA 多年歷史回填持有 SQLite writer lock 時把正常服務誤判為失敗；連續健康檢查已通過。
- `go test ./...`、JavaScript 語法及 8 頁 × 6 寬度瀏覽器測試通過。直接部署 `hub-v0.12.0-jebao.6-cockpit` 到 192.168.0.149，服務 active、版本已標記 CONFIRMED、`/`、`/jebao/`、`/K7/` 均為 200；Jebao 歷史樣本已持續增加。未上傳 GitHub，`.5` 保留為回滾版本。

## 2026-10-02 三欄單屏控制台

- 依實機畫面與參考圖再次重排首頁：桌面使用左側系統／水質、中間水溫／水流／生命維持、右側能源／快速控制／最近紀錄的三欄卡片拼接；移除長頁式大標題與大型模組清單。
- 平板改雙欄，手機改單欄；既有 API、設備操作、日夜模式、Jebao 歷史與 K7 獨立頁面均保留。
- 8 頁 × 6 寬度測試與完整 `go test ./...` 通過，實機視覺驗收後部署 `hub-v0.12.0-jebao.7-console`；服務 active、健康檢查通過，`.6` 保留為回滾版本。未上傳 GitHub。

## 2026-10-02 目標稿一比一資料儀表板

- 依使用者提供的 1680×945 目標圖重建首頁：完整頂部導覽、系統狀態、水溫雙圖、即時功率、本月耗電、七項水質、生命維持、三頭滴定、補水／捲棉、Raspberry Pi 與四台 Jebao 水流紀錄採相同比例的四層滿版配置。
- 首頁全部卡片使用現有 API 真實資料；服務重啟後的水溫首次輪詢期間，會顯示 SQLite 最後一筆有效水溫，避免把暫態 `0` 當成缸溫。資料採樣與設備控制邏輯未更動。
- 移除前一版不符合目標稿的水族照片素材。K7 路由與資產維持原狀；其餘模組沿用共用黑色控制台主題與日覽／夜覽／自動模式。
- 8 頁 × 6 寬度瀏覽器測試與 JavaScript 語法檢查通過；Go 套件除 Windows 既有的 symlink 權限測試外通過。部署 `hub-v0.12.0-jebao.8-target-dashboard`，服務 active、`/healthz`、`/K7/` 均為 200。四台 Jebao（包含離線設備）實機各有 665 筆 24 小時紀錄，總樣本 2,636 筆並持續增加。未上傳 GitHub，`.7-console` 保留為回滾版本。

## 2026-10-02 全模組介面統一

- 在首頁及 K7 完全不調整的前提下，將滴定、計算、電源、Jebao、水質、Thread／Matter、系統七個頁面套用首頁的黑底、青色細框、緊湊資訊卡、狀態色與頂部導覽風格。
- 僅修改 `module.html` 共用外殼與 `console-v2.css` 模組頁樣式；API、資料處理、事件綁定、設備控制、表單送出及儲存邏輯均未更動。
- 七個頁面逐頁以 1680×945 檢查，另完成 8 頁 × 6 寬度瀏覽器測試、JavaScript 語法與 `internal/hubweb` Go 測試，無溢位或頁面錯誤。
- 直接部署 `hub-v0.12.0-jebao.9-module-dashboard`；服務 active、健康檢查正常，首頁、K7 與七個模組頁均回應 200。未上傳 GitHub，`.8-target-dashboard` 保留為回滾版本。

## 部署結果
已部署 hub-v0.12.0-jebao.10-wave-flow（local-wave-flow, 2026-10-03），current 指向正確 releases 目錄，healthz HTTP 200；保留 jebao.9 作為 PREVIOUS。新版頁面三台造浪強度表單均完成載入，仍保留歷史曲線與主馬速度／餵食控制。
13-GMP30L 仍為 35%，DLW-20 仍為 40%，06-GMP30R 未連線；未執行實機造浪寫入。Docker 服務運行，但 docker ps 目前無執行中的容器。
首次部署因初始化超過 20 秒而回滾，重試時發現部署目錄文字替換誤將 .10 改寫為 .9-module-dashboard0；已修正並重新安裝至正確 .10-wave-flow。後續初始化及健康正常。多餘目錄保留未刪除，不影響 current。

## 2026-10-03 已確認版面實作與部署（jebao.11）

- 首頁滴定狀態由原先只顯示前三頭改為完整顯示 ALK、Ca、Mg、NP 四個滴定頭；沿用既有 `/api/hub/dosing` 資料與操作邏輯。
- `/power/` 在桌面寬度將三組排插詳情、三組能耗摘要、三張趨勢圖與三組設備對應設定固定為一排三欄；窄螢幕仍依既有響應式規則收合。
- `/jebao/` 第一排固定四台設備狀態與歷史，第二排固定四張個別設定卡。主馬保留轉速與餵食控制，三台造浪保留個別強度控制，四台設備均可個別或一次儲存 IP。
- 沒有新增或修改 API、設備寫入規則、資料庫結構或操作流程；Jebao 四台設備每 30 秒歷史紀錄持續運作，離線設備仍會留下樣本。
- `/K7/` 的頁面與資產未修改。
- UI 驗證通過：8 個頁面 × 320、390、768、1024、1366、1920 六種寬度，無水平溢位、無 JavaScript 頁面錯誤；另驗證首頁 4 個滴定頭、Power 桌面三欄、Jebao 4 狀態卡與 4 設定卡。
- Go 測試通過：`go test ./internal/hubweb`、`go test ./internal/jebao ./internal/storage`。
- 已直接部署 `hub-v0.12.0-jebao.11-approved-layout` 到樹莓派；服務 active、`/healthz` 正常，首頁、Power、Jebao、K7 與其餘模組頁均回應 200。保留 `.10-wave-flow` 作為回滾版本。
- 未上傳 GitHub。

2026-10-03 jebao.12-release-history：嵌入 jebao.1–.12 本地版號更新紀錄，更新歷程 API 合併 GitHub 與本地紀錄，離線保留本地摘要；視窗區分本地部署與 GitHub 發布。完整 Go／JS 檢查通過，部署 jebao.12，保留 jebao.11 回滾；實機 API 已核對 12 筆本地紀錄。未發布 GitHub。

## 2026-10-03 正式版 1.0.0

- 修正目標首頁 CSS 隱藏右上角版號與「檢查更新」的問題，兩個操作恢復為緊湊按鈕並保留原 OTA 流程。
- 盤點 Git 標籤、GitHub Releases、README、`PROGRESS.md` 與直接部署紀錄，建立 49 筆內嵌版本紀錄：36 個既有 Hub 正式版、12 個 JEBAO 本地迭代及 `hub-v1.0.0`。
- 版本歷程在 GitHub 暫時不可用時仍完整顯示；連線成功時會合併正式發布時間與 Release 連結，且不重複同一版號。
- 正式版號由 0.x 開發階段提升為 `hub-v1.0.0`，作為新介面與 JEBAO 整合後的 1.x 起點。
- K7 資產與控制邏輯未修改。
