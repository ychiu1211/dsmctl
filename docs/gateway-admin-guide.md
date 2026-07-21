# dsmctl MCP Server 管理介面指南

這個介面管理的是 **dsmctl MCP Server**，不是 DSM 本身。MCP Server
管理員、每台 NAS 的 DSM 帳號，以及 MCP 用戶端 Token 是三個彼此獨立的
信任邊界。

## 1. 第一次初始化

1. 開啟 `http://<gateway-host>:<port>/admin/`。
2. 在服務啟動後一小時內建立 MCP Server 管理員。密碼至少 8 個字元，強度由
   使用者自行負責；頁面會同時顯示絕對期限與剩餘時間。
3. 若設定期限已過，重新啟動尚未初始化的 container 或套件，再完成設定。

這個管理員只可進入 MCP Server 管理介面，不會自動取得 Host NAS 或其他
DSM 的權限。如果第一次開啟時已顯示完成初始化，而且不是你執行的，請停止
使用並清除該部署的 Gateway 資料後重新初始化。

## 2. 登入與語言

使用第一步建立的本機管理員登入。登入頁與管理介面右上角都可切換 English、
繁體中文、简体中文、日本語與 Deutsch；語言偏好不屬於登入狀態。

忘記管理員密碼時無法找回或重設：請清除該部署的 Gateway 資料並重新安裝，
於啟動後一小時內重新完成第一次設定。所有 NAS Profile、Token 與 Audit
紀錄都會一併刪除，登入頁也有相同提示。

## 3. 新增與登入 NAS

1. 前往「NAS 管理」並按「新增 NAS」。沒有任何 Profile 時，列表中央也會顯示
   「新增 NAS」；新增表單平常不會佔據頁面。
2. 精靈第一步會從 MCP Server 所在的 broadcast domain 搜尋 Synology 裝置；
   可選取搜尋結果，也可直接輸入 IP、DNS 名稱或完整 DSM URL。搜尋不跨路由器、
   VLAN 或 VPN；這些情況請手動輸入。
3. 選取裝置或輸入位址後，第二步會帶入容易辨識的 Profile 名稱與 container
   可連線的 DSM URL，例如 `https://192.168.1.20:5001`。搜尋結果不保證知道
   自訂 DSM port，因此 URL 仍必須由管理員確認。
4. 不需要預先選擇 TLS 模式或手動抄寫 fingerprint。系統一律先用 System CA、
   hostname 與有效期間驗證；若 CA、hostname 或有效期間驗證失敗，會列出每一項
   警告，以及 Gateway 觀察到的主體、簽發者、有效期間與 SHA-256 fingerprint，
   詢問是否信任並 pin 這張確切憑證。因此只有內網 IP、且 IP 未列在憑證 SAN 的 NAS
   仍可在管理員明確同意後使用。若拿不到可解析的憑證，或 TLS protocol、密碼學握手、
   網路本身失敗，則不提供 pin。憑證更換後會同時顯示原本與新觀察到的 fingerprint，必須再次確認，
   不會自動取代；修改 DSM URL 也會清除舊端點的 pin，重新從 System CA 驗證。
5. Profile 名稱建立後不可更改。精靈第三步建議使用 `Web Login`；也可開啟
   遮罩的「密碼/OTP」表單，連同實際 DSM 帳號完成登入。成功時會註冊本服務
   為該 NAS 的 trusted device。若先關閉精靈，列表會保留「完成設定」。
6. 新增、編輯、完成設定與重新登入共用同一個三步驟精靈：新增從搜尋頁開始、
   編輯與完成設定從連線頁開始、重新登入從登入頁開始，且登入頁可返回檢查連線。
7. Profile 列表顯示 DSM 實際帳號與「已儲存」的驗證方式。這代表加密認證資料
   存在，不代表 NAS 此刻一定在線。編輯、連線診斷、重新驗證與刪除收在該列的
   更多操作選單；「連線診斷」會依序顯示 DNS、TCP、TLS/HTTP 與 DSM 驗證結果，
   不再以未說明的「測試」或原始 JSON 呈現。
8. 更多操作選單也提供「複製帳號」與「複製密碼」。「複製密碼」只在該 Profile
   以密碼/OTP 登入並存有加密密碼時出現：由已登入的管理員瀏覽器 Session 明確
   觸發，Gateway 才把 vault 內的 DSM 密碼解密後直接放進剪貼簿——密碼不會顯示
   在畫面上，也不會出現在 Audit 內容裡，但每次讀取都會記錄獨立的
   `credential.reveal` Audit 事件。只用 Web Login 登入的 Profile 沒有可複製的
   密碼；環境變數提供的密碼也不在複製範圍。MCP Token 無法呼叫任何
   `/admin/api/` 路徑，因此密碼永遠不會經由 `/mcp` 洩出。複製後剪貼簿即含
   機密，貼上後請自行清空。

在憑證驗證成功或明確確認 pin 之前，Gateway 不會把 DSM 密碼、OTP、Session 或
一次性登入 code 送到 NAS。Gateway 的 pin 只保護 Gateway 到 NAS 的流量；Web
Login popup 是瀏覽器直接連到 DSM，因此瀏覽器仍可能針對自簽憑證顯示自己的警告。

管理頁不設定預設 NAS；預設解析只保留給本機 CLI/stdio。

