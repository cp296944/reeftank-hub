# 魔點四頭滴定能力矩陣

來源為使用者提供的 `app-release.apk` 靜態分析。軟體模擬已完成，但在實機封包擷取前，所有 BLE 欄位都維持 `false`，不得宣稱已控制實機。

| 功能 | APK 證據 | 模擬／UI | BLE 實機 |
|---|---|---:|---:|
| 四泵頭 | `DosingChannel` | ✅ | 待驗證 |
| 手動／臨時滴定 | `Manual dosing`, `tempDosing` | ✅ | 待驗證 |
| 校正 | `DosingCalibrateWidget` | ✅ | 待驗證 |
| 管路填充 | `Priming` | transport/API 已保留 | 待驗證 |
| 每日總量／分次 | `DosingQuantityWidget`, `Dosing periods` | ✅ | 待驗證 |
| 星期排程 | `add_period_widget` | ✅ | 待驗證 |
| 補充劑間隔 | APK 明示 30 秒避免交互作用 | 模型已記錄 | 待驗證 |
| 漏滴補償 | APK 明示需電池 | capability 已記錄 | 待驗證 |
| 容器與剩餘量 | bottle/volume assets | ✅ | 待驗證 |
| 錯誤流程 | timeout/disconnect/error/partial | ✅ | 待驗證 |

實機驗證順序固定為：唯讀掃描 → 讀取狀態 → 校時 → 最小校正／填充 → 0.1–1 mL 安全劑量 → 排程。未驗證命令不會切換為正式 transport。
