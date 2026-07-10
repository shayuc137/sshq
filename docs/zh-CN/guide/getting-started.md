# 快速开始

这篇教程从安装开始，带你完成第一次远程命令执行。示例使用 `myhost` 作为 `sshq` 别名，`10.0.0.1` 作为主机地址，`root` 作为用户名，`~/.ssh/id_ed25519` 作为私钥。

## 准备条件

使用前需要：

- 本机已有一组 SSH 密钥
- 至少一台可以连通的 SSH 主机

检查密钥文件，需要时创建：

```bash
ls ~/.ssh/id_ed25519 ~/.ssh/id_ed25519.pub
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519
```

先确认远端主机接受这把公钥，再把主机加入 sshq。

> [!NOTE]
> sshq 使用标准 SSH 配置、身份文件、ssh-agent 和 known_hosts 行为。

## 安装 sshq

选择你的平台，复制下面的命令运行即可。不需要安装 Go 或其他工具链。

**Linux (amd64)：**

```bash
curl -L https://github.com/shayuc137/sshq/releases/download/v0.3.0/sshq_0.3.0_linux_amd64.tar.gz | tar xz
sudo mv sshq /usr/local/bin/
sshq version
```

**Linux (arm64，如树莓派)：**

```bash
curl -L https://github.com/shayuc137/sshq/releases/download/v0.3.0/sshq_0.3.0_linux_arm64.tar.gz | tar xz
sudo mv sshq /usr/local/bin/
sshq version
```

**macOS (Apple Silicon)：**

```bash
curl -L https://github.com/shayuc137/sshq/releases/download/v0.3.0/sshq_0.3.0_darwin_arm64.tar.gz | tar xz
sudo mv sshq /usr/local/bin/
sshq version
```

**macOS (Intel)：**

```bash
curl -L https://github.com/shayuc137/sshq/releases/download/v0.3.0/sshq_0.3.0_darwin_amd64.tar.gz | tar xz
sudo mv sshq /usr/local/bin/
sshq version
```

**Windows：**

1. 打开 [GitHub Releases](https://github.com/shayuc137/sshq/releases)，下载 `sshq_0.3.0_windows_amd64.zip`
2. 解压得到 `sshq.exe`
3. 把 `sshq.exe` 移到一个已在 PATH 中的文件夹，比如 `C:\Windows\` 或 `C:\Users\你的用户名\bin\`
4. 打开新终端，运行 `sshq version`

> 不确定哪些文件夹在 PATH 里？在 cmd 里运行 `echo %PATH%`，或在 PowerShell 里运行 `$env:PATH -split ';'` 查看列表。选一个文件夹把 `sshq.exe` 放进去即可。

**备选：从源码安装（需要 Go 1.23+）：**

```bash
go install github.com/shayuc137/sshq/cmd/sshq@latest
```

> `go install` 会把二进制放到 `$(go env GOPATH)/bin`。如果 `sshq version` 提示找不到命令，把这个目录加到 PATH：`export PATH="$PATH:$(go env GOPATH)/bin"`（写进 `~/.bashrc` 或 `~/.zshrc` 可以永久生效）。

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

如果主机只能用密码登录（传统交换机、部分 Windows SSH 服务器），使用加密凭据库存储密码，不要写进 `~/.ssh/config`：

```bash
sshq credential set myhost
```

凭据加密的详细说明见[安全指南](security.md)。

## 测试连通性

检查配置中的 SSH 端口是否可达：

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
{"ok":true,"exit_code":0,"data":{"exit_code":0,"stdout":"myhost\n","stderr":"","host":"myhost","duration_ms":42},"schema_version":2}
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

第一条命令成功后：

- [远程执行](remote-execution.md) — 脚本文件、shell 覆盖、超时
- [文件传输](file-transfer.md) — 上传、下载、中转
- [集群操作](cluster-operations.md) — 多主机并发执行
- [SSH 隧道](tunnels.md) — 端口转发
- [安全](security.md) — 凭据加密、能力策略、审计日志
- [Agent 集成](agent-integration.md) — JSON 契约、stdout 纯净性、skill 安装
