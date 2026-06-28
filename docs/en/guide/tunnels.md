# Tunnels

`sshq tunnel` manages SSH port forwarding. Use it to access remote services locally or expose a local service through a remote host.

| Direction | Flag | Use case |
| --- | --- | --- |
| Local | `-L localport:remotehost:remoteport` | Local port reaches a service visible from the SSH host. |
| Remote | `-R remoteport:localhost:localport` | Remote port reaches a service on your machine. |

## Local Forwarding

Local forwarding listens on your machine and forwards traffic through SSH.

```bash
sshq tunnel start bastion -L 15432:db.internal:5432
```

This creates:

```text
localhost:15432 -> db.internal:5432 via bastion
```

Connect locally:

```bash
psql -h localhost -p 15432 -U app appdb
```

`db.internal` is resolved from the SSH host side, so it only needs to be reachable from `bastion`.

> [!TIP]
> Local forwarding is the usual choice for databases, admin panels, metrics endpoints, and private HTTP services behind a bastion.

Examples:

```bash
sshq tunnel start bastion -L 15432:db.internal:5432
sshq tunnel start ops-box -L 18080:dashboard.internal:80
sshq tunnel start cache-gw -L 16379:redis.internal:6379
```

## Remote Forwarding

Remote forwarding listens on the remote side and forwards traffic back to your machine.

```bash
sshq tunnel start public-vps -R 18080:localhost:3000
```

This creates:

```text
public-vps:18080 -> localhost:3000 on your machine
```

On the remote host:

```bash
curl http://localhost:18080
```

Your local service must be listening on `localhost:3000`.

> [!WARNING]
> Remote forwarding can be limited by SSH server settings such as `AllowTcpForwarding` and `GatewayPorts`.

Examples:

```bash
sshq tunnel start public-vps -R 18080:localhost:3000
sshq tunnel start webhook-host -R 19090:localhost:9090
sshq tunnel start staging -R 18081:localhost:5173
```

## Forward Spec Format

Use the explicit local form:

```bash
sshq tunnel start <alias> -L <local_port>:<remote_host>:<remote_port>
```

Use the explicit remote form:

```bash
sshq tunnel start <alias> -R <remote_port>:<local_host>:<local_port>
```

`sshq` binds the local side of each spec to `localhost:<port>`. Two-part shorthand is accepted and maps both host names to `localhost`:

```bash
sshq tunnel start bastion -L 8080:80
sshq tunnel start public-vps -R 18080:3000
```

## Foreground And Background

When the daemon is running, `tunnel start` registers a background tunnel and returns a tunnel ID.

```bash
sshq daemon start
sshq tunnel start bastion -L 15432:db.internal:5432
```

```text
tun-1 localhost:15432 -> db.internal:5432 via bastion
```

When the daemon is not running, `tunnel start` falls back to a foreground tunnel and stays attached until `Ctrl+C`.

```text
connecting to bastion...
local forward localhost:15432 -> db.internal:5432 via bastion
tunnel running, press Ctrl+C to stop
```

## Multiple Tunnels

Start the daemon first when you want several independent tunnels.

```bash
sshq daemon start
sshq tunnel start bastion -L 15432:db.internal:5432
sshq tunnel start bastion -L 18080:dashboard.internal:80
sshq tunnel start public-vps -R 19090:localhost:9090
sshq tunnel list
sshq tunnel stop tun-2
```

Example `list` output:

```text
tun-1 local localhost:15432 -> db.internal:5432 via bastion conns=1
tun-2 local localhost:18080 -> dashboard.internal:80 via bastion conns=0
tun-3 remote localhost:19090 -> localhost:9090 via public-vps conns=0
```

If the daemon is unreachable, `tunnel list` prints `no active tunnels`, and `tunnel stop <id>` returns `daemon not running`.

## Lifecycle

Each tunnel accepts connections until it is stopped or canceled. Transient listener accept errors are retried with jittered exponential backoff.

The retry delay starts at `100ms`, grows up to `5s`, and resets after a successful accepted connection. After `10` consecutive accept failures, the tunnel gives up and cancels itself.

> [!NOTE]
> Backoff applies to transient accept failures on the tunnel listener. If the daemon is stopped, all daemon-owned tunnels are stopped too.

## Common Patterns

Access a database behind a bastion:

```bash
sshq config add bastion --hostname bastion.example.com --user deploy --identity ~/.ssh/deploy_ed25519
sshq tunnel start bastion -L 15432:db.internal:5432
psql -h localhost -p 15432 -U app appdb
```

Access a private admin UI:

```bash
sshq tunnel start ops-box -L 18080:admin.internal:8080
open http://localhost:18080
```

Expose a local dev server:

```bash
npm run dev -- --host 127.0.0.1 --port 3000
sshq tunnel start public-vps -R 18080:localhost:3000
```

Use foreground for short tests and the daemon for long-running tunnels:

```bash
sshq tunnel start bastion -L 15432:db.internal:5432
sshq daemon start
sshq tunnel start bastion -L 15432:db.internal:5432
sshq tunnel list
```
