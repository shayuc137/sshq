# Changelog

## v0.4.1 (2026-08-02)

### Transfer timeout semantics

- `cp` no longer inherits the root 30-second deadline. Transfer time scales with file size, so a fixed wall clock was the wrong unit — it capped uploads at whatever fit in 30 seconds. Pass `--timeout` explicitly to set a ceiling.
- Explicit `cp --timeout` now applies on the daemon path, which previously dropped it: the payload carried no timeout field, so the deadline a caller asked for was silently ignored on the default path.
- cp deadline failures return the `timeout` error code with an actionable hint, in both direct and daemon modes. They previously returned `transfer_failed`, whose advice is to retry — which could only fail the same way.

### exec --raw

- New `--raw` flag for `exec`: mirrors remote stdout/stderr byte-for-byte with no envelope, and the process exit code mirrors the remote exit code verbatim (the one deliberate exception to the tri-state `0/1/2` contract). Mutually exclusive with `--json`/`--pretty`; other commands reject it. Fixes the surprise where `sshq exec web "cat conf" > conf` saved an envelope instead of the file.

### Skill docs

- Generated command references now include the root global flags once per file, including the 30-second `--timeout` default and the exec-only `--raw` constraint.
- Error codes now have one authoritative set, with contract tests keeping the JSON schema and `SKILL.md` table synchronized. The exec/cp flag tables are checked against Cobra, and the cp quick reference now includes `--mkdirs`.
- New `references/windows-encoding.md`: base64 recipes for round-tripping non-ASCII (e.g. Chinese) text with Windows hosts whose sshd chain performs lossy code-page conversion.

### JSON envelope contract

- Every data and error envelope now carries `protocol: "sshq/3"`.
- Execution deadlines now return the machine-readable `timeout` code and advise increasing `--timeout` or running the command in the background.
- Cluster hosts that never produced a remote exit status now return `exit_code: null` instead of the misleading zero value.

## v0.4.0 (2026-07-11)

### BREAKING: envelope schema v3

JSON envelopes and process exit codes are redesigned around one rule: the output explains itself. Anything consuming the v2 envelope must update.

- Success output is `{"data": ...}`; remote command execution adds a top-level `exit_code`. The `ok` and `schema_version` fields are gone — the presence of an `error` object is the failure signal.
- Errors are `{"error": {"code", "hint", "action"}}` with 14 machine-readable codes. `code` tells an agent what to do next; `result_indeterminate` marks operations that may have already executed and must never be blindly retried.
- Process exit codes are tri-state: `0` done with a good answer, `1` done with a bad answer (remote command failed, probe unreachable, doctor found problems, policy check denied), `2` sshq itself failed. Remote exit code passthrough is removed — remote exit 1 was indistinguishable from sshq's own errors; the exact remote code lives in the envelope.
- exec data: `host` is renamed to `alias` (the value always was the alias); the duplicated `exit_code` inside `data` is removed.
- cluster: top-level `exit_code` is removed; read `summary` and per-host `results`.
- `update --check` now exits `0` when an update is available — that is the answer the caller asked for, not a failure. (This reverses the v0.3.1 exit-1 behavior.)
- `trust --all` reports per-host results in `data.results`; list commands render empty results as `[]`, never `null`.

### Daemon freshness

- The daemon hot-reloads `~/.ssh/config` per request, and the shell profile cache is shared between CLI and daemon through the file (write-through with mtime detection): `config add`/`set` and cache changes apply without restarting the daemon.
- Stale shell profiles self-heal: a strong shell-mismatch signal invalidates the cached entry and appends a retry hint. Commands are never re-executed automatically.
- `sshq doctor`'s shell check bypasses the cache and repairs it with what it actually finds.
- New command: `sshq cache clear [alias]`.

### Documentation

- README rewritten in both languages: problem/answer intro, a Trust & privacy section, and sudo-free installation to `~/.local/bin`.
- Every JSON example across the skill package and guides shows the v3 envelope; the skill includes an `error.code` quick-reference table for agents.

### CI

- GitHub Actions bumped to Node 24 native majors (checkout v7, setup-go v6, goreleaser-action v7).

## v0.3.1 (2026-07-10)

### Self-update

