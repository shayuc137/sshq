# `Agent` 集成

`sshq` 面向通过子进程调用 `SSH` 的工具设计。它会默认给 `agent` 返回结构化输出，同时保留适合人在终端里阅读的输出。

## `agent` 优先原则

`AI agent` 通常通过管道调用命令行工具。它们需要稳定字段、可预测错误，以及可以直接解析的标准输出。

`sshq` 把这种子进程调用场景作为核心使用方式：

- 管道默认收到 `JSON`
- 终端默认收到易读文本
- 连接状态、进度和详细诊断写入标准错误
- 远程命令结果使用稳定的 `JSON` 信封

因此，`agent` 可以直接调用 `sshq myhost "hostname"`，无需额外添加输出参数。

## `TTY` 自动检测

`sshq` 按这个顺序决定输出模式：

1. `--json`
2. `--pretty`
3. `SSHQ_OUTPUT=json`
4. 标准输出的 `TTY` 检测结果

终端输出使用易读模式：

```bash
sshq myhost "hostname"
```

```text
myhost
```

管道输出使用 `JSON` 模式：

```bash
result=$(sshq myhost "hostname")
```

```json
{"protocol":"sshq/3","exit_code":0,"data":{"stdout":"myhost\n","stderr":"","alias":"myhost","duration_ms":42}}
```

需要时强制指定模式：

```bash
sshq --json myhost "hostname"
sshq --pretty myhost "hostname" | tee output.txt
```

> [!NOTE]
> `--json` 和 `--pretty` 是全局参数。建议把它们放在别名前，或放在显式子命令前。

## `JSON` 信封契约

每个 `JSON` 响应只会出现两种互斥形态之一。`data` 信封表示 `sshq` 已完成操作，`error` 信封表示 `sshq` 未能完成操作。

远程命令成功结果：

```json
{"protocol":"sshq/3","exit_code":0,"data":{"stdout":"myhost\n","stderr":"","alias":"myhost","duration_ms":42}}
```

错误结果：

```json
{"protocol":"sshq/3","error":{"code":"host_key_unknown","hint":"host key unknown for myhost (10.0.0.1:22)","action":"sshq trust myhost"}}
```

