# Crier

Crier 把邮件和其他信息源里的重要提醒打印成热敏小票。Windows 桌面版内置邮件来源、统一的 AI 设置、测试小票和打印记录。

## 安装与使用

从 [GitHub Releases](https://github.com/Zhuym07/crier/releases) 下载 Windows x64 安装包。首启可选择 ESC/POS 打印机、预览或打印测试小票，再配置邮件和可选的 AI。当前按 58 mm、384 点纸宽排版，打印机需先安装 Windows 驱动。

IMAP 填写带端口的地址，例如 `imap.example.com:993`。首次检查记录当前邮箱进度，从此之后的新邮件开始打印。仅在开启 AI 后，邮件标题和用于摘要的正文才会发送给所配置的 AI 接口。设置中的邮件密码和 API Key 由 Windows DPAPI 保护。

AI 的 Base URL 填到 `/v1` 即可（例如 `https://integrate.api.nvidia.com/v1`），传令会自动补上 `/chat/completions`；已经带该后缀的完整地址也照常可用。支持 HTTP 和 HTTPS。无需认证的本地服务可留空 API Key。HTTP 会明文传输请求内容和密钥，适合本机或可信网络。邮件 AI 支持日程、截止、提交回执、公告、验证码、待办、账单、推广和通用摘要。AI 请求失败时会打印原文摘录。

在侧边栏的「插件」页启用「Telegram 打印」并填入 Bot Token。可选填代理、管理员用户 ID、日志频道和待办频道。普通消息支持文本、图片、静态贴纸和 `[qr:内容]`。命令包括 `/print`、`/qrcode`、`/usage`、`/todo` 和 `/test_printer`。其中 `/todo` 和 `/test_printer` 需要管理员 ID，`/todo` 在开启中央 AI 设置后会拆解任务。使用旧机器人前请先停掉原来的轮询进程。Telegram Bot Token 和代理地址由 Windows DPAPI 保护。

在侧边栏的「插件」页管理邮件、Telegram 和外部插件。可用「官方下载」打开发布页，并通过「导入本地插件」选择已下载的可信程序。「移除」只取消登记，保留程序文件。

打印记录中的「已提交」表示 Windows 接受了任务。驱动明确报告 PRINTED 时会显示「驱动报告已打印」。收到小票后可在界面确认。Telegram 会收到相应回执，待办频道会收到和小票一致的任务拆解。若提交途中出错或进程中断，任务会标记为「结果未知」，便于先检查纸票和系统队列。

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

在 Windows 上安装 Go 和 Inno Setup 后运行 `./build.ps1`。可通过 `-Go` 和 `-ISCC` 指定工具路径。版本号写在 `VERSION`，构建时会注入程序和安装包。安装包与校验清单 `SHA256SUMS` 生成于 `dist/`。发布时把这两个文件一起上传到同一个 GitHub Release，应用内更新才能校验安装包。执行 `go test ./...` 运行测试。
