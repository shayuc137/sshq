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
				return output.Errorf(err.Error(), "run in an interactive terminal").WithCode(output.CodeCredentialError)
			}
			if password == "" {
				return output.Errorf("password cannot be empty", "").WithCode(output.CodeCredentialError)
			}

			store, err := openCredentialStoreForCommand(cmd)
			if err != nil {
				return output.Errorf("open credential store: "+err.Error(), "").WithCode(output.CodeCredentialError)
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
				return output.Errorf("open credential store: "+err.Error(), "").WithCode(output.CodeCredentialError)
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
				return output.Errorf("open credential store: "+err.Error(), "").WithCode(output.CodeCredentialError)
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

// EnvCredentialPassphrase supplies the passphrase for a passphrase-mode
// credential store in non-interactive runtime contexts (daemon background loop,
// agent pipe). Direct commands fall back to a TTY prompt when it is unset.
const EnvCredentialPassphrase = "SSHQ_CREDENTIAL_PASSPHRASE"

// runtimePassphraseProvider builds the passphrase callback used when opening the
// credential store for actual exec/cp/tunnel/daemon use (as opposed to the
// `credential` management commands). The callback is lazy: the credential store
// only invokes it when it must decrypt a passphrase-mode file, so key-mode
// stores never trigger a prompt.
//
// Resolution order:
//  1. SSHQ_CREDENTIAL_PASSPHRASE environment variable (works headless), then
//  2. a TTY prompt on the command's stdin (matches `credential set` UX).
//
// When neither is available it returns a clear error instead of silently
// skipping the password, so passphrase-mode credentials no longer fail as
// generic auth errors at dial time.
func runtimePassphraseProvider(cmd *cobra.Command) func() (string, error) {
	var once bool
	var passphrase string
	var err error

	return func() (string, error) {
		if once {
			return passphrase, err
		}
		once = true

		if env, ok := os.LookupEnv(EnvCredentialPassphrase); ok {
			passphrase = env
			return passphrase, nil
		}
		if cmd != nil && commandStdinIsTTY(cmd) {
			passphrase, err = readSecret(cmd, "Credential store passphrase: ")
			return passphrase, err
		}
		err = fmt.Errorf("credential store passphrase required: set %s or run in a TTY", EnvCredentialPassphrase)
		return "", err
	}
}

// daemonPassphraseProvider resolves the credential passphrase for the daemon's
// background accept loop, which has no controlling TTY. It reads only from the
// environment; the daemon pre-warms decryption at startup (while a TTY may still
// be attached) via runtimePassphraseProvider, so by the time this is consulted
// the value is expected to come from SSHQ_CREDENTIAL_PASSPHRASE.
func daemonPassphraseProvider() func() (string, error) {
	return func() (string, error) {
		if env, ok := os.LookupEnv(EnvCredentialPassphrase); ok {
			return env, nil
		}
		return "", fmt.Errorf("credential store passphrase required: set %s before starting the daemon", EnvCredentialPassphrase)
	}
}

func commandStdinIsTTY(cmd *cobra.Command) bool {
	in, ok := cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(int(in.Fd()))
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
		return output.Errorf("no SSH key found for encryption", "generate one with: ssh-keygen -t ed25519 or run credential commands in a TTY for passphrase mode").WithCode(output.CodeCredentialError)
	case errors.Is(err, credential.ErrCannotDecrypt):
		action := "ensure your SSH key has not changed; re-create credentials if needed"
		if alias != "" {
			action = "ensure your SSH key has not changed; re-create with: sshq credential set " + alias
		}
		return output.Errorf("cannot decrypt credentials", action).WithCode(output.CodeCredentialError)
	case errors.Is(err, credential.ErrCorrupt):
		action := "re-create credentials with: sshq credential set <alias>"
		if alias != "" {
			action = "re-create with: sshq credential set " + alias
		}
		return output.Errorf("credential file corrupt", action).WithCode(output.CodeCredentialError)
	default:
		if strings.Contains(err.Error(), "insecure permissions") {
			action := "fix with: chmod 600 <credentials.age path>"
			if p, pathErr := credential.DefaultPath(); pathErr == nil {
				action = "fix with: chmod 600 " + p
			}
			return output.Errorf(err.Error(), action).WithCode(output.CodeCredentialError)
		}
		return output.Errorf(err.Error(), "").WithCode(output.CodeCredentialError)
	}
}