调用方应先判断 `error` 是否存在。对于 `exec`，`data` 信封还会在顶层 `exit_code` 中给出精确的远端进程结果——详见下方[正确读取 exit_code](#正确读取-exit_code) 章节。

每个信封都携带 `protocol: "sshq/3"`。机器可读的完整契约发布在 [`schemas/envelope-v3.schema.json`](https://github.com/shayuc137/sshq/blob/main/schemas/envelope-v3.schema.json)，解析器应对照 `schema` 校验，而不是猜测字段形态。

## 标准输出纯净性保证

`sshq` 会避免把自身信息写入标准输出。

对易读模式下的 `exec`，进程标准输出精确等于远端标准输出，进程标准错误包含远端标准错误和 `sshq` 诊断信息。`connecting to myhost...` 这类状态行写入标准错误。

对 `JSON` 模式下的 `exec`，进程标准输出包含 `JSON` 信封。`data.stdout` 精确等于远端标准输出，`data.stderr` 是远端标准错误，状态行、进度和详细诊断写入标准错误。

> [!TIP]
> 在 `agent` 集成中，把标准错误当作日志和诊断来源。把标准输出当作机器读取契约。

## `exec` 的 `JSON` 结果

在终端中需要结构化结果时，使用 `--json`：

```bash
sshq --json exec myhost "uname -a"
```

`exec` 的完整信封结构：

```json
{
  "protocol": "sshq/3",
  "exit_code": 0,
  "data": {
    "stdout": "Linux myhost 6.8.0\n",
    "stderr": "",
    "alias": "myhost",
    "duration_ms": 42
  }
}
```

| 字段 | 含义 |
|------|------|
| `protocol` | 信封契约版本，当前版本线固定为 `sshq/3` |
| 顶层 `exit_code` | 远端进程的精确退出码，仅单远端命令携带 |
| `data.stdout` | 精确保留的远端标准输出 |
| `data.stderr` | 远端标准错误 |
| `data.alias` | `sshq` 主机别名 |
| `data.duration_ms` | 远端命令耗时，单位为毫秒 |

## 正确读取 exit_code

`data` 信封表示 `sshq` 调用已完成。对单远端命令，顶层 `exit_code` 是精确的远端结果：`0` 表示成功，非零值表示远端命令失败。

一个远端命令以退出码 2 失败的示例：

```json
{"protocol":"sshq/3","exit_code":2,"data":{"stdout":"","stderr":"ls: cannot access '/nonexistent': No such file or directory\n","alias":"myhost","duration_ms":112}}
```

这个响应同时包含 `data` 和 `exit_code: 2`。`sshq` 调用已完成，远端 `ls` 命令执行失败。

正确的检查方式：

```bash
json=$(sshq myhost "test -f /etc/os-release")
printf '%s' "$json" | jq -e 'has("data") and .exit_code == 0'
```

当 `sshq` 未能完成操作时，响应里是 `error` 对象而非 `data`，且没有顶层 `exit_code`：

```json
{"protocol":"sshq/3","error":{"code":"host_not_found","hint":"host \"myhost\" not found","action":"run 'sshq ls' to see available hosts"}}
```

汇总：

| 信封 | 进程退出码 | 含义 |
|------|-------------|------|
| `data`，且 exec `exit_code: 0` | `0` | 操作完成，结果成功 |
| `data`，且 exec `exit_code` 非零，或其他命令返回失败结果 | `1` | 操作完成，结果失败 |
| `error` | `2` | `sshq` 未能完成操作，按 `error.code` 分支处理 |

## 带建议动作的错误

文本错误使用两行格式：

```text
Error: host key unknown for myhost (10.0.0.1:22)
  -> run: sshq trust myhost
```

`JSON` 错误携带相同信息：

```json
{"protocol":"sshq/3","error":{"code":"host_key_unknown","hint":"host key unknown for myhost (10.0.0.1:22)","action":"sshq trust myhost"}}
```

常见建议动作包括：

- `run: sshq trust myhost`
- `if expected, run: sshq trust myhost --replace`
- `run 'sshq ls' to see available hosts`
- `retry or use --no-daemon`

`agent` 应按 `error.code` 编程化分支，把 `error.hint` 展示给用户，并把 `error.action` 作为下一条命令建议。有两个错误码要求重试前先核对远端状态：`result_indeterminate` 表示操作可能已经执行；`timeout` 表示本地 `--timeout` 预算耗尽，而远端命令可能仍在运行。这两种情况都禁止盲目重试。

## 作为 `Claude Code` 技能安装

安装内置技能：

```bash
claude skill add --from github.com/shayuc137/sshq/skills/sshq
```

这个技能会让 `Claude Code` 把 `SSH` 操作统一交给 `sshq` 执行。示例请求会映射到类似命令：

```bash
sshq myhost "uptime"
sshq cp ./deploy.tar.gz myhost:/tmp/
sshq probe myhost
```

## `agent` 调用最佳实践

始终给远端命令加引号：

```bash
sshq myhost "df -h"
sshq myhost "uname -a"
```

复杂脚本使用 `--script-file`：

```bash
printf '%s\n' 'set -e' 'hostname' 'uptime' 'df -h' > /tmp/check-system.sh
sshq exec --script-file /tmp/check-system.sh myhost
```

同时检查信封和远端退出码：

```bash
json=$(sshq myhost "test -f /etc/os-release")
printf '%s' "$json" | jq -e 'has("data") and .exit_code == 0'
```

使用这些参数让自动化更可预测：

```bash
sshq --no-progress cp ./app.tar.gz myhost:/tmp/
sshq --timeout 10s myhost "hostname"
sshq exec --no-daemon myhost "hostname"
```

解析器应把 `data` 与 `error` 当作互斥形态。只有单远端命令结果携带顶层 `exit_code`；cluster 的每主机退出码位于 `data.results[].exit_code`。

## 安全敏感操作

以下操作需要用户确认或控制终端，agent 应该转告用户而非自行执行：

- `sshq trust --replace <alias>` — 覆盖已知主机密钥，可能是中间人攻击
- `sshq credential set <alias>` — 需要终端输入密码
- `sshq credential delete <alias>` — 永久删除已存储的密码
- `sshq policy grant <alias> ...` — 需要终端确认，agent 不能自行授权
- `sshq config remove <alias>` — 从 SSH 配置中删除主机
- 远端转发（`-R`）— 把本地服务暴露给远端网络
- 破坏性远程命令 — `rm`、`shutdown`、`reboot`、`mkfs`、`systemctl stop`、防火墙变更

命令被策略拦截时，错误中的 `error.action` 包含建议的 `policy grant` 命令，直接转告用户即可。
