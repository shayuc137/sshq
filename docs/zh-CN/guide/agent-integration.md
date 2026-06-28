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
{"ok":true,"data":{"exit_code":0,"stdout":"myhost\n","stderr":"","host":"myhost","duration_ms":42},"schema_version":1}
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
{"ok":true,"data":{"exit_code":0,"stdout":"myhost\n","stderr":"","host":"myhost","duration_ms":42},"schema_version":1}
```

错误结果：

```json
{"ok":false,"error":{"hint":"host key unknown for myhost (10.0.0.1:22)","action":"run: sshq trust myhost"},"schema_version":1}
```

`agent` 调用方应先判断 `ok`。对于 `exec`，还要继续检查 `data.exit_code`。

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

`data` 对象结构如下：

```json
{
  "exit_code": 0,
  "stdout": "Linux myhost 6.8.0\n",
  "stderr": "",
  "host": "myhost",
  "duration_ms": 42
}
```

| 字段 | 含义 |
|------|------|
| `exit_code` | 远端进程退出码 |
| `stdout` | 精确保留的远端标准输出 |
| `stderr` | 远端标准错误 |
| `host` | `sshq` 主机别名 |
| `duration_ms` | 远端命令耗时，单位为毫秒 |

`exit_code` 为 `0` 表示远端进程成功。`exit_code` 为非零表示远端进程失败，即使 `sshq` 已经成功连接并捕获输出。

## 带建议动作的错误

文本错误使用两行格式：

```text
Error: host key unknown for myhost (10.0.0.1:22)
  -> run: sshq trust myhost
```

`JSON` 错误携带相同信息：

```json
{"ok":false,"error":{"hint":"host key unknown for myhost (10.0.0.1:22)","action":"run: sshq trust myhost"},"schema_version":1}
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
printf '%s' "$json" | jq -e '.ok == true and .data.exit_code == 0'
```

使用这些参数让自动化更可预测：

```bash
sshq --no-progress cp ./app.tar.gz myhost:/tmp/
sshq --timeout 10s myhost "hostname"
sshq exec --no-daemon myhost "hostname"
```

解析器应把 `schema_version` 作为兼容性判断字段。
