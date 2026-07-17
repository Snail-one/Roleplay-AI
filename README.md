# RoleLoom

RoleLoom 是一个个人自托管、单管理员、多角色的网页角色扮演应用。角色、会话、完整消息和滚动记忆保存在 SQLite；模型密钥使用本地主密钥加密后保存。第一版只提供 Go Web 服务，不包含 CLI、Telegram 或多用户注册。

## 功能

- 管理 OpenAI、OpenAI-compatible、DeepSeek、Anthropic/Claude 和 MiMo 模型档案
- 创建带独立人设、场景、开场白、示例对话及可选工具的角色
- 创建会话时冻结角色快照，后续编辑角色不会改变旧会话
- 原生 SSE 流式回复，支持停止、重试、编辑最新用户消息、截断删除和重新生成
- 长对话达到上下文窗口约 70% 时生成滚动摘要，同时永久保留数据库原始消息
- Argon2id 管理员密码、24 小时 HttpOnly 严格同站 Cookie、登录/聊天限流和同源校验
- SQLite WAL、外键、事务迁移和无 CGO 的纯 Go 驱动

## 环境要求

- Go 1.26+
- Node.js 24 LTS（仅构建或开发网页时需要）

## 配置与启动

复制示例配置；旧版包含 `api`、`agent` 或 `telegram` 的配置不兼容，也不会自动导入。

```bash
cp config.example.json config.json
```

`config.json` 只包含服务部署设置：

```json
{
  "server": {
    "address": "127.0.0.1:8080",
    "database_path": "data/roleloom.db",
    "master_key_path": "data/master.key",
    "static_dir": "web/dist",
    "secure_cookie": false
  },
  "log": { "level": "info" }
}
```

构建网页并启动：

```bash
cd web
npm ci
npm run build
cd ..
go run ./cmd/server
```

首次启动时，终端会提示输入并确认管理员密码；输入过程不会显示字符。程序会把 Argon2id 密码哈希存入 SQLite，并自动生成权限为 `0600` 的 `data/master.key`。以后只需运行 `go run ./cmd/server`，无需再次输入密码或设置环境变量。

打开 `http://127.0.0.1:8080`，登录后先创建一个模型档案和角色。模型 API URL 必须填写完整端点，例如 `/v1/chat/completions`、`/v1/responses` 或 `/v1/messages`。部署在 HTTPS 后面时应把 `secure_cookie` 设为 `true`，并让反向代理传递正确的 `Host` 和 `X-Forwarded-Proto`。

容器或 systemd 等非交互部署仍可使用环境变量。设置 `ROLELOOM_ADMIN_PASSWORD` 会初始化或主动修改管理员密码，并撤销现有登录；设置 `ROLELOOM_MASTER_KEY` 会覆盖主密钥文件。主密钥错误或文件丢失时，只要数据库中已有加密密钥，服务就会拒绝启动。

## 开发

Vite 把同源 `/api` 请求代理到 `127.0.0.1:8080`：

```bash
# 终端一
go run ./cmd/server

# 终端二
cd web
npm ci
npm run dev
```

后端检查：

```bash
go test -race ./...
go vet ./...
```

前端检查：

```bash
cd web
npm run typecheck
npm test
npm run build
```

## 数据与备份

聊天正文按明文保存，只有模型 API Key 使用 AES-256-GCM 加密，每条记录都有独立随机 nonce。完整备份需要同时保存：

- `database_path` 指向的 SQLite 数据库；在线复制时使用 SQLite 备份工具或先停服
- `master_key_path` 指向的主密钥文件（默认 `data/master.key`），或部署时使用的 `ROLELOOM_MASTER_KEY`
- `config.json`

丢失主密钥无法恢复数据库中的模型密钥。不要把数据库、配置、管理员密码或主密钥提交到版本库。

## 主要 API

- `POST /api/auth/login`、`POST /api/auth/logout`、`GET /api/auth/session`
- `/api/model-profiles` 与 `/{id}/test`
- `/api/characters` 与 `/{id}/avatar`
- `/api/conversations`、`/{id}/messages`、`/{id}/messages/stream`
- `PATCH|DELETE /api/conversations/{id}/messages/{messageID}`
- `POST /api/conversations/{id}/regenerate`、`POST /api/conversations/{id}/stop`

除健康检查、登录和静态文件外，所有 API 都要求登录。生产发布物由一个 Go 二进制和 `web/dist` 组成。
