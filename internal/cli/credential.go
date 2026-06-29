package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/shayuc137/sshq/internal/credential"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newCredentialCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credential",
		Short: "Manage encrypted password credentials",
	}
	cmd.AddCommand(
		newCredentialSetCommand(),
		newCredentialDeleteCommand(),
		newCredentialListCommand(),
	)
	return cmd
}

func newCredentialSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <alias>",
		Short: "Store an encrypted password credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			password, err := readConfirmedSecret(cmd, "Password: ", "Confirm password: ")
			if err != nil {
				return output.Errorf(err.Error(), "run in an interactive terminal")
			}
			if password == "" {
				return output.Errorf("password cannot be empty", "")
			}

			store, err := openCredentialStoreForCommand(cmd)
			if err != nil {
				return output.Errorf("open credential store: "+err.Error(), "")
			}
			if err := store.Set(alias, password); err != nil {
				return credentialOutputError(err, alias)
			}

			writerFrom(cmd.Context()).Success("stored credential for " + alias)
			return nil
		},
	}
}

func newCredentialDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <alias>",
		Aliases: []string{"rm"},
		Short:   "Delete an encrypted password credential",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			store, err := openCredentialStoreForCommand(cmd)
			if err != nil {
				return output.Errorf("open credential store: "+err.Error(), "")
			}
			if err := store.Delete(alias); err != nil {
				return credentialOutputError(err, alias)
			}

			writerFrom(cmd.Context()).Success("deleted credential for " + alias)
			return nil
		},
	}
}

func newCredentialListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List aliases with stored password credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openCredentialStoreForCommand(cmd)
			if err != nil {
				return output.Errorf("open credential store: "+err.Error(), "")
			}
			aliases, err := store.List()
			if err != nil {
				return credentialOutputError(err, "")
			}

			writerFrom(cmd.Context()).Render(credentialAliases(aliases))
			return nil
		},
	}
}

func openCredentialStoreForCommand(cmd *cobra.Command) (*credential.Store, error) {
	return credential.Open(credential.WithPassphrase(passphrasePromptForStore(cmd)))
}

func passphrasePromptForStore(cmd *cobra.Command) func() (string, error) {
	var once bool
	var passphrase string
	var err error

	return func() (string, error) {
		if once {
			return passphrase, err
		}
		once = true

		path, pathErr := credential.DefaultPath()
		create := false
		if pathErr == nil {
			_, statErr := os.Stat(path)
			create = errors.Is(statErr, os.ErrNotExist)
		}

		if create {
			passphrase, err = readConfirmedSecret(cmd, "Credential store passphrase: ", "Confirm credential store passphrase: ")
		} else {
			passphrase, err = readSecret(cmd, "Credential store passphrase: ")
		}
		return passphrase, err
	}
}

func readConfirmedSecret(cmd *cobra.Command, prompt, confirmPrompt string) (string, error) {
	first, err := readSecret(cmd, prompt)
	if err != nil {
		return "", err
	}
	second, err := readSecret(cmd, confirmPrompt)
	if err != nil {
		return "", err
	}
	if first != second {
		return "", fmt.Errorf("passwords do not match")
	}
	return first, nil
}

func readSecret(cmd *cobra.Command, prompt string) (string, error) {
	in, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(in.Fd())) {
		return "", fmt.Errorf("credential input requires a TTY")
	}

	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	raw, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", fmt.Errorf("read credential input: %w", err)
	}
	return string(raw), nil
}

type credentialAliases []string

func (aliases credentialAliases) Pretty() string {
	if len(aliases) == 0 {
		return "no credentials stored"
	}
	return strings.Join(aliases, "\n")
}

func credentialOutputError(err error, alias string) *output.CmdError {
	switch {
	case errors.Is(err, credential.ErrNoEncryptionKey):
		return output.Errorf("no SSH key found for encryption", "generate one with: ssh-keygen -t ed25519 or run credential commands in a TTY for passphrase mode")
	case errors.Is(err, credential.ErrCannotDecrypt):
		action := "ensure your SSH key has not changed; re-create credentials if needed"
		if alias != "" {
			action = "ensure your SSH key has not changed; re-create with: sshq credential set " + alias
		}
		return output.Errorf("cannot decrypt credentials", action)
	case errors.Is(err, credential.ErrCorrupt):
		action := "re-create credentials with: sshq credential set <alias>"
		if alias != "" {
			action = "re-create with: sshq credential set " + alias
		}
		return output.Errorf("credential file corrupt", action)
	default:
		if strings.Contains(err.Error(), "insecure permissions") {
			action := "fix with: chmod 600 <credentials.age path>"
			if p, pathErr := credential.DefaultPath(); pathErr == nil {
				action = "fix with: chmod 600 " + p
			}
			return output.Errorf(err.Error(), action)
		}
		return output.Errorf(err.Error(), "")
	}
}
