# 主机管理

`sshq` 读取并编辑 OpenSSH 风格主机配置。默认路径是 `~/.ssh/config`，也可以用全局 `--config <path>` 指定其他文件。

## 添加主机

使用 `sshq config add <alias>` 追加一个主机块。

```bash
sshq config add web-1 \
  --hostname 10.0.1.11 \
  --user deploy \
  --port 22 \
  --identity ~/.ssh/deploy_ed25519 \
  --desc "production web server" \
  --tag prod,web \
  --env production \
  --proxy-jump bastion
```

| 选项 | 含义 |
| --- | --- |
| `--hostname <host>` | 远端主机名或 IP，必填。 |
| `--port <port>` | SSH 端口。值为默认 `22` 时不会写入新块。 |
| `--user <user>` | SSH 用户。 |
| `--identity <path>` | 私钥文件路径。 |
| `--desc <text>` | 描述元数据。 |
| `--tag <a,b>` | 逗号分隔的 `tags` 元数据。 |
| `--env <name>` | `environment` 元数据。 |
| `--proxy-jump <alias>` | `ProxyJump` 主机别名。 |

> [!WARNING]
> `sshq` 会拒绝把密码写入 SSH 配置。如需密码认证，请使用加密凭据库（见下方[密码凭据](#密码凭据)段落）。

## 编辑主机

使用 `sshq config set <alias> <key> <value>` 修改单个字段。

```bash
sshq config set web-1 hostname 10.0.1.12
sshq config set web-1 user deploy
sshq config set web-1 port 2222
sshq config set web-1 identityfile ~/.ssh/deploy_ed25519
sshq config set web-1 proxyjump bastion
sshq config set web-1 tags prod,web
sshq config set web-1 env production
sshq config set web-1 description "production web server"
```

`env` 和 `environment` 都会写入规范字段 `environment`。`password` 会被拒绝：

```text
Error: passwords must not be stored in ssh config
  -> use ssh-agent or identity files
```

## 删除主机

```bash
sshq config remove web-1
sshq config rm web-1
```

如果别名不存在，`sshq` 会报告缺失主机，并提示运行 `sshq ls`。

## 列出、搜索和查看详情

```bash
sshq ls
sshq config list
```

`config list` 是 `ls` 的别名。

搜索不区分大小写，结果按别名排序，并匹配别名、主机名和描述。

```bash
sshq search api
sshq search production
sshq search 10.0.1
```

查看单个主机：

```bash
sshq info api-prod
```

`info` 会显示别名、主机名、用户、端口、私钥文件、`ProxyJump`、元数据，以及已缓存的远端 profile。

## 元数据

`sshq` 把非敏感元数据存成 `Host` 行上方的注释。标准 SSH 工具会忽略这些注释。

```ssh-config
# ===== api-prod =====
# sshq:description=production API node
# sshq:environment=production
# sshq:tags=prod,api
Host api-prod
    HostName api-1.example.internal
    User deploy
    IdentityFile ~/.ssh/prod_ed25519
    ProxyJump bastion
```

| 字段 | 设置方式 | 用途 |
| --- | --- | --- |
| `description` | `sshq config set <alias> description "..."` | `sshq search`、`sshq info` |
| `tags` | `sshq config set <alias> tags prod,web` | `sshq cluster exec --tag` |
| `environment` | `sshq config set <alias> env production` | `sshq cluster exec --env` |

> [!TIP]
> 标签建议短且稳定，例如 `web`、`db`、`linux`、`prod`、`staging`。

## ProxyJump

使用标准 OpenSSH `ProxyJump` 指令配置堡垒机访问。

```bash
sshq config add bastion --hostname bastion.example.com --user deploy --identity ~/.ssh/deploy_ed25519
sshq config add db-prod --hostname db.internal --user postgres --identity ~/.ssh/prod_ed25519 --proxy-jump bastion
```

之后直接使用目标别名：

```bash
sshq db-prod "hostname"
sshq cp ./schema.sql db-prod:/tmp/schema.sql
sshq tunnel start db-prod -L 15432:localhost:5432
```

`sshq` 会按别名解析多跳 `ProxyJump` 链。比如 `app-prod` 通过 `bastion-2`，`bastion-2` 再通过 `bastion-1`，使用时仍然只写最终目标 `app-prod`。

> [!WARNING]
> 如果发现循环 `ProxyJump` 链，解析会在重复主机处停止。

## 密码凭据

部分主机只支持密码认证（老交换机、某些 Windows OpenSSH 服务器）。`sshq` 使用 [age](https://github.com/FiloSottile/age) 加密密码，以 SSH 公钥作为收件人，存储在系统配置目录下（Linux 为 `~/.config/sshq/credentials.age`，macOS 为 `~/Library/Application Support/sshq/`，Windows 为 `%AppData%\sshq\`）。

```bash
sshq credential set router-1
sshq credential list
sshq credential delete router-1
```

`credential set` 会交互式提示输入密码（需要终端）。密码仅作为最低优先级的认证方式——SSH agent 和私钥认证始终优先。

> [!TIP]
> 如果没有 SSH 密钥，`sshq` 会回退到口令加密模式保护凭据文件。

无终端环境（daemon、agent 管道）下使用凭据库时，在启动命令前设置 `SSHQ_CREDENTIAL_PASSPHRASE` 环境变量。完整的凭据和策略配置见[安全指南](security.md)。

## 一步诊断

`sshq doctor <alias>` 按顺序执行七项检查——配置有效性、密钥文件、`ProxyJump` 解析、TCP 可达性、主机密钥、认证和 shell 探测。遇到第一个失败就停下来，返回可以直接执行的 `next_action`。

```bash
sshq doctor api-prod
```

全部通过：

```json
{
  "data": {
    "alias": "api-prod",
    "resolved": {
      "hostname": "10.0.1.100",
      "port": "22",
      "user": "deploy",
      "proxy_jump": "bastion",
      "identity_file": "~/.ssh/prod_ed25519"
    },
    "checks": {
      "config_valid": true,
      "identity_file_exists": true,
      "proxy_jump_exists": true,
      "tcp_reachable": true,
      "host_key_known": true,
      "auth_ok": true,
      "shell_detected": true
    },
    "profile": {
      "os": "linux",
      "shell": "bash",
      "home_dir": "/home/deploy",
      "detected_at": 1783615160
    }
  }
}
```

检出问题——主机密钥不在 `known_hosts` 中：

```json
{
  "data": {
    "alias": "api-prod",
    "failed_check": "host_key_known",
    "hint": "host key is not present in known_hosts",
    "next_action": "sshq trust api-prod"
  }
}
```

`next_action` 就是修复问题的命令。执行后再跑一次 `doctor` 确认后续检查通过。失败点之后的检查会标记为 `"skipped"`。未配置的密钥文件显示为 `null`。

## 检查连通性

使用 `probe` 做 TCP 可达性检查。默认情况下 `probe` 会走完整的 SSH 配置路径，包括已配置的 `ProxyJump`。

```bash
sshq probe api-prod
sshq probe api-prod --port 2222
sshq probe --all
```

结果中的 `probe_path` 字段说明连接方式——通过跳板时为 `"via-proxy"`，直连时为 `"direct"`：

```json
{
  "data": {
    "alias": "api-prod",
    "host": "10.0.1.100",
    "port": "22",
    "proxy_jump": "bastion",
    "probe_path": "via-proxy",
    "reachable": true,
    "latency_ms": 277
  }
}
```

使用 `--direct` 跳过 `ProxyJump`，直接测试目标的 TCP 可达性：

```bash
sshq probe api-prod --direct
```

这在排查问题时很有用——可以区分故障出在跳板还是目标端口本身。

TCP 检查成功后刷新远端 profile 缓存：

```bash
sshq probe api-prod --refresh-profile
```

profile 探测会通过 SSH 连接获取系统、shell、编码和家目录等信息。在 Windows 主机上，这一步还会探测 `powershell.exe` 或 `pwsh` 的可用路径。

## 信任主机密钥

`sshq` 使用严格主机密钥检查。用 `trust` 拉取主机密钥并写入 `known_hosts`。

```bash
sshq trust api-prod
sshq trust --all
```

如果密钥发生变化，`trust` 默认拒绝覆盖：

```text
Error: host key CHANGED for api-prod (api-1.example.internal:22)
  old: ssh-ed25519 SHA256:old...
  new: ssh-ed25519 SHA256:new...
  -> if expected (e.g. OS reinstall), re-run with --replace
```

确认变更后再替换：

```bash
sshq trust api-prod --replace
```

> [!WARNING]
> 主机密钥变化是高优先级安全信号。

## Daemon 生命周期

daemon 负责连接池、profile 缓存和后台 Tunnel 列表。

```bash
sshq daemon start
sshq daemon status
sshq daemon stop
```

多数命令在没有 daemon 时仍能工作；daemon 不可达时会使用直接 SSH 连接。daemon 正在运行时，支持 daemon 的命令会复用池中的 SSH 连接。daemon 停止时，会停止它管理的所有 Tunnel。空闲约 `30m` 后，daemon 也会自动退出。

> [!TIP]
> 多次运行 `exec`、`cp`、`cluster`，或需要后台 `tunnel` 时，先启动 daemon。一次性命令使用直接连接即可。
