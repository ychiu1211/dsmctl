# dsmctl MCP Server 管理介面指南

這個介面管理的是 **dsmctl MCP Server**，不是 DSM 本身。MCP Server
管理員、每台 NAS 的 DSM 帳號，以及 MCP 用戶端 Token 是三個彼此獨立的
信任邊界。

## 1. 第一次初始化

1. 開啟 `http://<gateway-host>:<port>/admin/`。
2. 在服務啟動後一小時內建立 MCP Server 管理員。密碼至少 12 個字元；頁面會
   同時顯示絕對期限與剩餘時間。
3. 若設定期限已過，重新啟動尚未初始化的 container 或套件，再完成設定。

這個管理員只可進入 MCP Server 管理介面，不會自動取得 Host NAS 或其他
DSM 的權限。如果第一次開啟時已顯示完成初始化，而且不是你執行的，請停止
使用並清除該部署的 Gateway 資料後重新初始化。

## 2. 登入與語言

使用第一步建立的本機管理員登入。登入頁與管理介面右上角都可切換 English、
繁體中文、简体中文、日本語與 Deutsch；語言偏好不屬於登入狀態。

## 3. 新增與登入 NAS

1. 前往「NAS 管理」，輸入容易辨識的 Profile 名稱。
2. 輸入 container 可連線的 DSM URL，例如 `https://192.168.1.20:5001`。
3. 選擇 TLS 驗證：正式 CA 使用 `System CA`；自簽憑證使用
   `Pinned fingerprint` 並核對 SHA-256 fingerprint。
4. Profile 名稱建立後不可更改。建立後依頁面提示執行 `Web Login`，或開啟
   遮罩的「密碼/OTP」表單，連同實際 DSM 帳號完成登入；成功時會註冊本服務
   為該 NAS 的 trusted device。
5. Profile 列表會顯示 DSM 實際登入的帳號。可編輯 URL、TLS 與 timeout，並按
   「測試」確認連線。管理頁不設定預設 NAS；預設解析只保留給本機 CLI/stdio。

每台 NAS 都必須個別新增、個別登入。即使 MCP Server container 跑在
Synology NAS 上，也不會自動知道或信任 Host NAS；請用該 NAS 的 LAN IP
或 DNS 名稱新增它，`localhost` 只代表 container 自己。

## 4. 建立 MCP Token

前往「MCP 存取」，設定 Token 名稱、NAS allowlist 與最小必要 Scope：

- `nas.read`：讀取 allowlist 內 NAS 的狀態與資料。
- `nas.plan`：建立變更計畫，但不執行。
- `nas.apply`：套用已建立的計畫並允許變更 NAS。
- `lan.discover`：只允許 LAN 裝置探索；探索結果可能超出 NAS allowlist，因此
  使用獨立前綴。Gateway 管理權限不是 MCP Scope。

Allowlist 留空代表不能存取任何 NAS。Bearer Token 只在建立或輪替後顯示
一次，請立即保存到 MCP 用戶端的秘密儲存區。用戶端連至 `/mcp`，並送出：

```http
Authorization: Bearer <token>
```

Token 預設永不到期，也可選 30、90 或 365 天。列表會顯示 Token ID、建立時間、
到期日與最近使用時間，並可立即到期、輪替或撤銷。Scope 與 Allowlist 建立後
不可編輯；要改權限請發行新 Token 或輪替。刪除 NAS Profile 會在同一個交易中
把名稱從所有 Token Allowlist 移除，重建同名 Profile 不會恢復舊存取權。

遠端 NAS 工具每次都必須明確提供 `nas`。只有 `list_nas`、
`discover_lan_devices` 與不指定目標的 `get_auth_status` 可省略。

## 5. 核准高風險操作

高風險 Apply 需要管理員另外建立一次性核准。已驗證 Token 成功建立高風險
Plan 後，「核准」頁會顯示待核准摘要、NAS、Token 與剩餘時間；按一次即可建立
標準核准，或忽略該請求。待核准請求最多 50 筆、24 小時到期，且不會改變真正
的 Apply admission 檢查。

手動備援只需輸入 64 位十六進位 Plan hash，並從現有 NAS Profile 與有效 Token
下拉選單選取；Profile revision 由伺服器在建立核准時擷取，不需人工抄寫。
核准最長有效十分鐘、使用一次後失效。若誤建核准，立即撤銷或輪替 requesting
Token，Apply admission 會重新檢查 Token 狀態。

## 6. Audit 與管理員

「稽核」頁以時間、Actor、Action、Tool/NAS 與結果表格顯示，畫面由新到舊並可
使用 `after`、`actor_id` 與 `action` 篩選。JSONL 匯出會包含所有保留事件，依
時間由舊到新排列，不只匯出目前頁面；密碼、Session 與 Bearer Token 不會出現。

「MCP Server 管理員」頁可變更本機管理密碼、登出其他裝置或登出目前
Session。變更管理密碼必須再次確認至少 12 個字元的新密碼，並會撤銷其他管理員
Session，但不會更改任何 DSM 帳號。

## 建議啟用順序

1. 建立本機 MCP Server 管理員。
2. 逐台新增 NAS，完成 DSM 登入並測試連線。
3. 為每個 MCP 用戶端建立獨立、最小權限的 Token。
4. 先以 `nas.read` 驗證資料，再按需要開放 `nas.plan`、`nas.apply`。
5. 定期檢查 Audit，撤銷不再使用的 Token 與管理員 Session。

## 介面設計圖

以下畫面由實際 `linux/amd64` 隔離測試 container 擷取；其中 NAS、Token
與核准資料皆為虛構示範資料，沒有連線任何真實 DSM。

### 第一次設定

![第一次設定](assets/gateway-admin/01-setup.png)

### 登入

![登入](assets/gateway-admin/08-login.png)

### 總覽

![總覽](assets/gateway-admin/02-overview.png)

### NAS 管理

![NAS 管理](assets/gateway-admin/03-nas.png)

### MCP 存取

![MCP 存取](assets/gateway-admin/04-mcp-access.png)

### 高風險核准

![高風險核准](assets/gateway-admin/05-approvals.png)

### Audit

![Audit](assets/gateway-admin/06-audit.png)

### MCP Server 管理員

![MCP Server 管理員](assets/gateway-admin/07-administrator.png)
