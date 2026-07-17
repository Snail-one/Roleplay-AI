# RoleLoom AI Agent

一个仅使用 Go 标准库实现的基础 AI Agent。它支持 OpenAI、DeepSeek、Claude（Anthropic）、小米 MiMo 及其他 OpenAI 兼容服务的多轮对话，并允许模型自主调用时间和计算器工具。

## 功能

- 终端多轮对话
- OpenAI Chat Completions 与 Responses API
- 其他兼容服务的 Chat Completions 与 Responses 接口
- Claude 原生 Anthropic Messages API
- 小米 MiMo Chat Completions、Responses 和 Anthropic Messages API
- `get_current_time` 时间工具（支持 IANA 时区）
- `calculate` 加、减、乘、除工具
- 工具调用循环与最大轮次保护
- `/reset` 清空上下文，`/exit` 或 `/quit` 退出
- Telegram 机器人聊天，每个聊天独立保存上下文

## 环境要求

- Go 1.26 或更高版本
- 所选模型需要支持对应 API 的工具调用能力

## 配置

首次运行时，如果配置文件不存在，程序会自动生成通用 OpenAI-compatible 配置模板并提示填写 API 信息：

```bash
go run ./cmd/agent
```

程序不会覆盖已有配置。自动生成的文件权限为 `0600`；也可以手动复制 `config.example.json`。

```json
{
  "api": {
    "provider": "openai",
    "api_url": "https://api.openai.com/v1/chat/completions",
    "api_key": "your-api-key",
    "model": "your-model",
    "timeout_seconds": 60,
    "max_output_tokens": 4096
  },
  "agent": {
    "system_prompt": "你是一个简洁、可靠的 AI 助手。",
    "max_iterations": 8
  }
}
```

`api.provider` 支持 `openai`、`deepseek`、`claude`、`mimo` 和 `openai_compatible`；省略时默认为 `openai`，`anthropic` 会归一化为 `claude`。专用提供商需要填写完整的 `api.api_url` 请求地址；`openai_compatible` 可以填写根地址或 API 前缀，程序会自动补全 `/chat/completions`，填写完整的 `/responses` 地址时则直接使用 Responses API。`api.model` 必填，`api.api_key` 对本地免鉴权服务可以留空。`max_output_tokens` 为 `0` 时，Claude 默认使用 4096，Chat Completions 服务则不主动发送限制。超时和最大模型调用轮数设置为 `0` 时分别使用内置默认值 60 秒和 8 轮。

### OpenAI

OpenAI 根据完整地址选择 Chat Completions 或 Responses API：

- `https://api.openai.com/v1/chat/completions`
- `https://api.openai.com/v1/responses`

Responses API 支持完整对话历史、函数调用和函数结果回传，并使用 `Authorization: Bearer` 认证。只填写 `https://api.openai.com/v1` 会报错。

### DeepSeek

DeepSeek 支持 OpenAI Chat Completions 和 Anthropic Messages 两种地址格式。Chat Completions 示例：

```json
{
  "api": {
    "provider": "deepseek",
    "api_url": "https://api.deepseek.com/chat/completions",
    "api_key": "your-deepseek-key",
    "model": "deepseek-v4-pro",
    "timeout_seconds": 60,
    "max_output_tokens": 4096
  }
}
```

使用 Anthropic Messages 时，将地址填写为 `https://api.deepseek.com/anthropic/v1/messages`。

### Claude

Claude 使用原生 Anthropic Messages API：

```json
{
  "api": {
    "provider": "claude",
    "api_url": "https://api.anthropic.com/v1/messages",
    "api_key": "your-anthropic-key",
    "model": "your-claude-model",
    "timeout_seconds": 60,
    "max_output_tokens": 4096
  }
}
```

### 其他兼容服务

通义千问、Moonshot、Ollama 或自建网关等提供 OpenAI 兼容接口时，可选择 `openai_compatible`。地址选择规则：

- 根地址或 API 前缀默认补全 `/chat/completions`，例如 `https://api.deepseek.com` 会变成 `https://api.deepseek.com/chat/completions`。
- 已经以 `/chat/completions` 结尾时保持原样。
- 以 `/responses` 结尾时保持原样并使用 Responses API。
- 以 `/messages` 结尾时明确报错。

通用 Responses 默认使用 OpenAI 兼容的 `Authorization: Bearer` 认证。MiMo Responses 使用 `api-key` 认证，因此调用 `https://api.xiaomimimo.com/v1/responses` 时应选择 `provider: "mimo"`。

### 小米 MiMo

