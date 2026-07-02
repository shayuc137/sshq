# SSH 隧道

`sshq tunnel` 用来管理 SSH 端口转发。它可以让本机访问远端服务，也可以把本地服务暴露给远端主机。

| 方向 | 参数 | 用途 |
| --- | --- | --- |
| 本地 | `-L localport:remotehost:remoteport` | 本机端口访问 SSH 主机可见的服务。 |
| 远端 | `-R remoteport:localhost:localport` | 远端端口访问本机服务。 |

## 本地转发

本地转发在本机监听端口，通过 SSH 转发流量。

```bash
sshq tunnel start bastion -L 15432:db.internal:5432
```

这个命令创建：

```text
localhost:15432 -> db.internal:5432 via bastion
```

在本机连接：

```bash
psql -h localhost -p 15432 -U app appdb
```

`db.internal` 从 SSH 主机一侧解析，所以它只需要能从 `bastion` 访问。

> [!TIP]
> 本地转发常用于访问堡垒机后的数据库、管理页面、指标接口和私有 HTTP 服务。

示例：

```bash
sshq tunnel start bastion -L 15432:db.internal:5432
sshq tunnel start ops-box -L 18080:dashboard.internal:80
sshq tunnel start cache-gw -L 16379:redis.internal:6379
```

## 远端转发

远端转发在远端监听端口，把流量转回本机。

```bash
sshq tunnel start public-vps -R 18080:localhost:3000
```

这个命令创建：

```text
public-vps:18080 -> localhost:3000 on your machine
```

在远端主机访问：

```bash
curl http://localhost:18080
```

本地服务必须监听在 `localhost:3000`。

> [!WARNING]
> 远端转发可能受 SSH 服务端设置限制，例如 `AllowTcpForwarding` 和 `GatewayPorts`。

示例：

```bash
sshq tunnel start public-vps -R 18080:localhost:3000
sshq tunnel start webhook-host -R 19090:localhost:9090
sshq tunnel start staging -R 18081:localhost:5173
```

## 转发格式

本地转发完整形式：

```bash
sshq tunnel start <alias> -L <local_port>:<remote_host>:<remote_port>
```

远端转发完整形式：

```bash
sshq tunnel start <alias> -R <remote_port>:<local_host>:<local_port>
```

`sshq` 会把每个规格里的本地监听侧绑定到 `localhost:<port>`。两段简写也可用，并会把两侧主机名都按 `localhost` 处理：

```bash
sshq tunnel start bastion -L 8080:80
sshq tunnel start public-vps -R 18080:3000
```

## 转发策略

启用能力策略后，隧道目标在创建前会被转发白名单检查：

- `-L`（本地转发）：`local_forward_whitelist` 检查远端目标（`remote_host:remote_port`）
- `-R`（远端转发）：`remote_forward_whitelist` 检查本地目标（`local_host:local_port`）

```bash
sshq policy check bastion --local-forward db.internal:5432
```

被拦截时，错误信息包含建议的 `policy grant` 命令。白名单语法和授权流程详见[安全指南](security.md)。

## 前台和后台

daemon 正在运行时，`tunnel start` 会注册后台 Tunnel，并返回 Tunnel ID。

```bash
sshq daemon start
sshq tunnel start bastion -L 15432:db.internal:5432
```

```text
tun-1 localhost:15432 -> db.internal:5432 via bastion
```

daemon 未运行时，`tunnel start` 会退回前台模式，直到按下 `Ctrl+C` 才退出。

```text
connecting to bastion...
local forward localhost:15432 -> db.internal:5432 via bastion
tunnel running, press Ctrl+C to stop
```

## 多个 Tunnel

需要多个可独立管理的 Tunnel 时，先启动 daemon。

```bash
sshq daemon start
sshq tunnel start bastion -L 15432:db.internal:5432
sshq tunnel start bastion -L 18080:dashboard.internal:80
sshq tunnel start public-vps -R 19090:localhost:9090
sshq tunnel list
sshq tunnel stop tun-2
```

`list` 输出示例：

```text
tun-1 local localhost:15432 -> db.internal:5432 via bastion conns=1
tun-2 local localhost:18080 -> dashboard.internal:80 via bastion conns=0
tun-3 remote localhost:19090 -> localhost:9090 via public-vps conns=0
```

如果 daemon 不可达，`tunnel list` 输出 `no active tunnels`，`tunnel stop <id>` 返回 `daemon not running`。

## 生命周期

每个 Tunnel 会持续接受连接，直到被停止或取消。监听器遇到临时 `Accept` 错误时，会使用带抖动的指数退避重试。

重试延迟从 `100ms` 开始，最高增长到 `5s`，成功接受连接后重置。连续 `10` 次 `Accept` 失败后，Tunnel 会放弃并取消自身。

> [!NOTE]
> 退避适用于 Tunnel 监听器上的临时 `Accept` 错误。daemon 停止时，daemon 管理的所有 Tunnel 都会停止。

## 常见模式

访问堡垒机后的数据库：

```bash
sshq config add bastion --hostname bastion.example.com --user deploy --identity ~/.ssh/deploy_ed25519
sshq tunnel start bastion -L 15432:db.internal:5432
psql -h localhost -p 15432 -U app appdb
```

访问私有管理页面：

```bash
sshq tunnel start ops-box -L 18080:admin.internal:8080
open http://localhost:18080
```

暴露本地开发服务：

```bash
npm run dev -- --host 127.0.0.1 --port 3000
sshq tunnel start public-vps -R 18080:localhost:3000
```

短期测试用前台模式，长期运行使用 daemon：

```bash
sshq tunnel start bastion -L 15432:db.internal:5432
sshq daemon start
sshq tunnel start bastion -L 15432:db.internal:5432
sshq tunnel list
```
