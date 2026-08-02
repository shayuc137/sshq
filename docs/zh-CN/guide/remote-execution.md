# 远程执行

使用 `sshq` 在已配置的 `SSH` 主机上运行命令。本指南按常见任务说明 `exec` 用法和会影响运行行为的参数。

## 基础执行

标准写法是：

```bash
sshq exec web-1 "hostname"
```

快捷写法走同一套 `exec` 实现：

```bash
sshq web-1 "hostname"
```

简单命令可以使用快捷写法。需要 `--script-file`、`--shell` 或 `--no-daemon` 这类命令专属参数时，使用 `sshq exec`。在终端中，远端标准输出会写到本地标准输出，远端标准错误会写到本地标准错误。`agent` 通过管道调用时，标准输出会包含 `JSON` 信封。

## 给命令加引号

始终给远程命令加引号：

```bash
sshq web-1 "uname -a"
sshq web-1 "systemctl status nginx"
```

引号会把远程参数、管道、重定向、变量和串联操作符保留在远程命令字符串里，避免本地 `shell` 解析，也避免 `Cobra` 把远程参数当成 `sshq` 参数。

当远程命令里有需要在远端展开的 `$` 时，使用外层单引号：

```bash
sshq web-1 'echo "$SHELL"'
```

## 串联多个命令

把多个命令放进同一个带引号的字符串，可以减少一次 `SSH` 往返：

```bash
sshq web-1 "cd /srv/app && git pull --ff-only && systemctl restart app"
sshq web-1 "date; uptime; df -h /"
```

需要失败即停时使用 `&&`。希望后续命令继续执行时使用 `;`。最终退出码由远端 `shell` 对整段命令字符串返回。

> [!TIP]
> 命令复杂到影响阅读时，把逻辑放进 `--script-file`。

## 覆盖 Shell

`sshq` 会探测远端 `shell` 并默认使用探测结果。需要指定语法时，用 `--shell` 覆盖：

```bash
sshq exec --shell bash web-1 "set -euo pipefail; echo ok"
sshq exec --shell sh web-1 "echo ok"
```

在 `Windows` 上，需要 `PowerShell` 语法时使用 `--shell powershell`：

```bash
sshq exec --shell powershell win-1 "Get-Service | Where-Object Status -eq 'Running' | Select-Object -First 5"
```

一行 `cmd.exe` 命令可以使用 `--shell cmd`：

```bash
sshq exec --shell cmd win-1 "dir C:\Windows"
```

> [!WARNING]
> `cmd` 无法通过 `stdin` 注入 `--script-file`。`Windows` 脚本请使用 `--shell powershell`。

## 运行脚本文件

命令很长或引号很难处理时，使用 `--script-file`：

```bash
sshq exec --script-file ./scripts/diagnostics.sh web-1
sshq exec --shell powershell --script-file ./scripts/diagnostics.ps1 win-1
```

`sshq` 会读取本地文件并发送到远端解释器。脚本不需要提前存在于远端，也不需要可执行权限。

对 POSIX shell（`bash`、`ash`、`sh`、`zsh`），`sshq` 通过 `stdin` 管道把脚本发给对应的解释器（例如 `bash -s`）。

对 `PowerShell`，`sshq` 使用两级执行策略来保证稳定性：

- 8 KiB 以内的脚本会按 UTF-16LE 编码后 base64 传输，通过 `powershell -EncodedCommand` 执行。多行块、here-string 和中文字符都不会有引号问题。
- 超过 8 KiB 的脚本会先上传为 UTF-8 BOM 临时 `.ps1` 文件，用 `powershell -File` 执行，完成后自动清理。

两条路径都使用 `-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass`，脚本在干净环境中执行，不受远端用户的 `PowerShell` profile 影响。

运行本地健康检查脚本：

```bash
sshq exec --script-file ./scripts/health-check.sh web-1
```

## 策略和审计

启用能力策略后，sshq 在执行前检查命令是否匹配白名单/黑名单规则。被拦截的命令返回错误，附带匹配的模式和建议的 `policy grant` 命令：

```bash
sshq policy check prod --command "journalctl -u app -n 100"
```

审计日志（启用后）记录每次执行的时间戳、别名、命令摘要（截断到 200 字符）、结果和耗时。脚本文件操作只记录 SHA-256 哈希和字节数。配置详见[安全指南](security.md)。

## 控制超时

`--timeout` 是全局参数，默认值是 `30s`。远程执行场景下，它覆盖连接建立、shell 探测、命令执行和脚本执行。

```bash
sshq --timeout 10s web-1 "uptime"
sshq --timeout 2m exec --script-file ./scripts/slow-maintenance.sh web-1
sshq --timeout 15m web-1 "apt-get update && apt-get install -y jq"
```

超时到期后，`sshq` 停止等待、关闭自己的会话，返回 `error.code: "timeout"`。**远端命令不会被终止**，它会在主机上继续跑完。重跑任何有副作用的命令之前，先确认实际状态：

```bash
sshq web-1 "pgrep -af maintenance.sh"
```

超时的清理或迁移脚本通常还在工作，第二次执行会和第一次撞车。

`cp` 是唯一不继承这个默认值的命令，见[文件传输指南](file-transfer.md#传输超时)。

## Windows 编码

`Windows` 主机可能使用 `GBK` 这类本地代码页输出文本。`sshq` 会在主机档案探测阶段通过 `chcp` 检查 `Windows` 代码页。

对于已识别的非 `UTF-8` 编码，命令标准输出和标准错误会先转成 `UTF-8`，再进入渲染流程：

```bash
sshq exec --shell powershell win-1 "Get-ChildItem C:\"
```

已知映射包含 `936` 到 `gbk`、`950` 到 `big5`、`932` 到 `shift-jis`、`949` 到 `euc-kr`。代码页 `65001` 已经是 `UTF-8`，会直接保留。

## 直连与守护进程

守护进程已经运行时，`sshq` 可以把执行请求发给守护进程，并复用连接池里的 `SSH` 连接。守护进程不可用或调度失败时，`sshq` 会回退到直连。

手动启动和查看守护进程：

```bash
sshq daemon start
sshq daemon status
```

排查连接建立、`shell` 探测或守护进程路径问题时，使用 `--no-daemon`：

```bash
sshq exec --no-daemon web-1 "hostname"
sshq exec --no-daemon --script-file ./scripts/diagnostics.sh web-1
```

日常使用可以省略 `--no-daemon`。

## 详细模式

使用 `--verbose` 或 `-v` 把诊断信息打印到标准错误：

```bash
sshq -v web-1 "hostname"
```

详细输出可能包含连接耗时、守护进程连接是否复用、探测到的系统和 `shell`、选择的 `shell`、执行细节：

```text
connection: alias=web-1 duration=18ms daemon reused=true
profile: os=linux shell=bash
shell selected: bash
```

详细输出会留在标准错误。终端模式下远端标准输出保持干净，`agent` 模式下 `JSON` 仍写在标准输出。

## 常见模式

检查单台主机磁盘空间：

```bash
sshq web-1 "df -h /"
```

检查多台主机磁盘空间：

```bash
sshq cluster exec "df -h /" --hosts web-1,web-2,db-1
```

运行本地部署脚本：

```bash
sshq exec --script-file ./scripts/deploy.sh web-1
```

运行基础诊断：

```bash
sshq web-1 "hostname && uptime && df -h / && free -m"
```

使用 `PowerShell` 运行 `Windows` 诊断：

```bash
sshq exec --shell powershell win-1 "Get-ComputerInfo | Select-Object CsName,WindowsVersion,OsUptime"
```
