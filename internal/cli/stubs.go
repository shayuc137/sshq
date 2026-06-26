package cli

import (
	"fmt"

	"github.com/shayuc137/sshq/internal/output"
	"github.com/spf13/cobra"
)

func newStubCommand(name, short string, phase int) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return output.Errorf(
				fmt.Sprintf("%s is not yet implemented", name),
				fmt.Sprintf("planned for Phase %d", phase),
			)
		},
	}
}
