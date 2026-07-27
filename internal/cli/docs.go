package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func newDocsCommand() *cobra.Command {
	var skill bool
	var verify bool
	cmd := &cobra.Command{
		Use:    "docs <output-dir>",
		Short:  "Generate command reference documentation",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			root := cmd.Root()
			root.DisableAutoGenTag = true

			if verify {
				return verifySkillDocs(cmd, root, dir)
			}

			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("create dir: %w", err)
			}
			if skill {
				return genSkillDocs(root, dir)
			}
			return doc.GenMarkdownTree(root, dir)
		},
	}
	cmd.Flags().BoolVar(&skill, "skill", false, "generate skill reference docs (grouped by scenario)")
	cmd.Flags().BoolVar(&verify, "verify", false, "verify skill docs are up-to-date with cobra command tree")
	return cmd
}

var skillGroups = []struct {
	file     string
	title    string
	intro    string
	cmds     []string
	appendix string
}{
	{
		file:     "exec-transfer.md",
		title:    "Execution & File Transfer",
		intro:    "Commands for running remote commands and transferring files.",
		cmds:     []string{"exec", "cp"},
		appendix: execTransferAppendix,
	},
	{
		file:     "config.md",
		title:    "Configuration Management",
		intro:    "Commands for managing SSH host configuration and sshq metadata.",
		cmds:     []string{"config", "config add", "config set", "config remove", "config list"},
		appendix: configMetadataRef,
	},
	{
		file:     "cluster-tunnel.md",
		title:    "Cluster & Tunnel",
		intro:    "Commands for concurrent multi-host operations and port forwarding.",
		cmds:     []string{"cluster", "cluster exec", "tunnel", "tunnel start", "tunnel stop", "tunnel list"},
		appendix: clusterTunnelAppendix,
	},
	{
		file:     "policy.md",
		title:    "Policy & Audit",
		intro:    "Commands for validating capability policy, managing temporary daemon grants, and querying audit logs.",
		cmds:     []string{"policy", "policy grant", "policy revoke", "policy list", "policy validate", "policy check", "audit"},
		appendix: policyAppendix,
	},
	{
		file:     "discovery.md",
		title:    "Discovery & Daemon",
		intro:    "Commands for listing, searching, inspecting hosts, credentials, and managing the daemon.",
		cmds:     []string{"ls", "search", "info", "probe", "doctor", "trust", "credential", "credential set", "credential delete", "credential list", "daemon", "daemon start", "daemon stop", "daemon status", "cache", "cache clear", "version", "update", "skill", "skill install", "skill update", "skill export", "skill status"},
		appendix: discoveryAppendix,
	},
	{
		file:     "windows-paths.md",
		title:    "Windows Path Recipes",
		intro:    "Canonical Windows path forms for remote execution and file transfer.",
		appendix: windowsPathRecipes,
	},
	{
		file:     "windows-encoding.md",
		title:    "Windows Non-ASCII Output Recipes",
		intro:    "Reliable round-tripping of non-ASCII (e.g. Chinese) text with Windows hosts.",
		appendix: windowsEncodingRecipes,
	},
	{
		file:     "windows-background.md",
		title:    "Windows Background Task Recipes",
		intro:    "Durable Windows background work using Task Scheduler.",
		appendix: windowsBackgroundRecipes,
	},
	{
		file:     "remote-windows-support.md",
		title:    "Remote Windows Support Recipe",
		intro:    "A complete temporary-support flow from WireGuard reachability through cleanup.",
		appendix: remoteWindowsSupportRecipe,
	},
}