- Added `sshq update` to explicitly update the release binary and every installed AI skill.
- Added `sshq update --check` with stable process exits: `0` when current, `1` when an update is available, and `2` on operational failure.
- Release downloads require the matching `checksums.txt` entry and SHA-256 verification before archive extraction or binary replacement.
- Binary replacement uses a same-directory staged file, backup, atomic rename, and rollback; permission failures preserve the verified binary and print an English recovery action.
- GoReleaser now publishes stable asset aliases such as `sshq_linux_amd64.tar.gz` alongside versioned archives.

### Windows reliability

- Fixed Windows profile detection when OpenSSH uses its default `cmd.exe` shell by invoking PowerShell explicitly with a shared UTF-16LE encoded payload.
- Added real Windows updater coverage for replacing a running executable and cleaning the retained `.old` file on the next update.

### Documentation

- README and getting-started installation links now use permanent `releases/latest/download` asset names.
- Skill routing and generated command references document the new update workflow and exit-code contract.

## v0.3.0 (2026-07-10)

### Agent and connection ergonomics

- Added `sshq doctor` for one-command checks of configuration, identity, ProxyJump, TCP, host key, authentication, and remote shell detection.
- Unified probe, trust, and exec connection paths so ProxyJump behavior is consistent.
- Added top-level remote `exit_code` to the schema-v2 JSON envelope while retaining the field inside `data` for compatibility.
- Made shortcut execution share the canonical exec flags and added command-tree documentation contract tests.

### Windows support

- Stabilized PowerShell `--script-file` with UTF-16LE `-EncodedCommand` for small scripts and upload-run cleanup for larger scripts.
- Added PowerShell 7 path detection, `cp --mkdirs`, Windows cluster output encoding fixes, and Windows support recipes.

### Skill maintenance

- Added `sshq skill update` and outdated-skill reminders.
- Added automatic VCS version discovery and a `--version` flag.

## v0.2.0 (2026-07-02)

### Security layer

- **Encrypted credential store**: passwords encrypted with age using SSH public key, stored in OS config directory. `sshq credential set/list/delete`. No command prints stored passwords.
- **Capability policy**: command whitelist/blacklist, local/remote path whitelist, per-host override/append mode, all configured in `config.toml`. `sshq policy validate/check/list/grant/revoke`.
- **Tunnel forward whitelist**: `local_forward_whitelist` / `remote_forward_whitelist` restrict which targets tunnels can reach. Matching supports exact, port wildcard, port range, and host wildcard.
- **Temporary grants**: `sshq policy grant` with TTL, requires controlling terminal (agents cannot self-grant), never overrides blacklists.
- **Audit logging**: JSONL metadata for exec, cp, tunnel, cluster, and policy-blocked operations. SHA-256 hash for script-file. Rotation at configurable max size. `sshq audit` query CLI. Fail-closed when log is unwritable.

### New commands

- `sshq docs --verify <dir>`: detect drift between cobra command tree and generated skill reference docs. Exit 0 when consistent, exit 1 with diff summary.
- `sshq policy check --local-forward/--remote-forward`: test tunnel forward policy decisions.

### CLI improvements

- `sshq skill install` simplified: `--target`/`--scope` replaced with `--codex`/`--project` boolean flags.
- Connecting status moved to verbose output — stderr is silent by default for direct connections.

### Documentation

- README rewritten with problem-driven intro, TTY before/after demo, security model section, and platform-specific install instructions (no Go required).
- All 8 bilingual guides updated with policy/audit coverage.
- SKILL.md restructured: environment checks, safety confirmations, expanded routing table, JSON-first error handling.
- Reference generator enhanced with output fields, exit behavior, agent handling notes, and security annotations per command group.
- Bilingual (English + Chinese) guides fixed: broken links, stale titles, over-backticked terms.

## v0.1.0 (2026-06-28)

Initial release.

- exec with TTY auto-detect (pipe → JSON, terminal → pretty), stdout purity guarantee
- cp with SFTP + raw byte stream fallback, server-to-server relay
- daemon connection pool with graceful degradation
- cross-shell detection (bash/ash/powershell/cmd) and automatic command wrapping
- cluster concurrent execution with tag/env/hosts filtering
- local/remote SSH tunnels with exponential backoff reconnect
- ProxyJump multi-hop chain support
- config CRUD with lossless SSH config preservation
- sshq metadata (tags, env, description) as namespaced comments
- `sshq skill install` for Claude Code and Codex
- `sshq docs --skill` auto-generates scenario-grouped reference docs
- goreleaser + GitHub Actions for Linux/macOS/Windows amd64+arm64 releases
