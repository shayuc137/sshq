# 文件传输

使用 `sshq cp` 上传、下载文件，或者在两台远端主机之间中继传输。本指南按常见传输任务说明 `cp` 的用法和运行行为。

## 方向推断

`sshq cp` 通过 `alias:path` 语法判断传输方向。

从本地上传到远端：

```bash
sshq cp ./release.tar.gz web-1:/tmp/
```

从远端下载到本地：

```bash
sshq cp web-1:/var/log/nginx/access.log ./logs/
```

从一台远端主机中继到另一台远端主机：

```bash
sshq cp web-1:/var/backups/app.tar.gz backup-1:/srv/backups/
```

至少有一端需要是远端。本地到本地复制会返回错误：

```bash
sshq cp ./a.txt ./b.txt
```

`Windows` 本地盘符路径会被当成本地路径：

```bash
sshq cp C:/Users/alice/release.zip web-1:/tmp/
sshq cp C:\data\release.zip web-1:/tmp/
```

解析器会把 `C:` 当作盘符前缀，因此它会保持为本地路径。

## 传输单个文件

上传到远端目录：

```bash
sshq cp ./dist/app.tar.gz web-1:/tmp/
```

下载到本地目录：

```bash
sshq cp web-1:/var/log/app.log ./logs/
```

目标路径以 `/` 结尾时，`sshq` 会保留源文件名。目标路径包含文件名时，`sshq` 会写入这个路径。

传输期间，进度会写到标准错误，最终结果会写到标准输出。在 `JSON` 模式下，最终结果包含方向、远端路径、大小、耗时、传输引擎和文件数量。

`sshq` 会先写入临时路径，复制成功后再重命名。传输被取消或失败时，最终文件路径不会留下半写入内容。

## 复制目录

目录传输使用 `-r` 或 `--recursive`：

```bash
sshq cp -r ./dist web-1:/srv/app/dist
sshq cp -r web-1:/var/log/myapp ./logs/web-1
sshq cp -r web-1:/srv/app/uploads backup-1:/srv/backups/uploads
```

复制目录时省略 `-r` 会返回错误：

```bash
sshq cp ./dist web-1:/srv/app/dist
```

> [!TIP]
> 递归传输会在最终结果里报告总文件数。

## 服务器之间中继

远端到远端语法用于服务器之间传输：

```bash
sshq cp web-1:/data/export.tar.gz backup-1:/data/imports/
```

`sshq` 会连接两台主机，在源主机打开读取器，在目标主机打开写入器，并让字节流经过本地 `sshq` 进程。本地不会创建临时文件。

本机仍然需要能访问两台主机，并拥有两边凭据。目标路径以 `/` 结尾时，会追加源文件名：

```text
/data/imports/export.tar.gz
```

## 传输引擎

每个远端连接都会先尝试 `SFTP`。`SFTP` 可用时，引擎名是 `sftp`。

在 `POSIX` 类主机上，如果 `SFTP` 不可用，`sshq` 会回退到基于远端 `shell` 的原始字节流。`raw` 引擎使用 `cat` 和临时文件这类简单命令，适合 `BusyBox` 或 `OpenWrt` 这类精简系统。

打开详细输出可以查看选择的引擎：

```bash
sshq -v cp ./config.tar openwrt-1:/tmp/
```

示例输出：

```text
sftp unavailable, using raw stream
transfer engine: raw
```

中继传输中，源端和目标端会分别选择引擎。结果里可能出现 `sftp->raw` 这类混合引擎。

> [!WARNING]
> 对需要 `stdin` 注入的 `Windows` 主机，`raw` 回退不可用。文件传输需要在 `Windows` 的 `OpenSSH` 服务端启用 `SFTP`。

## 路径策略

启用能力策略后，`cp` 会分别检查本地和远端路径是否在白名单范围内：

```bash
sshq policy check prod --remote-path /var/log/app.log
```

- `local_path_whitelist`：限制 `cp` 可以读写的本地目录
- `remote_path_whitelist`：限制 `cp` 可以访问的远端目录

被拦截时返回错误，附带建议的 `policy grant` 命令。白名单配置详见[安全指南](security.md)。

## 控制进度

进度写到标准错误，因此标准输出可以保持为最终结果。使用全局参数 `--no-progress` 关闭进度：

```bash
sshq --no-progress cp ./dist/app.tar.gz web-1:/tmp/
```

这适合只需要最终 `JSON` 结果的 `agent` 或脚本。`--no-progress` 只关闭进度快照；连接消息、回退消息和详细诊断仍然使用标准错误。

## 二进制完整性

文件传输复制字节内容，不会对文件内容做编码转换。文本编码转换只适用于命令输出，不适用于 `cp` 传输内容。

需要证明完整性时，使用校验和。

上传并校验：

```bash
sha256sum ./dist/app.tar.gz
sshq cp ./dist/app.tar.gz web-1:/tmp/
sshq web-1 "sha256sum /tmp/app.tar.gz"
```

下载并校验：

```bash
sshq web-1 "sha256sum /tmp/app.tar.gz"
sshq cp web-1:/tmp/app.tar.gz ./downloads/
sha256sum ./downloads/app.tar.gz
```

中继后校验：

```bash
sshq web-1 "sha256sum /data/export.tar.gz"
sshq cp web-1:/data/export.tar.gz backup-1:/data/
sshq backup-1 "sha256sum /data/export.tar.gz"
```

校验和字符串应保持一致。

## 常见模式

部署构建产物：

```bash
sshq cp ./dist/app.tar.gz web-1:/tmp/
sshq web-1 "mkdir -p /srv/app/releases && tar -xzf /tmp/app.tar.gz -C /srv/app/releases"
```

收集日志：

```bash
mkdir -p ./logs/web-1
sshq cp -r web-1:/var/log/myapp ./logs/web-1
```

把备份复制到备份主机：

```bash
sshq cp web-1:/var/backups/app.sql.gz backup-1:/srv/backups/web-1/
```

在主机之间迁移上传数据：

```bash
sshq cp -r web-1:/srv/app/uploads web-2:/srv/app/uploads
```

把配置包推送到精简路由器：

```bash
sshq cp ./router-config.tar.gz openwrt-1:/tmp/
```
