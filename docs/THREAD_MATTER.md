# ESP32-C6 Thread / Matter 架構

ReefTank Hub 使用 ESP32-C6 作為 USB OpenThread RCP。C6 不執行 Wi-Fi、Matter 或 Home Assistant 邏輯；樹莓派上的 OTBR 保存 Thread 網路並透過有線網路提供 Border Router，裝置最終由 Home Assistant Matter Server 管理。

## 安裝邊界

1. 先將 `firmware/esp32c6-ot-rcp` 編譯並燒錄到 MuseLab nanoESP32-C6。燒錄會清除原本 ESPHome 韌體。
2. 將 C6 插入樹莓派 USB，確認 `/dev/serial/by-id/` 只出現預期的 C6，或明確設定 `RCP_DEVICE`。
3. 確認樹莓派已安裝 Docker 與 Docker Compose，再執行：

   ```bash
   cd /opt/reeftank-hub/current/deploy/thread
   sudo ./install.sh --confirm
   ```

4. 在 Home Assistant 的 Thread 整合加入 OTBR，設為偏好的 Thread 網路並將憑證傳送到手機。
5. 使用 Home Assistant Matter Server及手機替 IKEA Matter-over-Thread 裝置配對。

安裝腳本不會自動安裝 Docker、不會猜測多個USB序列裝置，也不會改動HA。Thread資料保存在 `/opt/reeftank-hub/thread/data`，應納入備份。

## 驗證

- Hub：`/api/hub/thread/status`
- OTBR REST：`http://127.0.0.1:8081/api/node`
- Docker：`sudo docker compose --project-directory /opt/reeftank-hub/thread ps`

若 C6 拔除，Hub只顯示離線；K7、能源、水質及HA原有功能不受影響。
