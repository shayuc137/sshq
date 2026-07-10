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
{"ok":true,"exit_code":0,"data":{"exit_code":0,"stdout":"myhost\n","stderr":"","host":"myhost","duration_ms":42},"schema_version":2}
```

需要时强制指定模式：

```bash
sshq --json myhost "hostname"
sshq --pretty myhost "hostname" | tee output.txt
```

> [!NOTE]
> `--json` 和 `--pretty` 是全局参数。建议把它们放在别名前，或放在显式子命令前。

## `JSON` 信封契约

每个 `JSON` 响应都是带有 `schema_version` 的单个信封。

远程命令成功结果：

```json
{"ok":true,"exit_code":0,"data":{"exit_code":0,"stdout":"myhost\n","stderr":"","host":"myhost","duration_ms":42},"schema_version":2}
```

错误结果：

```json
{"ok":false,"error":{"hint":"host key unknown for myhost (10.0.0.1:22)","action":"sshq trust myhost"},"schema_version":2}
```

调用方应先判断 `ok`。对于 `exec`，还必须检查顶层 `exit_code`——详见下方[正确读取 exit_code](#正确读取-exit_code) 章节。

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
  "ok": true,
  "exit_code": 0,
  "data": {
    "exit_code": 0,
    "stdout": "Linux myhost 6.8.0\n",
    "stderr": "",
    "host": "myhost",
    "duration_ms": 42
  },
  "schema_version": 2
}
```

| 字段 | 含义 |
|------|------|
| 顶层 `exit_code` | 远端进程退出码，调用方应读取这个字段 |
| `data.exit_code` | 相同值，保留以兼容旧解析器 |
| `data.stdout` | 精确保留的远端标准输出 |
| `data.stderr` | 远端标准错误 |
| `data.host` | `sshq` 主机别名 |
| `data.duration_ms` | 远端命令耗时，单位为毫秒 |

## 正确读取 exit_code

`ok: true` 表示 sshq 调用本身完成了——连接成功、命令已执行、输出已捕获。这并不代表远端命令成功。远端命令的结果在顶层 `exit_code` 里。

一个远端命令以退出码 2 失败的示例：

```json
{"ok":true,"exit_code":2,"data":{"exit_code":2,"stdout":"","stderr":"ls: cannot access '/nonexistent': No such file or directory\n","host":"myhost","duration_ms":112},"schema_version":2}
```

这个响应里 `ok` 为 `true`，`exit_code` 为 `2`。sshq 调用本身没问题，但远端 `ls` 命令失败了。如果只看 `ok: true` 就认为成功，会漏掉这个错误。

正确的检查方式：

```bash
json=$(sshq myhost "test -f /etc/os-release")
printf '%s' "$json" | jq -e '.ok == true and .exit_code == 0'
```

当 `ok` 为 `false` 时，响应里是 `error` 对象而非 `data`，且没有顶层 `exit_code`：

```json
{"ok":false,"error":{"hint":"host \"myhost\" not found","action":"run 'sshq ls' to see available hosts"},"schema_version":2}
```

汇总：

| `ok` | `exit_code` | 含义 |
|------|-------------|------|
| `true` | `0` | 远端命令成功 |
| `true` | 非零 | 远端命令失败（sshq 调用本身正常） |
| `false` | 不存在 | sshq 层面失败，查看 `error.hint` |

## 带建议动作的错误

文本错误使用两行格式：

```text
Error: host key unknown for myhost (10.0.0.1:22)
  -> run: sshq trust myhost
```

`JSON` 错误携带相同信息：

```json
{"ok":false,"error":{"hint":"host key unknown for myhost (10.0.0.1:22)","action":"sshq trust myhost"},"schema_version":2}
```

常见建议动作包括：

- `run: sshq trust myhost`
- `if expected, run: sshq trust myhost --replace`
- `run 'sshq ls' to see available hosts`
- `retry or use --no-daemon`

`agent` 可以把 `error.hint` 展示给用户，并把 `error.action` 作为下一条命令建议。

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
printf '%s' "$json" | jq -e '.ok == true and .exit_code == 0'
```

使用这些参数让自动化更可预测：

```bash
sshq --no-progress cp ./app.tar.gz myhost:/tmp/
sshq --timeout 10s myhost "hostname"
sshq exec --no-daemon myhost "hostname"
```

解析器应把 `schema_version` 作为兼容性判断字段。

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
