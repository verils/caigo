# Caigo

```text
 ██████╗  █████╗ ██╗ ██████╗  ██████╗
██╔════╝ ██╔══██╗██║██╔════╝ ██╔═══██╗
██║      ███████║██║██║  ███╗██║   ██║
██║      ██╔══██║██║██║   ██║██║   ██║
╚██████╗ ██║  ██║██║╚██████╔╝╚██████╔╝
 ╚═════╝ ╚═╝  ╚═╝╚═╝ ╚═════╝  ╚═════╝
           ░░░ Autonomous Agent ░░░
```

一个用 Go 编写的终端 AI 编程助手。

> Rust 很好，但世界需要 Go ！

## 特性

- **ReAct 智能体** — 推理与行动交替执行，支持多轮工具调用
- **终端 UI** — 全屏交互界面，实时流式输出
- **内置工具** — 文件读写、Bash / PowerShell 命令执行
- **OpenAI 兼容** — 支持任意 OpenAI 兼容 API（OpenAI、MiMo 等）

## 快速开始

```bash
go build -o caigo ./cmd
./caigo
```

首次启动时会交互式引导配置 API 地址和密钥，配置保存在 `~/.caigo/config.json`。

## 配置

`~/.caigo/config.json` 示例：

```json
{
  "model": "mimo-v2.5-pro",
  "providers": {
    "xiaomi-mimo": {
      "name": "Xiaomi MiMo",
      "baseUrl": "https://api.example.com/v1",
      "apiKey": "sk-xxx",
      "type": "openai-compatible"
    }
  },
  "models": {
    "mimo-v2.5-pro": {
      "name": "MiMo V2.5 Pro",
      "provider": "xiaomi-mimo",
      "contextWindowSize": 128000
    }
  }
}
```

通过环境变量 `CAIGO_MODEL` 可切换使用的模型。

## 架构

```
cmd/caigo.go              入口
internal/
  agent/                   ReAct 智能体循环
  config/                  配置加载与解析
  message/                 消息类型定义
  model/                   模型接口
    openai/                OpenAI 兼容流式客户端
  session/                 会话状态管理
  tool/                    工具接口与内置工具实现
  tui/                     终端界面
```
