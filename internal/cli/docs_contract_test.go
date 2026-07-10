package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/shayuc137/sshq/internal/version"
	"github.com/spf13/cobra"
)

var (
	sshqCommandLineRE = regexp.MustCompile(`^\s*sshq\s+`)
	commentSuffixRE   = regexp.MustCompile(`\s+#.*$`)
	placeholderRE     = regexp.MustCompile(`<[^>\s]+>`)
	shellTokenRE      = regexp.MustCompile(`"(?:\\.|[^"\\])*"|'[^']*'|[^\s]+`)
)

type documentedCommand struct {
	file string
	line int
	text string
}

func TestSkillDocumentedCommandsMatchCobraTree(t *testing.T) {
	repoRoot := repositoryRoot(t)
	paths := []string{filepath.Join(repoRoot, "skills", "sshq", "SKILL.md")}
	references, err := filepath.Glob(filepath.Join(repoRoot, "skills", "sshq", "references", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, references...)

	var commands []documentedCommand
	for _, path := range paths {
		assertDocumentationVersion(t, path)
		commands = append(commands, extractBashCommands(t, path)...)
	}
	if len(commands) == 0 {
		t.Fatal("no sshq commands found in bash code blocks")
	}

	for _, example := range commands {
		example := example
		t.Run(fmt.Sprintf("%s:%d", filepath.Base(example.file), example.line), func(t *testing.T) {
			if err := validateDocumentedCommand(NewRootCommand(), example.text); err != nil {
				t.Fatalf("%s:%d: %s\ncommand: %s", example.file, example.line, err, example.text)
			}
		})
	}
	t.Logf("extracted %d sshq commands from %d skill documents; validated %d", len(commands), len(paths), len(commands))
}

func TestRootShortcutAndExecExposeSameExecFlags(t *testing.T) {
	root := NewRootCommand()
	execCmd, _, err := root.Find([]string{"exec"})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"script-file", "shell", "no-daemon"} {
		rootFlag := root.Flags().Lookup(name)
		execFlag := execCmd.Flags().Lookup(name)
		if rootFlag == nil || execFlag == nil {
			t.Fatalf("flag --%s: root=%v exec=%v", name, rootFlag != nil, execFlag != nil)
		}
		if rootFlag.DefValue != execFlag.DefValue || rootFlag.Usage != execFlag.Usage {
			t.Fatalf("flag --%s differs between root and exec", name)
		}
	}

	for _, line := range []string{
		`sshq exec <alias> "<cmd>" --script-file <path> --shell bash --no-daemon`,
		`sshq <alias> "<cmd>" --script-file <path> --shell bash --no-daemon`,
	} {
		if err := validateDocumentedCommand(NewRootCommand(), line); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
	}
}

func extractBashCommands(t *testing.T, path string) []documentedCommand {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var commands []documentedCommand
	inBash := false
	scanner := bufio.NewScanner(f)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		trimmed := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(trimmed, "```") {
			if inBash {
				inBash = false
			} else {
				inBash = strings.TrimSpace(strings.TrimPrefix(trimmed, "```")) == "bash"
			}
			continue
		}
		if inBash && sshqCommandLineRE.MatchString(scanner.Text()) {
			commands = append(commands, documentedCommand{file: path, line: lineNo, text: strings.TrimSpace(scanner.Text())})
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return commands
}

func validateDocumentedCommand(root *cobra.Command, line string) error {
	rawTokens := splitDocumentedCommand(line)
	tokens := splitDocumentedCommand(placeholderRE.ReplaceAllString(line, "dummy"))
	if len(tokens) < 2 || tokens[0] != "sshq" {
		return fmt.Errorf("invalid sshq command line")
	}

	cmd, remaining, err := root.Find(tokens[1:])
	if err != nil {
		return err
	}
	if cmd == root {
		if len(rawTokens) < 2 || rawTokens[1] != "<alias>" {
			return fmt.Errorf("unknown subcommand %q", tokens[1])
		}
		execCmd, _, findErr := root.Find([]string{"exec"})
		if findErr != nil {
			return findErr
		}
		cmd.Args = execCmd.Args
	}
	if err := cmd.ParseFlags(remaining); err != nil {
		return err
	}
	if cmd.Args != nil {
		return cmd.Args(cmd, cmd.Flags().Args())
	}
	return nil
}

func splitDocumentedCommand(line string) []string {
	line = commentSuffixRE.ReplaceAllString(line, "")
	tokens := shellTokenRE.FindAllString(line, -1)
	for i, token := range tokens {
		if len(token) >= 2 && ((token[0] == '"' && token[len(token)-1] == '"') || (token[0] == '\'' && token[len(token)-1] == '\'')) {
			tokens[i] = token[1 : len(token)-1]
		}
	}
	return tokens
}

func assertDocumentationVersion(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	prefix := string(content)
	if i := strings.Index(prefix, "\n## "); i >= 0 {
		prefix = prefix[:i]
	}
	want := "Documentation version: `sshq v" + version.Number() + "`"
	if !strings.Contains(prefix, want) {
		t.Errorf("%s: top section missing %q", path, want)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
