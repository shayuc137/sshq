# 安全

`sshq` 提供三层安全机制：加密凭据存储、能力策略和审计日志。所有配置位于系统配置目录下的 `config.toml`：

- Linux：`~/.config/sshq/`
- macOS：`~/Library/Application Support/sshq/`
- Windows：`%APPDATA%\sshq\`

## 密码凭据

部分主机只支持密码认证（老交换机、某些 Windows OpenSSH 服务器）。`sshq` 使用 [age](https://github.com/FiloSottile/age) 加密密码，以 SSH 公钥作为收件人，存储在 `config.toml` 同目录的 `credentials.age` 中。

```bash
sshq credential set router-1       # 交互输入密码，需要终端
sshq credential list                # 仅显示别名，不显示密码
sshq credential delete router-1
```

密码仅作为最低优先级的认证方式——SSH agent 和私钥认证始终优先。如果没有 SSH 密钥，`sshq` 会回退到口令加密模式保护凭据文件。

在无终端环境（daemon 后台、agent 管道模式）下，通过环境变量 `SSHQ_CREDENTIAL_PASSPHRASE` 提供口令。

> [!WARNING]
> `sshq` 没有 `credential get` 命令——这是有意为之，减少已存储密码的暴露面。

## 能力策略

能力策略控制 agent 可以执行哪些命令、访问哪些路径。在 `config.toml` 中配置：

```toml
[policy.default]
command_whitelist = ["^ls(\\s|$)", "^cat(\\s|$)", "^grep(\\s|$)", "^tail(\\s|$)"]
command_blacklist = ["(?i)\\brm\\s+-rf\\b", "(?i)\\bmkfs\\b", "(?i)\\bshutdown\\b"]
local_path_whitelist = ["."]
remote_path_whitelist = []

[policy.hosts.prod-db]
mode = "override"
command_whitelist = ["^SELECT\\s", "^SHOW\\s"]
command_blacklist = ["^DROP\\s", "^DELETE\\s", "^TRUNCATE\\s"]
remote_path_whitelist = ["/var/log"]
```

### 全局默认与 per-host 覆盖

- `[policy.default]` 对所有主机生效
- `[policy.hosts.<alias>]` 覆盖或追加到默认策略
- `mode = "override"` 完全替换默认数组；`mode = "append"`（默认）在默认基础上追加
- 某个主机设置 `enabled = false` 则完全跳过策略检查
- 没有 `config.toml` = 没有策略 = 不限制（向后兼容）

### 白名单与黑名单

- **白名单**（非空时）：命令必须匹配至少一条
- **黑名单**：命中任意一条即拒绝
- 顺序：先检查白名单，再检查黑名单。黑名单始终优先

建议使用锚定模式：

```toml
command_whitelist = ["^journalctl(\\s|$)", "^systemctl\\s+status\\s"]
command_blacklist = ["(?i)(^|[;&|])\\s*(rm|dd|mkfs|shutdown)\\b"]
```

### 路径白名单

- `local_path_whitelist`：限制 `cp` 的本地端路径
- `remote_path_whitelist`：限制 `cp` 的远端路径
- 空白名单 = 该维度不限制
- 本地路径会解析为绝对路径并解析 symlink，使用前缀边界匹配

### 转发白名单

隧道转发使用和命令/路径相同的策略框架：

```toml
[policy.default]
local_forward_whitelist = ["localhost:8000-9000", "db.internal:5432"]
remote_forward_whitelist = []
```

- `local_forward_whitelist` 检查 `-L` 隧道的**远端目标**（`remote_host:remote_port`）
- `remote_forward_whitelist` 检查 `-R` 隧道的**本地目标**（`local_host:local_port`）
- 空白名单 = 不限制

支持精确匹配（`localhost:8080`）、端口通配（`localhost:*`）、端口范围（`localhost:8000-9000`）和 host 通配（`*:22`）。

测试转发是否允许：

```bash
sshq policy check bastion --local-forward db.internal:5432
```

临时授权转发访问：

```bash
sshq policy grant bastion "db.internal:5432" --kind local-forward --ttl 15m
```

### 临时授权

命令被阻止时，错误信息会包含建议的授权命令：

```bash
sshq policy grant prod "^docker restart" --ttl 1h
```

授权需要终端（agent 不能自行授权），仅存在于 daemon 内存中，按 TTL 过期（最长 1 小时），且永远不覆盖黑名单。撤销方式：

```bash
sshq policy revoke --alias prod     # 撤销某主机的全部授权
sshq policy revoke <grant-id>       # 撤销单条授权
```

### 验证与测试

```bash
sshq policy validate                                           # 检查配置语法和正则合法性
sshq policy check prod --command "journalctl -u app -n 100"    # 模拟测试：这条命令会被允许吗？
sshq policy list prod                                           # 查看生效策略和活跃授权
```

### Cluster 预检

`sshq cluster exec` 在执行前对所有目标主机做策略检查。任一主机被阻止则不执行任何主机——防止部分执行。

> [!TIP]
> 生产主机建议使用 `mode = "override"` 配合窄白名单，而非追加到宽泛的全局默认。

## 审计日志

审计日志记录每次操作（exec、cp、tunnel、cluster）的元数据，不存储命令输出、密码或完整脚本内容。通过 `config.toml` 控制：

```toml
[audit]
enabled = true
path = "~/.config/sshq/audit.jsonl"
max_size = "10MB"
```

### 记录内容

每次操作生成一条 JSONL 记录，包含时间戳、主机别名、操作类型、命令摘要（截断到 200 字符）、结果（success/error/blocked）、耗时和退出码。

脚本文件操作记录 SHA-256 哈希和字节数，不记录完整脚本内容。策略阻止的操作以 `result = "blocked"` 记录，附带匹配的 pattern。

### 查询

```bash
sshq audit                               # 显示最近记录
sshq audit --last 50                      # 最近 50 条
sshq audit --alias prod                   # 按主机过滤
sshq audit --operation exec               # 按操作类型过滤
sshq audit --alias prod --operation cp    # 组合过滤
```

### 日志轮转

日志文件达到 `max_size` 时自动轮转为 `audit-YYYYMMDD-HHMMSS.log`，并创建新的 `audit.log`。查询命令会同时扫描当前和已轮转的文件。

### Fail-Closed

审计开启但日志文件无法创建或写入时，操作会被阻止——避免"审计说开了但实际没记录"的静默失败。

> [!WARNING]
> 审计日志使用 append-only 写入且文件权限为 0600，但同一 OS 用户或 root 仍可修改日志。如需防篡改能力，未来可考虑外部日志存储。