每台 NAS 都必須個別新增、個別登入。即使 MCP Server container 跑在
Synology NAS 上，也不會自動知道或信任 Host NAS；請用該 NAS 的 LAN IP
或 DNS 名稱新增它，`localhost` 只代表 container 自己。

## 4. 連接 MCP 用戶端

前往「MCP 存取」。建議優先使用頁面上方的 **「使用 MCP URL 連線」**：在支援
MCP OAuth 的用戶端新增遠端 MCP Server，只貼上頁面顯示的完整 `/mcp` URL。
用戶端第一次連線時，Gateway 會開啟瀏覽器授權頁；使用既有的 Gateway 管理員
帳號密碼登入、核對 Client 名稱、回呼位置、NAS 與 Scope，再按「登入並允許」。
用戶端會自動取得 Token，不必從管理頁手動複製秘密。

這個瀏覽器登入使用的是 **Gateway 管理員**，不是 DSM 帳號。密碼只送到同一台
Gateway 的 `/oauth/authorize`，不會交給 MCP 用戶端；DSM 憑證仍只保存在各自的
NAS Profile。OAuth access token 有效一小時，用戶端使用旋轉式 refresh token
自動續期；refresh grant 最長 365 天。管理頁將這類憑證標示為「OAuth URL 登入」。

無頭自動化或不支援瀏覽器 OAuth 的舊用戶端，才使用下方的「建立手動 Token」。
每個 Agent 或裝置應建立自己的憑證。手動表單要求明確勾選可使用的 NAS，不會
因為 Profile 存在就隱含授權。

這是提供給可信任 Power User 的私有服務，因此畫面預設為：

- 勾選目前所有 NAS；
- 「完整權限」，也就是下列四個 Scope 全開；
- 365 天有效期限。

預設權限不等於略過安全確認：Agent 應在真正套用變更前向使用者確認，
高風險 Plan 還必須回到「核准」頁由管理員同意。

- `nas.read`：讀取 allowlist 內 NAS 的狀態與資料。
- `nas.plan`：建立變更計畫，但不執行。
- `nas.apply`：套用已建立的計畫並允許變更 NAS。
- `lan.discover`：只允許 LAN 裝置探索；探索結果可能超出 NAS allowlist，因此
  使用獨立前綴。Gateway 管理權限不是 MCP Scope。

無論 OAuth 或手動模式，Bearer Token 都是 MCP 用戶端的身分；任何持有者都
具有所選權限。手動 Token 只在建立或輪替後顯示一次，請立即保存到 MCP
用戶端的秘密儲存區，不要貼到一般筆記、聊天或瀏覽器儲存空間。完成畫面可
分別複製 Token、絕對 MCP 端點，或通用 Streamable HTTP 設定。HTTP 驗證標頭為：

```http
Authorization: Bearer <token>
```

手動 Token 預設 365 天，也可選 30、90 天或在進階選項中設為不過期。Client
憑證列表顯示「尚未使用」、「曾經使用」、「已過期」或「已撤銷」；這是最近
使用紀錄，不代表維持一條長連線。更多操作可複製 Token ID、立即到期或撤銷；
手動 Token 也可輪替。OAuth 憑證由 refresh flow 自動旋轉，因此管理頁不提供
手動輪替，立即到期或撤銷時也會同時使 refresh token 失效。Scope 與 Allowlist
建立後不可編輯；要改權限請重新授權或建立新憑證。刪除 NAS Profile 會在同一個
交易中把名稱從所有 Token Allowlist 移除，重建同名 Profile 不會恢復舊存取權。

遠端 NAS 工具每次都必須明確提供 `nas`。只有 `list_nas`、
`discover_lan_devices` 與不指定目標的 `get_auth_status` 可省略。

## 5. 核准高風險操作

高風險 Apply 需要管理員另外建立一次性核准。已驗證 Token 成功建立高風險
Plan 後，「核准」頁會顯示待核准摘要、NAS、Token 與剩餘時間；按一次即可建立
標準核准，或忽略該請求。待核准請求最多 50 筆、24 小時到期，且不會改變真正
的 Apply admission 檢查。

「手動備援」平常不佔頁面，只有按下頁面右上角的次要操作才會開啟。此流程只需
輸入 64 位十六進位 Plan hash，並從現有 NAS Profile 與有效 Token 下拉選單
選取；Profile revision 由伺服器在建立核准時擷取，不需人工抄寫。
核准最長有效十分鐘、使用一次後失效。若誤建核准，立即撤銷或輪替 requesting
Token，Apply admission 會重新檢查 Token 狀態。

## 6. Audit 與管理員

「稽核」頁先顯示事件列表；需要時按「篩選條件」，再用 `after`、`actor_id` 與
`action` 縮小畫面內容。JSONL 匯出會包含所有保留事件，依時間由舊到新排列，
不受畫面篩選影響；密碼、Session 與 Bearer Token 不會出現。

「MCP Server 管理員」頁先呈現安全性與目前 Session 狀態；按「變更密碼」才會
開啟表單。新密碼至少 8 個字元且必須再次確認，變更後會撤銷其他管理員
Session，但不會更改任何 DSM 帳號。

## 建議啟用順序

1. 建立本機 MCP Server 管理員。
2. 逐台新增 NAS，完成 DSM 登入並測試連線。
3. 為每個 MCP 用戶端建立獨立連線，確認明確 NAS 清單與預設完整權限是否符合
   該使用者的信任範圍。
4. 在 Agent 端保留 Apply 前確認；高風險操作再使用管理介面的核准流程。
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
