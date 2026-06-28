# File Transfer

Use `sshq cp` to upload, download, and relay files between configured SSH hosts. This guide covers common transfer tasks and the runtime behavior behind them.

## Direction Inference

`sshq cp` decides the transfer direction from `alias:path` syntax.

Upload from local to remote:

```bash
sshq cp ./release.tar.gz web-1:/tmp/
```

Download from remote to local:

```bash
sshq cp web-1:/var/log/nginx/access.log ./logs/
```

Relay from one remote host to another:

```bash
sshq cp web-1:/var/backups/app.tar.gz backup-1:/srv/backups/
```

At least one side must be remote. Local-to-local copy returns an error:

```bash
sshq cp ./a.txt ./b.txt
```

Windows local drive paths are treated as local paths:

```bash
sshq cp C:/Users/alice/release.zip web-1:/tmp/
sshq cp C:\data\release.zip web-1:/tmp/
```

The parser treats `C:` as a drive prefix, so it remains local.

## Transfer One File

Upload to a remote directory:

```bash
sshq cp ./dist/app.tar.gz web-1:/tmp/
```

Download into a local directory:

```bash
sshq cp web-1:/var/log/app.log ./logs/
```

If the destination ends with `/`, `sshq` keeps the source file name. If the destination includes a file name, `sshq` writes to that path.

During transfer, progress is written to stderr and the final result is written to stdout. In JSON mode, the final result includes direction, remote path, size, duration, engine, and file count.

`sshq` writes to a temporary path first and renames it after the copy succeeds. This avoids leaving a partially written final file when a transfer is cancelled or fails.

## Copy Directories

Use `-r` or `--recursive` for directories:

```bash
sshq cp -r ./dist web-1:/srv/app/dist
sshq cp -r web-1:/var/log/myapp ./logs/web-1
sshq cp -r web-1:/srv/app/uploads backup-1:/srv/backups/uploads
```

Without `-r`, copying a directory returns an error:

```bash
sshq cp ./dist web-1:/srv/app/dist
```

> [!TIP]
> Recursive transfers report a total file count in the final result.

## Server-to-Server Relay

Use remote-to-remote syntax for server-to-server transfer:

```bash
sshq cp web-1:/data/export.tar.gz backup-1:/data/imports/
```

`sshq` connects to both hosts, opens a reader on the source, opens a writer on the destination, and streams bytes through the local `sshq` process. No local temporary file is created.

The local machine still needs network access and credentials for both hosts. If the destination ends with `/`, the source file name is appended:

```text
/data/imports/export.tar.gz
```

## Transfer Engine

For each remote connection, `sshq` tries SFTP first. When SFTP is available, the engine name is `sftp`.

When SFTP is unavailable on POSIX-like hosts, `sshq` falls back to a raw byte stream over the remote shell. The raw engine uses simple commands such as `cat` plus temporary files, which helps minimal systems such as BusyBox or OpenWrt devices.

Run with verbose output to see the selected engine:

```bash
sshq -v cp ./config.tar openwrt-1:/tmp/
```

Example output:

```text
sftp unavailable, using raw stream
transfer engine: raw
```

For relay transfers, source and destination engines are selected independently. The result can show a mixed engine such as `sftp->raw`.

> [!WARNING]
> On Windows hosts that require stdin injection, raw stream fallback is unavailable. Enable SFTP on the Windows SSH server for file transfer.

## Progress Control

Progress is written to stderr so stdout remains clean for the final result. Suppress progress with the global `--no-progress` flag:

```bash
sshq --no-progress cp ./dist/app.tar.gz web-1:/tmp/
```

This is useful for agents or scripts that only need the final JSON result. `--no-progress` only suppresses progress snapshots; connection messages, fallback messages, and verbose diagnostics still use stderr.

## Binary Integrity

File transfers copy bytes and do not transcode file content. Text encoding conversion applies to command output, not to `cp` payloads.

Use checksums when you need proof of integrity.

Upload and verify:

```bash
sha256sum ./dist/app.tar.gz
sshq cp ./dist/app.tar.gz web-1:/tmp/
sshq web-1 "sha256sum /tmp/app.tar.gz"
```

Download and verify:

```bash
sshq web-1 "sha256sum /tmp/app.tar.gz"
sshq cp web-1:/tmp/app.tar.gz ./downloads/
sha256sum ./downloads/app.tar.gz
```

Relay and verify:

```bash
sshq web-1 "sha256sum /data/export.tar.gz"
sshq cp web-1:/data/export.tar.gz backup-1:/data/
sshq backup-1 "sha256sum /data/export.tar.gz"
```

The checksum strings should match.

## Common Patterns

Deploy an artifact:

```bash
sshq cp ./dist/app.tar.gz web-1:/tmp/
sshq web-1 "mkdir -p /srv/app/releases && tar -xzf /tmp/app.tar.gz -C /srv/app/releases"
```

Collect logs:

```bash
mkdir -p ./logs/web-1
sshq cp -r web-1:/var/log/myapp ./logs/web-1
```

Copy a backup to a backup host:

```bash
sshq cp web-1:/var/backups/app.sql.gz backup-1:/srv/backups/web-1/
```

Migrate uploaded data between hosts:

```bash
sshq cp -r web-1:/srv/app/uploads web-2:/srv/app/uploads
```

Push a config bundle to a minimal router:

```bash
sshq cp ./router-config.tar.gz openwrt-1:/tmp/
```
