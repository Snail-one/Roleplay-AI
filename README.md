# RoleLoom AI Agent

一个仅使用 Go 标准库实现的基础 AI Agent。它支持 OpenAI、DeepSeek、Claude（Anthropic）、小米 MiMo 及其他 OpenAI 兼容服务的多轮对话，并允许模型自主调用时间和计算器工具。

## 功能

- 终端多轮对话
- OpenAI 与其他兼容服务的 Chat Completions 接口
- Claude 原生 Anthropic Messages API
- 小米 MiMo Chat Completions、Responses 和 Anthropic Messages API
- `get_current_time` 时间工具（支持 IANA 时区）
- `calculate` 加、减、乘、除工具
- 工具调用循环与最大轮次保护
- `/reset` 清空上下文，`/exit` 或 `/quit` 退出

## 环境要求

- Go 1.26 或更高版本
- 所选模型需要支持对应 API 的工具调用能力

## 配置

首次运行时，如果配置文件不存在，程序会自动生成默认 MiMo 配置并提示填写密钥：

```bash
go run ./cmd/agent
```

程序不会覆盖已有配置。自动生成的文件权限为 `0600`；也可以手动复制 `config.example.json`。

```json
{
  "api": {
    "provider": "openai",
    "base_url": "https://api.openai.com/v1",
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

`api.provider` 支持 `openai`、`deepseek`、`claude`、`mimo` 和 `openai_compatible`；省略时默认为 `openai`，旧配置中的 `anthropic` 会自动归一化为 `claude`。`api.model` 必填，`api.api_key` 对本地免鉴权服务可以留空。`max_output_tokens` 为 `0` 时，Claude 默认使用 4096，Chat Completions 服务则不主动发送限制。超时和最大模型调用轮数设置为 `0` 时分别使用内置默认值 60 秒和 8 轮。

### DeepSeek

DeepSeek 可以省略 `base_url`，程序会使用官方地址：

```json
{
  "api": {
    "provider": "deepseek",
    "api_key": "your-deepseek-key",
    "model": "deepseek-v4-pro",
    "timeout_seconds": 60,
    "max_output_tokens": 4096
  }
}
```

### Claude

Claude 使用原生 Anthropic Messages API，可以省略 `base_url`：

```json
{
  "api": {
    "provider": "claude",
    "api_key": "your-anthropic-key",
    "model": "your-claude-model",
    "timeout_seconds": 60,
    "max_output_tokens": 4096
  }
}
```

### 其他兼容服务

通义千问、Moonshot、Ollama 或自建网关等提供 OpenAI Chat Completions 兼容接口时，可选择 `openai_compatible` 并填写对应的 `base_url` 和模型名。

### 小米 MiMo

MiMo 支持三种协议，通过 `api.protocol` 选择：

- `chat_completions`：默认值，调用 `/v1/chat/completions`。
- `responses`：调用 `/v1/responses`，程序负责转换完整历史和函数调用项。
- `anthropic`：调用 `/anthropic/v1/messages`，使用 Anthropic 内容块格式。

使用 Chat Completions：

```json
{
  "api": {
    "provider": "mimo",
    "protocol": "chat_completions",
    "api_key": "your-mimo-key",
    "model": "mimo-v2.5-pro",
    "timeout_seconds": 60,
    "max_output_tokens": 4096
  }
}
```

使用 Responses API 时只需修改：

```json
"protocol": "responses"
```

使用 Anthropic Messages API 时修改为：

```json
"protocol": "anthropic"
```

MiMo 未配置 `base_url` 时，Chat/Responses 默认使用 `https://api.xiaomimimo.com/v1`，Anthropic 默认使用 `https://api.xiaomimimo.com/anthropic/v1`。如使用其他 MiMo 网关或 Token Plan，可显式填写对应协议的基础地址。

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

## 架构

```text
cmd/agent                 CLI 入口
internal/agent            对话历史与工具调用循环
internal/ai               统一消息类型、接口和调用客户端
└── provider              提供商工厂与协议适配
    ├── common            共享 Chat Completions 协议
    ├── openai            OpenAI 配置与适配
    ├── deepseek          DeepSeek 配置与适配
    ├── claude            Claude Messages API 适配
    ├── mimo              MiMo 三种协议适配
    └── openai_compatible 其他兼容服务适配
```

Agent 仅依赖 `internal/ai` 的统一接口。提供商实现不会依赖 Agent，各厂商之间也没有相互依赖；新增提供商时实现 `ai.Backend` 并在工厂注册即可。

## 验证

```bash
go test ./...
go vet ./...
go build -o bin/roleloom-agent ./cmd/agent
```