func genSkillDocs(root *cobra.Command, dir string) error {
	index := buildCmdIndex(root, "")

	for _, g := range skillGroups {
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n", g.title)
		fmt.Fprintf(&b, "Documentation version: `sshq v%s`.\n\n", version.Number())
		fmt.Fprintf(&b, "%s\n\n", g.intro)
		b.WriteString("Auto-generated from `sshq docs --skill`. Do not edit manually.\n\n")

		for _, name := range g.cmds {
			cmd, ok := index[name]
			if !ok {
				continue
			}
			depth := strings.Count(name, " ")
			hdr := "##"
			if depth > 0 {
				hdr = "###"
			}
			fmt.Fprintf(&b, "%s sshq %s\n\n", hdr, name)

			b.WriteString("```\n")
			b.WriteString(cmd.UseLine())
			b.WriteString("\n```\n\n")

			if cmd.Long != "" {
				b.WriteString(cmd.Long)
			} else if cmd.Short != "" {
				b.WriteString(cmd.Short)
			}
			b.WriteString("\n\n")

			if cmd.Example != "" {
				b.WriteString("**Examples:**\n\n```bash\n")
				b.WriteString(strings.TrimSpace(cmd.Example))
				b.WriteString("\n```\n\n")
			}

			flags := cmd.NonInheritedFlags()
			if flags.HasFlags() {
				b.WriteString("**Flags:**\n\n```\n")
				b.WriteString(strings.TrimRight(flags.FlagUsages(), "\n"))
				b.WriteString("\n```\n\n")
			}
		}

		if g.appendix != "" {
			b.WriteString(g.appendix)
		}

		path := dir + "/" + g.file
		if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

func buildCmdIndex(cmd *cobra.Command, prefix string) map[string]*cobra.Command {
	idx := make(map[string]*cobra.Command)
	for _, c := range cmd.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" || c.Name() == "docs" {
			continue
		}
		name := c.Name()
		if prefix != "" {
			name = prefix + " " + name
		}
		idx[name] = c
		for k, v := range buildCmdIndex(c, name) {
			idx[k] = v
		}
	}
	return idx
}

// DocDrift holds the result of comparing generated docs against existing ones.
type DocDrift struct {
	Consistent bool       `json:"consistent"`
	Added      []string   `json:"added,omitempty"`
	Removed    []string   `json:"removed,omitempty"`
	Changed    []FileDiff `json:"changed,omitempty"`
}

type FileDiff struct {
	File    string `json:"file"`
	Summary string `json:"summary"`
}

func (d DocDrift) Pretty() string {
	var b strings.Builder
	for _, f := range d.Added {
		fmt.Fprintf(&b, "  + %s (new, not in target)\n", f)
	}
	for _, f := range d.Removed {
		fmt.Fprintf(&b, "  - %s (in target, no longer generated)\n", f)
	}
	for _, c := range d.Changed {
		fmt.Fprintf(&b, "  ~ %s: %s\n", c.File, c.Summary)
	}
	return strings.TrimRight(b.String(), "\n")
}

func verifySkillDocs(cmd *cobra.Command, root *cobra.Command, dir string) error {
	w := writerFrom(cmd.Context())

	tmpDir, err := os.MkdirTemp("", "sshq-docs-verify-*")
	if err != nil {
		return output.Errorf("create temp dir: "+err.Error(), "").WithCode(output.CodeInternalError)
	}
	defer os.RemoveAll(tmpDir)

	if err := genSkillDocs(root, tmpDir); err != nil {
		return output.Errorf("generate docs failed: "+err.Error(), "").WithCode(output.CodeInternalError)
	}

	drift := compareDocDirs(tmpDir, dir)
	if drift.Consistent {
		w.Success("docs are up-to-date with cobra command tree")
		return nil
	}

	w.Render(drift)
	w.Info("run: sshq docs --skill " + dir)
	return &exec.ExitError{Code: 1}
}

func compareDocDirs(generatedDir, targetDir string) DocDrift {
	genFiles := listMDFiles(generatedDir)
	targetFiles := listMDFiles(targetDir)

	var drift DocDrift

	for _, f := range genFiles {
		if !contains(targetFiles, f) {
			drift.Added = append(drift.Added, f)
		}
	}

	for _, f := range targetFiles {
		if !contains(genFiles, f) {
			drift.Removed = append(drift.Removed, f)
		}
	}

	for _, f := range genFiles {
		if !contains(targetFiles, f) {
			continue
		}
		genContent, err1 := os.ReadFile(filepath.Join(generatedDir, f))
		targetContent, err2 := os.ReadFile(filepath.Join(targetDir, f))
		if err1 != nil || err2 != nil {
			continue
		}
		if !bytes.Equal(genContent, targetContent) {
			summary := docDiffSummary(string(targetContent), string(genContent))
			drift.Changed = append(drift.Changed, FileDiff{File: f, Summary: summary})
		}
	}

	drift.Consistent = len(drift.Added) == 0 && len(drift.Removed) == 0 && len(drift.Changed) == 0
	return drift
}

func listMDFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func docDiffSummary(existing, generated string) string {
	oldCmds := extractDocCommands(existing)
	newCmds := extractDocCommands(generated)

	var parts []string
	for _, c := range newCmds {
		if !contains(oldCmds, c) {
			parts = append(parts, "+cmd: "+c)
		}
	}
	for _, c := range oldCmds {
		if !contains(newCmds, c) {
			parts = append(parts, "-cmd: "+c)
		}
	}

	if len(parts) == 0 {
		parts = append(parts, "content changed (flags/usage)")
	}
	return strings.Join(parts, ", ")
}

func extractDocCommands(content string) []string {
	var cmds []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## sshq ") || strings.HasPrefix(line, "### sshq ") {
			cmd := strings.TrimPrefix(line, "### ")
			cmd = strings.TrimPrefix(cmd, "## ")
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

const execTransferAppendix = "---\n" +
	"\n## Agent notes\n\n" +
	"### exec output contract\n\n" +
	"In pretty mode, process stdout is the remote stdout exactly — sshq never writes its own messages there. " +
	"In JSON mode, `data.stdout` and `data.stderr` carry the remote streams separately.\n\n" +
	"| Field | Type | Description |\n" +
	"|-------|------|-------------|\n" +
	"| `exit_code` | int | Remote process exit code (0 = success) |\n" +
	"| `stdout` | string | Remote stdout verbatim |\n" +
	"| `stderr` | string | Remote stderr verbatim |\n" +
	"| `alias` | string | SSH config alias used |\n" +
	"| `duration_ms` | int | Wall-clock milliseconds |\n\n" +
	"A completed exec has `data` plus one top-level `exit_code`; `data` does not repeat the code. " +
	"An sshq-level failure has `error.code`, `error.hint`, and `error.action`, with no `data` or `exit_code`.\n\n" +
	"Remote success: `{\"exit_code\":0,\"data\":{\"stdout\":\"web-1\\n\",\"stderr\":\"\",\"alias\":\"web-1\",\"duration_ms\":42}}`\n\n" +
	"Remote failure: `{\"exit_code\":3,\"data\":{\"stdout\":\"\",\"stderr\":\"command failed\\n\",\"alias\":\"web-1\",\"duration_ms\":42}}`\n\n" +
	"### PowerShell script files\n\n" +
	"For complex Windows commands, prefer `--script-file <path> --shell powershell`; this avoids local-shell expansion of PowerShell `$variables` and nested quoting. " +
	"Scripts up to 8 KiB run as `powershell -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand <base64(UTF-16LE(script))>`. " +
	"Larger scripts automatically use upload-run: sshq uploads a UTF-8-with-BOM temporary `.ps1`, executes it with the same flags plus `-File`, then removes the remote file. " +
	"The bash, ash, sh, and zsh script paths continue to execute through stdin.\n\n" +
	"### cp output contract\n\n" +
	"| Field | Type | Description |\n" +
	"|-------|------|-------------|\n" +
	"| `direction` | string | upload / download / relay |\n" +
	"| `remote` | string | Remote path |\n" +
	"| `size` | int | Total bytes transferred |\n" +
	"| `duration` | string | Human-readable duration |\n" +
	"| `engine` | string | sftp / raw / sftp→sftp |\n" +
	"| `files` | int | File count |\n\n" +
	"### Exit behavior\n\n" +
	"- Exit 0: sshq completed and the result is successful\n" +
	"- Exit 1: sshq completed but the result is unsuccessful; for exec, read the exact remote code from top-level `exit_code`\n" +
	"- Exit 2: sshq could not complete the operation; inspect `error.code`, `error.hint`, and `error.action`\n\n" +
	"### Security\n\n" +
	"- exec: checked against `command_whitelist` / `command_blacklist`\n" +
	"- cp: local and remote paths checked against `local_path_whitelist` / `remote_path_whitelist`\n" +
	"- `--script-file`: audit records SHA-256 hash and byte count, not the script content\n"

const windowsPathRecipes = `---

## Canonical path form

Use forward slashes in Windows remote paths. This keeps the alias:path boundary unambiguous and works with the SFTP path model:

~~~text
company-win:C:/Users/support/Desktop/report.txt
company-win:C:/Program Files/Support Tool/config.json
~~~

Quote the complete endpoint when a path contains spaces. Add --mkdirs when the remote parent directory may not exist:

~~~bash
sshq cp --mkdirs ./support.exe 'company-win:C:/Program Files/Support Tool/support.exe'
sshq cp 'company-win:C:/Program Files/Support Tool/support.log' './support logs/'
~~~

A local Windows drive path such as C:/Temp/input.txt remains local; a remote Windows path always includes the alias prefix:

~~~bash
sshq cp 'C:/Temp/input.txt' 'company-win:C:/Users/support/Desktop/input.txt'
~~~

For complex PowerShell expressions or paths containing PowerShell variables, put the script in a local .ps1 file:

~~~bash
sshq exec company-win --script-file ./inspect-paths.ps1 --shell powershell
~~~
`

const windowsEncodingRecipes = `---

## When you need this

On some Windows hosts (observed: Chinese Windows + PowerShell 5.1), remote output containing non-ASCII text arrives garbled: UTF-8 bytes are decoded as GBK somewhere in the sshd forwarding chain, and illegal sequences are replaced with U+FFFD. Once that replacement happens the original bytes are gone — no client-side decoding can recover them, and setting [Console]::OutputEncoding on the remote side does not help.

Pure-ASCII output is unaffected. Skip these recipes when the command output contains no non-ASCII text.

The reliable workaround keeps every byte on the wire ASCII: encode with base64 on the remote side, decode locally. This survives any code page.

## Receiving non-ASCII output

In the remote script, serialize to JSON, encode the UTF-8 bytes as base64, and emit fixed-width lines:

~~~powershell
$data  = Get-ChildItem 'C:/Users/support/Desktop' | Select-Object Name, LastWriteTime
$json  = $data | ConvertTo-Json -Compress
$b64   = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
for ($i = 0; $i -lt $b64.Length; $i += 120) {
    $b64.Substring($i, [Math]::Min(120, $b64.Length - $i))
}
~~~

Decode locally by joining the lines:

~~~bash
sshq exec company-win --script-file ./list-desktop.ps1 --shell powershell \
  | jq -r '.data.stdout' | tr -d '\n' | base64 -d
~~~

## Sending non-ASCII arguments

Non-ASCII literals written directly into a script (paths, filenames) are damaged in transit the same way. Encode them locally and decode inside the script:

~~~bash
B64=$(printf '%s' "AI业务操作台" | base64 -w0)
cat > open-dir.ps1 <<EOF
\$name = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$B64'))
Get-ChildItem -LiteralPath "C:/Users/support/Desktop/\$name"
EOF
sshq exec company-win --script-file ./open-dir.ps1 --shell powershell
~~~

## Status

This is a workaround, not a fix. The layer performing the lossy GBK decode is unconfirmed (suspected: Windows OpenSSH sshd console code-page conversion), so an in-band fix such as an encoding flag cannot work yet. The recipes above were validated in production use — six calls, zero corruption.
`

const windowsBackgroundRecipes = `---

## Prefer Task Scheduler

Use a scheduled task for work that must survive the SSH session. Start-Process can remain tied to the session/job object and provides weaker query and cleanup behavior. Task Scheduler gives explicit create, query, and delete operations.

Create a startup task that runs as SYSTEM:

~~~bash
sshq exec company-win 'schtasks /Create /TN "sshq-support" /SC ONSTART /RU SYSTEM /TR "C:\Program Files\Support Tool\support.exe" /F'
~~~

Query its definition and latest result:

~~~bash
sshq exec company-win 'schtasks /Query /TN "sshq-support" /V /FO LIST'
~~~

Delete it during cleanup:

~~~bash
sshq exec company-win 'schtasks /Delete /TN "sshq-support" /F'
~~~

For commands with PowerShell variables, multiple actions, or nested quoting, write a .ps1 file and execute it with --script-file rather than expanding the one-line command.
`

const remoteWindowsSupportRecipe = `---

## 1. Add temporary WireGuard reachability

Allocate one peer address and route only that address. The hub-side shape is:

~~~text
[Peer]
PublicKey = <windows-peer-public-key>
AllowedIPs = 10.0.0.4/32
~~~

Install the matching peer configuration on Windows, bring up the tunnel, and confirm the support machine can reach the assigned address before changing SSH configuration.

## 2. Enable OpenSSH Server on Windows

Run these commands in an elevated PowerShell console on the Windows machine:

~~~powershell
Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Set-Service -Name sshd -StartupType Automatic
Start-Service sshd
New-NetFirewallRule -Name sshq-temp-sshd -DisplayName 'sshq temporary OpenSSH' -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22 -RemoteAddress 10.0.0.0/24
~~~

## 3. Install a temporary support key

Create a dedicated key locally, then append its public key to the target user's %USERPROFILE%/.ssh/authorized_keys. Restrict the .ssh directory and file to that user and SYSTEM; Windows OpenSSH rejects overly broad ACLs.

## 4. Register and diagnose the host

~~~bash
sshq config add support-win --hostname 10.0.0.4 --user support --identity ~/.ssh/support-win
sshq doctor support-win
~~~

Read the resolved alias, hostname, user, port, identity file, and ProxyJump values from config add; continue only after doctor passes the connection checks.

## 5. Execute and transfer

~~~bash
sshq exec support-win --script-file ./diagnose.ps1 --shell powershell
sshq cp --mkdirs ./support.exe 'support-win:C:/Program Files/Support Tool/support.exe'
sshq cp 'support-win:C:/ProgramData/Support Tool/diagnostics.zip' ./diagnostics.zip
~~~

## 6. Clean up

Remove temporary artifacts and scheduled tasks while the connection still works, then remove the sshq alias, the authorized key line, the temporary firewall rule, and the WireGuard peer:

~~~bash
sshq exec support-win 'schtasks /Delete /TN "sshq-support" /F'
sshq exec support-win 'powershell -NoProfile -Command "Remove-Item -LiteralPath ''C:/Program Files/Support Tool'' -Recurse -Force"'
sshq config remove support-win
~~~

Finally remove sshq-temp-sshd if it was created only for the support window, and delete the peer from both WireGuard endpoints.
`

const clusterTunnelAppendix = "---\n" +
	"\n## Agent notes\n\n" +
	"### cluster output contract\n\n" +
	"JSON mode returns `data: {results: [{alias, stdout, stderr, exit_code, error}], summary: {total, success, failed}}`. " +
	"Cluster envelopes have no top-level `exit_code`; read `summary` for the aggregate result and each `results[].exit_code` for an individual remote command. " +
	"The process exits 1 when any host fails.\n\n" +
	"### cluster policy pre-flight\n\n" +
	"After selector resolution, sshq checks policy for all targets before execution. " +
	"If any host is blocked, no hosts execute. A pre-flight block is a `policy_blocked` error envelope with process exit 2, not a partial-failure data result.\n\n" +
	"### tunnel output contract\n\n" +
	"tunnel start returns `{id, direction, local_addr, remote_addr}`. " +
	"tunnel list returns an array of `{id, direction, alias, local_addr, remote_addr, active_connections}`.\n\n" +
	"### tunnel forward whitelist\n\n" +
	"When capability policy is enabled:\n" +
	"- `-L` checks `local_forward_whitelist` against the remote target (`remote_host:remote_port`)\n" +
	"- `-R` checks `remote_forward_whitelist` against the local target (`local_host:local_port`)\n\n" +
	"Matching supports exact (`host:port`), port wildcard (`host:*`), port range (`host:8000-9000`), and host wildcard (`*:port`).\n\n" +
	"### Daemon vs foreground\n\n" +
	"With daemon running, tunnels are background and managed via `tunnel list` / `tunnel stop`. " +
	"Without daemon, tunnel runs in foreground until Ctrl+C.\n"

const policyAppendix = "---\n" +
	"\n## Agent notes\n\n" +
	"### policy check output\n\n" +
	"Returns `{decision: {allowed, alias, kind, reason, pattern, input}}`. " +
	"Exit 0 means allowed; exit 1 means denied. Both are completed data results.\n\n" +
	"### policy grant behavior\n\n" +
	"- Requires a controlling TTY (agents cannot self-grant)\n" +
	"- TTL maximum is 1 hour\n" +
	"- Grants live only in daemon memory; daemon restart clears them\n" +
	"- Grants never override `command_blacklist` — blacklist always wins\n" +
	"- Supported kinds: `command`, `local-path`, `remote-path`, `local-forward`, `remote-forward`\n\n" +
	"### audit output\n\n" +
	"Returns an array of JSONL entries with: `timestamp`, `alias`, `operation`, `summary`, `result` (success/error/blocked), `duration_ms`, `source` (direct/daemon), `exit_code`.\n\n" +
	"Blocked entries include `blocked_by` (reason) and `matched_pattern`.\n"

const discoveryAppendix = "---\n" +
	"\n## Agent notes\n\n" +
	"### doctor output contract\n\n" +
	"`doctor <alias>` runs ordered configuration, identity file, ProxyJump, TCP, host key, authentication, and shell checks. " +
	"Later checks are `\"skipped\"` after a failed prerequisite; an unconfigured identity file is `null`. " +
	"A completed diagnosis returns a `data` envelope, uses exit 0 when every applicable check passes and exit 1 when any check fails, and provides `next_action` only when the command is executable in the current state.\n\n" +
	"### Security-sensitive commands\n\n" +
	"- `trust --replace`: overwrites a known host key — ask user first (possible MITM)\n" +
	"- `credential set`: requires TTY for password input — relay to user\n" +
	"- `credential delete`: permanently deletes a stored password — ask user first\n" +
	"- `credential list`: only shows aliases, never prints passwords\n\n" +
	"### daemon status output\n\n" +
	"Returns `{running, uptime_seconds, connections: [{alias, host, idle}]}`.\n\n" +
	"### update exit codes\n\n" +
	"`update --check` returns exit 0 when the check completes, whether current or an update is available, and exit 2 when the check fails. " +
	"In JSON mode, inspect `data.update_available`; top-level `exit_code` is only present for a single remote command result.\n\n" +
	"### skill commands\n\n" +
	"- `skill install`: installs sshq skill to Claude Code (`--codex` for Codex, `--project` for project-level)\n" +
	"- `skill update`: refreshes every existing installation in place and skips targets that are not installed\n" +
	"- `skill status`: shows install location and version\n" +
	"- `skill export`: writes embedded skill files to a selected directory\n\n" +
	"When an installed skill differs from the running binary, sshq prints a deduplicated stderr reminder to run `sshq skill update`.\n"

const configMetadataRef = "---\n" +
	"\n## sshq metadata format\n\n" +
	"sshq stores extended metadata as `# sshq:key=value` comments directly above the `Host` line in `~/.ssh/config`. Standard SSH tools ignore these comments.\n\n" +
	"**Example host entry:**\n\n" +
	"```ssh-config\n" +
	"# sshq:description=Production web server\n" +
	"# sshq:tags=prod,web,nginx\n" +
	"# sshq:environment=production\n" +
	"# sshq:created_at=2026-06-25 10:30:00\n" +
	"# sshq:updated_at=2026-06-25 10:30:00\n" +
	"Host prod-web\n" +
	"    HostName 10.0.1.100\n" +
	"    User root\n" +
	"    Port 22\n" +
	"    IdentityFile ~/.ssh/id_ed25519\n" +
	"```\n\n" +
	"**Metadata keys:**\n\n" +
	"| Key | Set via | Used by |\n" +
	"|-----|---------|--------|\n" +
	"| `description` | `sshq config set <alias> description \"...\"` | `sshq ls`, `sshq search` |\n" +
	"| `tags` | `sshq config set <alias> tags prod,web` | `sshq cluster exec --tag` |\n" +
	"| `environment` | `sshq config set <alias> env staging` | `sshq cluster exec --env` |\n" +
	"| `created_at` | Auto-set on `config add` | informational |\n" +
	"| `updated_at` | Auto-set on `config set` | informational |\n\n" +
	"**SSH properties managed through config set:**\n\n" +
	"```bash\n" +
	"sshq config set myhost hostname 10.0.0.1\n" +
	"sshq config set myhost user deploy\n" +
	"sshq config set myhost port 2222\n" +
	"sshq config set myhost identityfile ~/.ssh/deploy_key\n" +
	"sshq config set myhost proxyjump bastion\n" +
	"```\n\n" +
	"After `config add` or connection-related `config set` changes, run `sshq doctor myhost` to verify the fully resolved configuration and connection path.\n\n" +
	"**ProxyJump:** Configure in `~/.ssh/config` using standard `ProxyJump` directive. sshq resolves multi-hop chains automatically — just use the target alias.\n"
