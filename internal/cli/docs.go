package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func newDocsCommand() *cobra.Command {
	var skill bool
	cmd := &cobra.Command{
		Use:    "docs <output-dir>",
		Short:  "Generate command reference documentation",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("create dir: %w", err)
			}
			root := cmd.Root()
			root.DisableAutoGenTag = true
			if skill {
				return genSkillDocs(root, dir)
			}
			return doc.GenMarkdownTree(root, dir)
		},
	}
	cmd.Flags().BoolVar(&skill, "skill", false, "generate skill reference docs (grouped by scenario)")
	return cmd
}

var skillGroups = []struct {
	file  string
	title string
	intro string
	cmds  []string
}{
	{
		file:  "exec-transfer.md",
		title: "Execution & File Transfer",
		intro: "Commands for running remote commands and transferring files.",
		cmds:  []string{"exec", "cp"},
	},
	{
		file:  "config.md",
		title: "Configuration Management",
		intro: "Commands for managing SSH host configuration and sshq metadata.",
		cmds:  []string{"config", "config add", "config set", "config remove", "config list"},
	},
	{
		file:  "cluster-tunnel.md",
		title: "Cluster & Tunnel",
		intro: "Commands for concurrent multi-host operations and port forwarding.",
		cmds:  []string{"cluster", "cluster exec", "tunnel", "tunnel start", "tunnel stop", "tunnel list"},
	},
	{
		file:  "discovery.md",
		title: "Discovery & Daemon",
		intro: "Commands for listing, searching, inspecting hosts, credentials, and managing the daemon.",
		cmds:  []string{"ls", "search", "info", "probe", "trust", "credential", "credential set", "credential delete", "credential list", "daemon", "daemon start", "daemon stop", "daemon status", "version", "skill", "skill install", "skill export", "skill status"},
	},
}

func genSkillDocs(root *cobra.Command, dir string) error {
	index := buildCmdIndex(root, "")

	for _, g := range skillGroups {
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n", g.title)
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

		if g.file == "config.md" {
			b.WriteString(configMetadataRef)
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
	"**ProxyJump:** Configure in `~/.ssh/config` using standard `ProxyJump` directive. sshq resolves multi-hop chains automatically — just use the target alias.\n"
