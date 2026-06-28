# 快速开始

这篇教程从安装开始，带你完成第一次远程命令执行。示例使用 `myhost` 作为 `sshq` 别名，`10.0.0.1` 作为主机地址，`root` 作为用户名，`~/.ssh/id_ed25519` 作为私钥。

## 准备条件

使用前需要：

- `Go 1.23` 或更新版本，用于通过 `go install` 安装
- 本机已有一组 `SSH` 密钥
- 至少一台可以连通的 `SSH` 主机

检查 `Go`、密钥文件，并在需要时创建密钥：

```bash
go version
ls ~/.ssh/id_ed25519 ~/.ssh/id_ed25519.pub
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519
```

先确认远端主机接受这把公钥，再把主机加入 `sshq`。

> [!NOTE]
> `sshq` 使用标准 `SSH` 配置、身份文件、`ssh-agent` 和 `known_hosts` 行为。

## 安装 `sshq`

通过 `Go` 安装，并确认 `Go` 二进制目录在 `PATH` 中：

```bash
go install github.com/shayuc137/sshq/cmd/sshq@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

也可以从 `GitHub Releases` 下载预编译二进制：

```bash
# 根据操作系统和处理器架构下载对应压缩包：
# https://github.com/shayuc137/sshq/releases
```

把 `sshq` 放到 `PATH` 中的目录，例如 `Linux` 或 `macOS` 上的 `/usr/local/bin`。

验证安装：

```bash
sshq version
```

在终端中运行会输出易读文本。在 `agent` 子进程或脚本中运行会输出 `JSON` 信封。

## 添加第一台主机

添加名为 `myhost` 的主机：

```bash
sshq config add myhost \
  --hostname 10.0.0.1 \
  --user root \
  --identity ~/.ssh/id_ed25519
```

主机使用非默认 `SSH` 端口时，加上 `--port`：

```bash
sshq config add myhost \
  --hostname 10.0.0.1 \
  --user root \
  --identity ~/.ssh/id_ed25519 \
  --port 2222
```

检查保存的主机：

```bash
sshq ls
sshq info myhost
```

> [!TIP]
> 建议选择短且稳定的别名，例如 `myhost`、`prod-web-1` 或 `lab-router`。

## 测试连通性

检查配置中的 `SSH` 端口是否可达：

```bash
sshq probe myhost
```

如果探测失败，检查地址、端口、防火墙、`VPN` 和网络路径。

## 信任主机密钥

获取主机密钥并加入 `known_hosts`：

```bash
sshq trust myhost
```

命令成功后，`sshq` 可以连接主机，不会遇到未知主机密钥提示。

如果主机密钥发生变化，`sshq` 会报告不匹配并建议替换命令：

```bash
sshq trust myhost --replace
```

> [!WARNING]
> 只有在确认主机密钥变化符合预期后，才使用 `--replace`。

## 运行第一条命令

在远端主机上运行 `hostname`：

```bash
sshq myhost "hostname"
```

> [!TIP]
> 始终给远端命令加引号。这样 `-a` 之类的参数会留在远端命令中，而不会被本地外壳或 `sshq` 提前解析。

试一条带参数的命令：

```bash
sshq myhost "uname -a"
```

强制输出适合 `agent` 读取的结构化结果：

```bash
sshq --json myhost "hostname"
```

示例 `JSON` 输出：

```json
{"ok":true,"data":{"exit_code":0,"stdout":"myhost\n","stderr":"","host":"myhost","duration_ms":42},"schema_version":1}
```

## 理解输出模式

`sshq` 会根据标准输出选择输出模式：

| 调用方 | 标准输出类型 | 默认输出 |
|--------|--------------|----------|
| 终端中的人 | `TTY` | 易读文本 |
| `agent` 或脚本 | 管道 | `JSON` 信封 |

易读模式适合人直接看：

```bash
sshq myhost "df -h"
```

`JSON` 模式适合工具读取：

```bash
result=$(sshq myhost "df -h")
```

对于 `exec`，`JSON` 模式会在 `data` 下返回 `exit_code`、`stdout`、`stderr`、`host` 和 `duration_ms`。

`sshq` 的信息消息、进度和详细诊断都会写入标准错误。在易读模式下，`exec` 的进程标准输出会精确等于远端标准输出。在 `JSON` 模式下，精确的远端标准输出位于 `data.stdout`。

需要时可以强制输出模式：

```bash
sshq --json myhost "hostname"
sshq --pretty myhost "hostname"
SSHQ_OUTPUT=json sshq myhost "hostname"
```

## 下一步

- [`Agent` 集成](agent-integration.zh-CN.md)
- [命令参考](../commands/sshq.md)
- [`sshq exec`](../commands/sshq_exec.md)
- [`sshq cp`](../commands/sshq_cp.md)
- [`sshq config add`](../commands/sshq_config_add.md)
- [`sshq cluster exec`](../commands/sshq_cluster_exec.md)
- [`sshq tunnel`](../commands/sshq_tunnel.md)

第一次命令成功后，建议继续试 `sshq cp` 文件传输、`sshq cluster exec` 多主机执行，以及 `sshq tunnel start` 端口转发。
