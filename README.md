# Crier

Crier 把邮件和其他信息源里的重要提醒打印成热敏小票。Windows 桌面版内置邮件来源、统一的 AI 设置、测试小票和打印记录。

## 安装与使用

从 [GitHub Releases](https://github.com/Zhuym07/crier/releases) 下载 Windows x64 安装包。首启可选择 ESC/POS 打印机、预览或打印测试小票，再配置邮件和可选的 AI。当前按 58 mm、384 点纸宽排版，打印机需先安装 Windows 驱动。

IMAP 填写带端口的地址，例如 `imap.example.com:993`。首次检查记录当前邮箱进度，从此之后的新邮件开始打印。仅在开启 AI 后，邮件标题和用于摘要的正文才会发送给所配置的 AI 接口。设置中的邮件密码和 API Key 由 Windows DPAPI 保护。

打印记录中的「已提交」表示 Windows 接受了任务。收到小票后可在界面确认。若提交途中出错或进程中断，任务会标记为「结果未知」，便于先检查纸票和系统队列。

## 开发信息源插件

在 `%APPDATA%\Crier\plugins.json` 注册可信的本地程序，各条目的 `id` 唯一，`executable` 为绝对路径。`settings` 会作为 RPC 参数发送，应用会另外填入 `cursor`。`allowAI` 允许该插件的内容使用中央 AI 设置。

```json
[{"id":"calendar","executable":"C:\\Tools\\calendar-crier.exe","settings":{},"allowAI":false}]
```

Crier 启动插件进程，向 stdin 写入一行 JSON-RPC 2.0 请求并关闭 stdin。插件处理 `poll` 后向 stdout 写出一条响应并退出。插件负责生成稳定的事件 ID 与下一次轮询的游标。调试输出请写到 stderr。

```json
{"jsonrpc":"2.0","id":1,"method":"poll","params":{"cursor":""}}
```

```json
{"jsonrpc":"2.0","id":1,"result":{"cursor":"3","events":[{"id":"meeting-3","title":"课程会议","body":"下午两点，B203","template":"reminder","fields":{"deadline":"14:00"}}]}}
```

事件可指定 `template: "reminder"` 并选择 `ai: "summarize"`、`ai: "classify"` 或 `ai: "classify_summarize"`。不指定 `ai` 就直接使用原文。也可以直接交付 `layout`，例如 `{"blocks":[{"type":"header","title":"提醒"},{"type":"text","text":"下午两点"},{"type":"qr","data":"https://example.com"}]}`，由 Crier 统一渲染。支持的块类型和字段见 [布局结构](internal/ticket/ticket.go)。受信任且显式设置 `allowRaw: true` 的插件也可以提供 `raw`，值为 Base64 编码的 ESC/POS 字节。原始数据会直接交给打印机，请仅为你信任的插件开启。

## 构建

在 Windows 上安装 Go 和 Inno Setup 后运行 `./build.ps1`。可通过 `-Go` 和 `-ISCC` 指定工具路径。安装包生成于 `dist/`。执行 `go test ./...` 运行测试。