MiMo 根据完整的 `api_url` 自动识别三种 API 格式：

- 以 `/chat/completions` 结尾：使用 Chat Completions。
- 以 `/responses` 结尾：使用 Responses，程序负责转换完整历史和函数调用项。
- 以 `/messages` 结尾：使用 Anthropic Messages 内容块格式。

程序直接请求所填写的完整地址，不会移除、追加或重复拼接端点路径。

使用 Chat Completions：

```json
{
  "api": {
    "provider": "mimo",
    "api_url": "https://api.xiaomimimo.com/v1/chat/completions",
    "api_key": "your-mimo-key",
    "model": "mimo-v2.5-pro",
    "timeout_seconds": 60,
    "max_output_tokens": 4096
  }
}
```

使用 Responses：

```json
{
  "api": {
    "provider": "mimo",
    "api_url": "https://api.xiaomimimo.com/v1/responses",
    "api_key": "your-mimo-key",
    "model": "mimo-v2.5-pro"
  }
}
```

使用 Anthropic Messages：

```json
{
  "api": {
    "provider": "mimo",
    "api_url": "https://api.xiaomimimo.com/anthropic/v1/messages",
    "api_key": "your-mimo-key",
    "model": "mimo-v2.5-pro"
  }
}
```

MiMo 未配置 `api_url`、只填写 API 根地址或填写无法识别的路径时会直接报错。

真实配置文件 `config.json` 已加入 `.gitignore`，不要将真实密钥写入 `config.example.json` 或提交到代码仓库。

## 运行

使用默认的 `config.json`：

```bash
go run ./cmd/agent
```

也可以指定其他配置文件：

```bash
go run ./cmd/agent -config ./configs/local.json
```

示例会话：

```text
> 上海现在几点？
Agent: 上海当前时间是……
> 计算 (12 + 8) / 4
Agent: 计算结果是 5。
> /reset
Agent: 上下文已清空。
```

模型可以在一次回答中调用多个工具；工具参数错误会作为结果反馈给模型，使其有机会自行修正。API 或协议错误不会污染已有会话历史。

## Telegram 机器人

先在 Telegram 中联系 [@BotFather](https://t.me/BotFather)，使用 `/newbot` 创建机器人并取得 Token。然后填写配置：

```json
{
  "telegram": {
    "bot_token": "123456789:your-telegram-bot-token",
    "allowed_user_ids": [123456789],
    "poll_timeout_seconds": 30
  }
}
```

启动机器人：

```bash
go run ./cmd/telegram
```

也可以指定配置文件：

```bash
go run ./cmd/telegram -config ./configs/local.json
```

Telegram 命令：

- `/start`：开始聊天并清空当前上下文
- `/reset`：清空当前聊天上下文
- `/id`：显示当前用户 ID 和聊天 ID
- `/help`：显示帮助

每个 `chat_id` 对应一个独立 Agent，上下文不会在不同私聊或群组之间共享。`allowed_user_ids` 是允许使用机器人的 Telegram 用户 ID；空数组表示允许所有用户，会消耗你的模型 API 配额，不建议用于公开机器人。首次获取自己的 ID 时可以暂时使用空数组，向机器人发送 `/id` 后再把返回的 `user_id` 写入白名单并重启。

机器人使用长轮询 `getUpdates`，因此同一个 Bot Token 不应同时运行多个实例，也不能同时配置 Telegram Webhook。Token 可以完全控制机器人，应保存在权限受限的本地配置中，不要提交到代码仓库。

## 架构

```text
cmd/agent                 CLI 入口
cmd/telegram              Telegram Bot 入口
internal/agent            对话历史与工具调用循环
internal/telegram         Telegram Bot API 与聊天会话调度
internal/ai               统一消息类型、接口和调用客户端
└── provider              提供商工厂与协议适配
    ├── common            共享 Chat Completions 协议
    ├── openai            OpenAI Chat Completions 与 Responses 适配
    ├── deepseek          DeepSeek 配置与适配
    ├── claude            Claude Messages API 适配
    ├── mimo              MiMo 三种 API 适配（Responses 复用 OpenAI 实现）
    └── openai_compatible 其他兼容服务适配
```

Agent 仅依赖 `internal/ai` 的统一接口，提供商实现不会反向依赖 Agent。MiMo 的 Responses 适配按设计复用 OpenAI Responses 实现；新增独立提供商时实现 `ai.Backend` 并在工厂注册即可。

## 验证

```bash
go test ./...
go vet ./...
go build -o bin/roleloom-agent ./cmd/agent
go build -o bin/roleloom-telegram ./cmd/telegram
```
