# New TNFSH Online Judge — NTOJ

前後端分離的線上評測系統。

| 目錄 | 內容 |
| --- | --- |
| `backend/` | Kotlin API 伺服器（Ktor + JDBC），唯一的資料庫是 PostgreSQL |
| `frontend/` | React Router v8（SPA 模式）+ shadcn/ui + Tailwind CSS v4 |
| `docs/judge-api.md` | 評測 worker 與後端之間的協定 |

程式碼執行沙盒不在這個 repo 的範圍內：後端只負責排隊、派發與計分，
實際編譯與執行由外部的評測 worker 透過 [Judge API](docs/judge-api.md) 完成。
沒有設定 `JUDGE_TOKEN` 或沒有 worker 連上時，提交會停留在 Pending。

## 快速啟動（Docker Compose）

```sh
cp .env.example .env      # 填入 DB_PASSWORD、ADMIN_PASSWORD、JUDGE_TOKEN
docker compose up -d --build
```

預設在 `http://localhost:8080`。正式環境請在前面放一層 HTTPS 反向代理，並保持 `COOKIE_SECURE=true`；
只在本機用純 HTTP 測試時才設成 `false`，否則瀏覽器不會送出登入 cookie。

第一次啟動且系統內沒有任何管理員時，會用 `ADMIN_EMAIL` / `ADMIN_PASSWORD` 建立管理員帳號。

## 開發

需要 JDK 21、Node 22+、PostgreSQL 16+（Gradle 由 `./gradlew` 自動下載）。

```sh
# 後端（啟動時自動套用 backend/src/main/resources/db/migrations 內的 migration）
cd backend
DATABASE_URL=postgres://ntoj:ntoj@localhost/ntoj COOKIE_SECURE=false \
ADMIN_EMAIL=admin@example.com ADMIN_PASSWORD=change-me-please \
./gradlew run

# 前端（Vite 會把 /api 代理到 http://localhost:8080，因此與正式環境一樣是同源）
cd frontend
npm install
npm run dev
```

測試：

```sh
cd backend
./gradlew test                                                        # 單元測試
TEST_DATABASE_URL=postgres://ntoj:ntoj@localhost/ntoj_test ./gradlew test   # 含 API 整合測試（會清空該資料庫）

cd frontend
npm run typecheck && npm run build
```

## 後端設定（環境變數）

| 變數 | 預設 | 說明 |
| --- | --- | --- |
| `DATABASE_URL` | 必填 | PostgreSQL 連線字串（`postgres://user:pass@host/db` 或 `jdbc:postgresql://…`） |
| `LISTEN_ADDR` | `:8080` | 監聽位址 |
| `DATA_DIR` | `./data` | 測資與題目附件的儲存位置 |
| `SITE_TITLE` | `NTOJ` | 網站名稱 |
| `COOKIE_SECURE` | `true` | session cookie 是否加上 `Secure` |
| `ALLOW_SIGNUP` | `true` | 是否開放自行註冊 |
| `SUBMIT_COOLDOWN` | `5` | 競賽外同一帳號兩次提交的最短間隔（秒） |
| `TRUSTED_PROXIES` | 空 | 可信任的反向代理 IP / CIDR（逗號分隔）；只有來自這些位址的 `X-Forwarded-For` 會被採用 |
| `ALLOWED_ORIGINS` | 空 | 前端與 API 不同源時，額外允許的 Origin |
| `JUDGE_TOKEN` | 空 | 評測 worker 的共享密鑰（至少 32 字元）；留空則停用 Judge API |
| `ADMIN_EMAIL` / `ADMIN_NAME` / `ADMIN_PASSWORD` | 空 | 首次啟動時建立的管理員 |

## 功能

- 帳號：註冊、登入、個人資料、登入裝置列表與遠端登出、管理員停權 / 綁定 IP / 重設密碼
- 題目：Markdown + LaTeX 題敘、附件、測資上傳、子任務（group-min 與相依）、標籤、語言限制
- 提交：列表與篩選、逐子任務 / 逐測資結果、重測
- 競賽：IOI / ICPC 賽制、固定或彈性開始時間、三種報名模式、封版、提問與公告、
  每題結果揭露程度、比賽期間限制瀏覽其他頁面、管理員與稽核紀錄
- 其他：公告、題單（官方 / 個人、可收藏）、排行榜、Board、對管理員提問、稽核紀錄

## 安全性設計

- 密碼以 argon2id 雜湊，最少 8 字元；登入對不存在的帳號也花費相同時間
- session token 為 256-bit 亂數，資料庫只存 SHA-256；cookie 為 `HttpOnly`、`SameSite=Lax`、`Secure`；
  改密碼會登出其他裝置
- 所有會改變狀態的跨站請求一律拒絕（`Sec-Fetch-Site` / `Origin` 檢查），作為 CSRF 防線
- 登入、註冊、改密碼、提問皆有速率限制（單一行程記憶體內）
- 所有 SQL 皆為參數化查詢；請求 body 有大小上限且拒絕未知欄位
- 上傳的測資以伺服器產生的檔名儲存；附件檔名受白名單限制，
  除圖片 / PDF / 純文字外一律以下載方式提供，並帶 `nosniff` 與 sandbox CSP
- Markdown 不解譯原始 HTML，`javascript:` 等連結會被移除
- 隱藏題目與他人原始碼對無權限者回應 404；管理員檢視他人原始碼會寫入稽核紀錄
- 評測分數一律由後端依子任務設定計算，不採信 worker 回報的分數

已知限制：速率限制不跨行程共享；前端 CSP 因 React Router 的 inline bootstrap script 而允許 `'unsafe-inline'`。

## 從舊版（Python / Tornado）升級

這次是完整重寫，資料庫 schema 重新設計，**沒有提供舊資料的自動搬移**。
舊版程式碼保留在 git 歷史中。與舊版的主要差異：

- 不再需要 Redis
- 題敘改存 Markdown（舊版為 HTML / PDF 檔案）；PDF 可當附件上傳後在題敘中連結
- 只支援 batch 題型；communication 題型、自訂 checker 的編譯、system test 與 TOJ 題目包匯入
  都和評測沙盒綁在一起，尚未移植
- 即時更新由 WebSocket 改為前端輪詢
