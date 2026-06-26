package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func newDocsCommand() *cobra.Command {
	return &cobra.Command{
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
			return doc.GenMarkdownTree(root, dir)
		},
	}
}
