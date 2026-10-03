# JEBAO LAN 監控與主馬速度控制

入口 `/jebao/`，主馬速度控制 `POST /api/hub/jebao/return-pump/speed`，JSON `{"speed":80}`。只接受整數 1–100，核對 MAC 與 product key，餵食／排程啟用或主馬關閉時拒絕；只選取 Motor_Speed flag，ACK 與讀回均確認才回報成功，寫入結果不明時不重送。狀態 `GET /api/hub/jebao`。MDP-10000、GMP-30 ×2、DLW-20 依使用者提供的 MAC 登錄。每 30 秒探索已知 IP 並讀取本地狀態，啟動及每 5 分鐘重新廣播探索。網頁每 10 秒更新快取；手動「重新探索並讀取」不改動設備。

探索 UDP 12414，TCP 12416；passcode/login 0x06/0x08、只讀 0x90 action=0x02。新增已實測 MDP-10000 主馬速度控制；造浪、模式、餵食與排程仍不提供控制，沒有雲端帳密。連線前用 UDP 身分確認 MAC，unknown product key 不猜測配對；讀回格式必須符合模型長度。

模型包含上游 chrisc123 的 18 份定義，固定來源 commit `bee59f5ce40cb2d915450f61f728e66a073fae09`。來源與 MIT 授權保存於 `internal/jebao/THIRD_PARTY_LICENSE.txt` 並嵌入二進位。GMP Pro 的 uint8 模式標籤依上游 device_configs.json 的對照；不同型號保留不同編碼，不依市售名稱推定。

IP 設定 `GET/PUT /api/hub/jebao/settings`，格式 `{"hosts":{"return-pump-1":"192.168.0.55"}}`；只接受登錄設備 ID 及 private IPv4。空 hosts 使用自動探索，寫入 data-dir/jebao.json，重啟與 OTA 保留。指定 IP 仍需回應正確 MAC；跨 VLAN 若 UDP 被阻擋也不能完成識別。

讀取失敗保留最後成功值，但 connected=false、stale=true；超过 90 秒的值也標為過期。未知故障旗標不顯示正常。`verified=false` 表示尚未與 App／使用者確認現場讀值；connected=true 只代表該次本地讀取及格式解析成功。設定速度百分比不是實測流量。

## 2026-10-02 本機 LAN 測試

| 設備 | IP | Product key | 當次結果 |
|---|---|---|---|
| MDP-10000 | 192.168.0.55 | 02039876751049deb404d1d89221ec4b | 開啟、速度設定 83%、餵食未啟用、排程未啟用 |
| GMP-30（MAC 88:56:A6:FC:E1:00） | 192.168.0.65 | 50dbc92221fd4d33ae69a1fedd43b555 | 開啟、流速設定 35%、模式原值 6、獨立 |
| DLW-20 | 192.168.0.214 | 54114ccdac1e41c0bb17e222887c07ba | 開啟、流速設定 40%、隨機造浪、排程啟用 |
| GMP-30（MAC 1C:DB:D4:12:C7:88） | 未取得 | 未取得 | 尚未回應探索，不顯示模擬值 |

三台當次七項 fault flags 均為 false；此為設備回報，不等於對實際運轉／感測能力的保證。原始測試輸出位於工作區 JEBAO/LAN_STATUS_20261002.json，不當成程式預設狀態。

開發驗證：完整 go test ./...（Windows 需 symlink 權限）、分段／合併封包、跨 byte Linkage、短狀態拒絕、只讀握手命令、MAC 不符禁止 TCP、離線保留舊值、IP 設定與重啟持久化；另需 Raspberry Pi arm64 建置。

## 2026-10-02 JEBAO LAN 實機整合

- 新增 `/jebao/` 與首頁入口；只讀 UDP 探索及 TCP 狀態，每 30 秒讀取，不提供設備控制。
- 登錄 MDP-10000、GMP-30 ×2、DLW-20；實機 MAC 驗證、未知模型拒絕、斷線保留舊值並標示過期。
- `go test ./...`、JavaScript 語法、Linux arm64 建置通過；瀏覽器頁面已驗證。
- 已部署 Pi 192.168.0.149：hub-v0.12.0-jebao.1；備份 hub-20261002T053937Z.db，保留 hub-v0.12.0 回滾。
- Pi 三台成功且下一輪 last_success 前進：MDP 83%、GMP（E1:00）35%、DLW 40%；另一台 GMP（C7:88）未回應。設定百分比不是實測流量，尚待 App 核對。
- 已保存三台 IP 至 data/jebao.json；實機輸出在工作區 JEBAO/REEFHUB_LAN_STATUS_20261002.json。
- 未發布 GitHub release；原 auto_update=true 保留。未來較新正式版本若不含此次修改，可能覆蓋監控功能。
- 既有 Hub 資料庫健康檢查偶發 503，原 0.12.0 日誌亦有 database locked／OTA health failure；JEBAO 讀取不依賴此資料庫，三台仍持續更新。此既有問題另待排查，不宣稱全服務健康已穩定。

## 2026-10-02 主馬餵食控制（待實機測試）
新增開始／結束餵食按鈕與 POST /api/hub/jebao/return-pump/feeding，JSON {"enabled":true} 或 false。只寫 FeedSwitch id=2，沿用 FeedTime，不寫速度／模式／排程；開始時拒絕關機或排程啟用，結束允許清除餵食旗標。ACK + 讀回確認，不自動重送。AutoMode 可由設備自行變化。未宣稱實際停轉／降速、倒數、恢復與雲端行為已驗證。
完整測試、模擬開始／結束握手與單一欄位寫入測試通過。部署 hub-v0.12.0-jebao.4；僅驗證頁面與本地狀態，未觸發實機餵食，待使用者回家自行測試。

2026-10-03：新增造浪強度控制 POST /api/hub/jebao/wavemakers/{id}/flow（flow 整數 1–100）。GMP Pro id=4、DLW id=8；拒絕關機、餵食、排程與聯動狀態，核對 MAC+product key、ACK+讀回，不改模式／排程、不自動重送。尚未進行實機強度寫入，頁面標示待現場測試。盤點 JEBAO/PI_INVENTORY_20261003.md。
