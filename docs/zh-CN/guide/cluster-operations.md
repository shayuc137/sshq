# 集群操作

`sshq cluster exec` 用来把同一条命令并发发送到多台已配置 SSH 主机，适合健康检查、分批操作和指标采集。

## 基本语法

```bash
sshq cluster exec "<command>" --all
sshq cluster exec "<command>" --tag web
sshq cluster exec "<command>" --env production
sshq cluster exec "<command>" --hosts rn,wee,ali
```

远端命令建议加引号。每次执行都需要一个主机选择器。

| 选择器 | 目标 |
| --- | --- |
| `--all` | 当前 SSH 配置里的所有具体主机别名。 |
| `--tag web` | `tags` 元数据包含 `web` 的主机。 |
| `--env production` | `environment` 元数据等于 `production` 的主机。 |
| `--hosts rn,wee,ali` | 逗号分隔的明确别名列表。 |

`--tag` 和 `--env` 可以组合。`--hosts` 是独占选择器，不能和 `--tag`、`--env`、`--all` 一起使用。

> [!TIP]
> 目标不明确时，先运行 `sshq ls` 或 `sshq search <pattern>`。

## 在所有主机执行

```bash
sshq cluster exec "hostname" --all
sshq cluster exec "uptime" --all
sshq cluster exec "df -h /" --all
```

主机列表默认来自 `~/.ssh/config`。需要其他配置文件时使用全局 `--config <path>`。`Host *` 这类通配配置不会作为集群目标。

## 按标签筛选

标签以逗号分隔形式存入主机元数据，可用 `config` 添加或修改。

```bash
sshq config add web-1 --hostname 10.0.1.11 --user deploy --tag prod,web
sshq config set web-2 tags prod,web
sshq config set db-1 tags prod,db
```

按标签执行：

```bash
sshq cluster exec "systemctl is-active nginx" --tag web
```

标签会按逗号拆分后精确匹配。`prod,web` 可以匹配 `web`，`websocket` 不会匹配。

## 按环境筛选

环境存储为 `environment` 元数据。

```bash
sshq config add web-1 --hostname 10.0.1.11 --user deploy --env production
sshq config set web-2 env production
sshq config set web-canary env staging
```

按环境执行，或同时使用环境和标签：

```bash
sshq cluster exec "uname -a" --env production
sshq cluster exec "systemctl status nginx --no-pager" --tag web --env production
```

## 使用明确主机列表

目标列表固定、无需依赖元数据时，使用 `--hosts`。

```bash
sshq cluster exec "hostname" --hosts rn,wee,ali
sshq cluster exec "uptime" --hosts web-1,web-2,web-3
sshq cluster exec "hostname" --hosts "web-1, web-2, web-1"
```

别名前后的空格会被忽略，重复别名会被忽略。别名不存在时，远端命令开始前就会失败：

```text
Error: hosts not found: web-9
  -> run 'sshq ls' to see available hosts
```

## 控制并发数

用 `--concurrency N` 限制同时执行的主机数。

```bash
sshq cluster exec "uptime" --tag web --concurrency 5
sshq cluster exec "df -h /" --all --concurrency 20
```

默认值是 `10`。小于或等于零的值会按默认值处理。会改变状态的命令适合低并发，只读检查可以使用更高并发。

> [!TIP]
> 做分批变更时，先用 `--concurrency 1`。

## 输出格式

文本输出会给每一行远端标准输出加 `[alias]` 前缀，最后输出汇总行。

```bash
sshq cluster exec "hostname" --hosts web-1,web-2
```

```text
[web-1] web-1
[web-2] web-2
total=2 success=2 failed=0
```

远端非零退出码和连接错误会按主机显示：

```text
[web-1] active
[web-2] inactive
[web-2] exit=3
total=2 success=1 failed=1
```

```text
[web-1] ok
[web-2] error: dial tcp 10.0.1.12:22: i/o timeout
total=2 success=1 failed=1
```

需要程序读取时使用 `--json`。JSON 数据包含 `results` 和 `summary`，每个结果包含 `alias`、`stdout`、`stderr`、`exit_code`，失败时还有 `error`。

## 错误处理

部分失败不会阻止其他主机。每台被选中的主机都有独立结果。任意主机连接失败、执行失败或远端退出码非零，本地命令都会返回非零退出码。

选择器错误会在执行前返回：

```text
Error: specify --hosts, --tag, --env, or --all
  -> usage: sshq cluster exec --all "command"
```

```text
Error: --hosts cannot be combined with --tag, --env, or --all
  -> use exactly one host selector
```

## 常见模式

健康检查：

```bash
sshq cluster exec "systemctl is-active nginx" --tag web --env production
sshq cluster exec "curl -fsS http://127.0.0.1/health" --tag web
```

一次重启一台：

```bash
sshq cluster exec "sudo systemctl restart nginx" --tag web --env production --concurrency 1
```

采集指标：

```bash
sshq cluster exec "df -h /" --all
sshq cluster exec "cat /proc/loadavg" --tag linux
sshq cluster exec "free -m" --tag linux --concurrency 20
```

直接指定小批量主机：

```bash
sshq cluster exec "date -u" --hosts web-1,web-2,web-canary
```
