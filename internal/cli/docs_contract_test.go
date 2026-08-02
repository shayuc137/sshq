package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	sshqCommandLineRE = regexp.MustCompile(`^\s*sshq\s+`)
	commentSuffixRE   = regexp.MustCompile(`\s+#.*$`)
	placeholderRE     = regexp.MustCompile(`<[^>\s]+>`)
	shellTokenRE      = regexp.MustCompile(`"(?:\\.|[^"\\])*"|'[^']*'|[^\s]+`)
	flagTokenRE       = regexp.MustCompile(`--?[a-z][a-z0-9-]*`)
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

func TestEnvelopeSchemaErrorCodesMatchOutputCodes(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "schemas", "envelope-v3.schema.json")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var schema struct {
		Defs struct {
			Error struct {
				Properties struct {
					Code struct {
						Enum []string `json:"enum"`
					} `json:"code"`
				} `json:"properties"`
			} `json:"error"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(content, &schema); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	assertStringSetEqual(t, "schema error.code enum", schema.Defs.Error.Properties.Code.Enum, output.AllCodes())
}

func TestSkillErrorCodesMatchOutputCodes(t *testing.T) {
	content := readSkillMarkdown(t)
	section := markdownSection(t, content, "### Error code quick reference")

	var codes []string
	inTable := false
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "|") {
			if inTable {
				break
			}
			continue
		}
		inTable = true
		columns := strings.Split(line, "|")
		if len(columns) < 3 {
			continue
		}
		code := strings.Trim(strings.TrimSpace(columns[1]), "`")
		if code == "code" || strings.Trim(code, "-") == "" {
			continue
		}
		codes = append(codes, code)
	}
	assertStringSetEqual(t, "SKILL.md error code table", codes, output.AllCodes())
}

func TestSkillExecAndCpFlagsMatchCobraCommands(t *testing.T) {
	content := readSkillMarkdown(t)
	root := NewRootCommand()

	for _, name := range []string{"exec", "cp"} {
		t.Run(name, func(t *testing.T) {
			cmd, _, err := root.Find([]string{name})
			if err != nil {
				t.Fatal(err)
			}
			documented := documentedFlags(t, markdownSection(t, content, "## "+name))

			for token := range documented {
				if !commandHasFlag(cmd, token) {
					t.Errorf("SKILL.md documents unknown %s flag %s", name, token)
				}
			}

			cmd.NonInheritedFlags().VisitAll(func(flag *pflag.Flag) {
				if flag.Name != "help" && !documented["--"+flag.Name] && (flag.Shorthand == "" || !documented["-"+flag.Shorthand]) {
					t.Errorf("SKILL.md %s Flags line is missing --%s", name, flag.Name)
				}
			})
		})
	}
}

func TestDocsIncludeRootPersistentFlags(t *testing.T) {
	root := NewRootCommand()
	dir := t.TempDir()
	if err := genSkillDocs(root, dir); err != nil {
		t.Fatalf("generate skill docs: %v", err)
	}

	var generated strings.Builder
	for _, name := range listMDFiles(dir) {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		generated.Write(content)
	}
	if generated.Len() == 0 {
		t.Fatal("skill docs generator produced no Markdown")
	}

	root.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
		if !strings.Contains(generated.String(), "--"+flag.Name) {
			t.Errorf("generated skill docs are missing root persistent flag --%s", flag.Name)
		}
	})
	if !strings.Contains(generated.String(), "--timeout duration   operation timeout (default 30s)") {
		t.Error("generated skill docs are missing the --timeout default of 30s")
	}
}

func readSkillMarkdown(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repositoryRoot(t), "skills", "sshq", "SKILL.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func markdownSection(t *testing.T, content, heading string) string {
	t.Helper()
	start := strings.Index(content, heading+"\n")
	if start < 0 {
		t.Fatalf("missing Markdown heading %q", heading)
	}
	section := content[start+len(heading)+1:]
	level := strings.Fields(heading)[0]
	if end := strings.Index(section, "\n"+level+" "); end >= 0 {
		section = section[:end]
	}
	return section
}

func documentedFlags(t *testing.T, section string) map[string]bool {
	t.Helper()
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "Flags:") {
			flags := make(map[string]bool)
			for _, token := range flagTokenRE.FindAllString(line, -1) {
				flags[token] = true
			}
			return flags
		}
	}
	t.Fatal("section has no Flags: line")
	return nil
}

func commandHasFlag(cmd *cobra.Command, token string) bool {
	if strings.HasPrefix(token, "--") {
		return cmd.Flag(strings.TrimPrefix(token, "--")) != nil
	}
	found := false
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		found = found || flag.Shorthand == strings.TrimPrefix(token, "-")
	})
	return found
}

func assertStringSetEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	gotSet := make(map[string]bool, len(got))
	wantSet := make(map[string]bool, len(want))
	for _, item := range got {
		gotSet[item] = true
	}
	for _, item := range want {
		wantSet[item] = true
	}

	var missing, extra []string
	for item := range wantSet {
		if !gotSet[item] {
			missing = append(missing, item)
		}
	}
	for item := range gotSet {
		if !wantSet[item] {
			extra = append(extra, item)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("%s mismatch: missing=%v extra=%v", label, missing, extra)
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
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if inBash {
				inBash = false
			} else {
				fence := strings.TrimPrefix(strings.TrimPrefix(trimmed, "```"), "~~~")
				inBash = strings.TrimSpace(fence) == "bash"
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

// TestEveryTopLevelCommandIsGrouped is the reverse direction of the contract:
// a new top-level command must be assigned to a skillGroups scenario, or it
// silently vanishes from the generated skill references.
func TestEveryTopLevelCommandIsGrouped(t *testing.T) {
	grouped := make(map[string]bool)
	for _, g := range skillGroups {
		for _, c := range g.cmds {
			grouped[strings.Fields(c)[0]] = true
		}
	}

	// docs is the generator itself; help/completion are cobra builtins.
	exempt := map[string]bool{"docs": true, "help": true, "completion": true}

	for _, cmd := range NewRootCommand().Commands() {
		name := cmd.Name()
		if cmd.Hidden || exempt[name] {
			continue
		}
		if !grouped[name] {
			t.Errorf("top-level command %q is not in any skillGroups scenario (internal/cli/docs.go); assign it or the skill docs will silently omit it", name)
		}
	}
}
