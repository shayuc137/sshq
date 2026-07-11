package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/shayuc137/sshq/internal/humanize"
	"golang.org/x/term"
)

// Mode is the resolved output form. After construction it is only ModeJSON or ModePretty.
type Mode int

const (
	ModeAuto   Mode = iota // resolved from env var and TTY detection
	ModeJSON               // {data} or {error} envelope
	ModePretty             // human-readable form
)

// Renderable types provide a human-readable rendering, used by Writer.Render in non-JSON mode.
type Renderable interface {
	Pretty() string
}

// ExitCoder marks results that carry a remote command outcome. It lets the
// envelope writer expose that outcome without coupling commands to JSON layout.
type ExitCoder interface {
	RemoteExitCode() (int, bool)
}

// BadNewsError marks a completed command whose result is an unsuccessful
// outcome. The command has already rendered its data before returning it.
type BadNewsError struct{}

func (*BadNewsError) Error() string        { return "command completed with an unsuccessful result" }
func (*BadNewsError) ProcessExitCode() int { return 1 }

// BadNews declares that a command completed but its data represents a bad result.
func BadNews() error { return &BadNewsError{} }

// ProgressInfo is a transfer progress snapshot. It is defined here rather than
// reusing transfer.ProgressInfo to keep output free of a transfer dependency;
// callers convert between the two.
type ProgressInfo struct {
	File        string `json:"file"`
	Percent     int    `json:"percent"`
	Transferred int64  `json:"transferred"`
	Total       int64  `json:"total"`
	Speed       string `json:"speed"`
}

// ExecResult is a remote command result, used for structured JSON output.
type ExecResult struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Host       string `json:"host"`
	DurationMs int64  `json:"duration_ms"`
}

func (r ExecResult) RemoteExitCode() (int, bool) { return r.ExitCode, true }

// Writer is sshq's output façade: it absorbs format decisions, TTY detection and
// progress control, so callers only pass structured data.
type Writer struct {
	out      io.Writer
	err      io.Writer
	mode     Mode
	progress bool
	verbose  bool
}

// Option configures a Writer at construction time.
type Option func(*Writer)

// WithJSON forces JSON output, overriding TTY detection and the env var.
func WithJSON() Option { return func(w *Writer) { w.mode = ModeJSON } }

// WithPretty forces pretty output, overriding TTY detection and the env var.
func WithPretty() Option { return func(w *Writer) { w.mode = ModePretty } }

// WithNoProgress disables progress output.
func WithNoProgress() Option { return func(w *Writer) { w.progress = false } }

// WithVerbose enables verbose messages.
func WithVerbose() Option { return func(w *Writer) { w.verbose = true } }

// New constructs a Writer. Mode precedence: explicit flag > SSHQ_OUTPUT env var >
// TTY detection (terminal defaults to pretty, pipe defaults to JSON).
func New(out, err io.Writer, opts ...Option) *Writer {
	w := &Writer{
		out:      out,
		err:      err,
		mode:     ModeAuto,
		progress: true,
	}
	for _, opt := range opts {
		opt(w)
	}
	w.mode = w.resolveMode()
	return w
}

// isTerminal is a package var so tests can replace it. out must implement
// Fd() uintptr (os.File does).
var isTerminal = func(w io.Writer) bool {
	f, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func (w *Writer) resolveMode() Mode {
	if w.mode == ModeJSON || w.mode == ModePretty {
		return w.mode
	}
	if DetectEnvJSONMode() {
		return ModeJSON
	}
	if isTerminal(w.out) {
		return ModePretty
	}
	return ModeJSON
}

func (w *Writer) isJSON() bool { return w.mode == ModeJSON }

func (w *Writer) IsVerbose() bool { return w.verbose }

// Render writes structured data: a JSON envelope in JSON mode, otherwise
// Renderable.Pretty(); non-Renderable values fall back to fmt formatting.
func (w *Writer) Render(data any) {
	if w.isJSON() {
		w.writeEnvelope(data)
		return
	}
	if r, ok := data.(Renderable); ok {
		w.writeln(w.out, r.Pretty())
		return
	}
	w.writeln(w.out, fmt.Sprint(data))
}

// Exec writes a remote command result. JSON mode emits an envelope; pretty mode
// writes remote stdout verbatim to stdout and remote stderr to stderr, so local
// stdout is an exact mirror of remote stdout.
func (w *Writer) Exec(r *ExecResult) {
	if w.isJSON() {
		w.writeEnvelope(r)
		return
	}
	fmt.Fprint(w.out, r.Stdout)
	if r.Stderr != "" {
		fmt.Fprint(w.err, r.Stderr)
	}
}

// Progress writes transfer progress to stderr, gated by WithNoProgress. JSON mode
// writes a JSON line (for agent monitoring), pretty mode writes a readable line.
func (w *Writer) Progress(info ProgressInfo) {
	if !w.progress {
		return
	}
	if w.isJSON() {
		b, _ := json.Marshal(info)
		w.writeln(w.err, string(b))
		return
	}
	w.writeln(w.err, fmt.Sprintf("%s %d%% %s/%s %s",
		info.File, info.Percent,
		humanize.Bytes(info.Transferred),
		humanize.Bytes(info.Total),
		info.Speed))
}

// Info always writes to stderr; used for connection status, engine selection, etc.
func (w *Writer) Info(msg string) {
	w.writeln(w.err, msg)
}

// Verbose writes to stderr only in verbose mode.
func (w *Writer) Verbose(msg string) {
	if !w.verbose {
		return
	}
	w.writeln(w.err, msg)
}

// Success writes a simple success message: an envelope in JSON mode, stdout otherwise.
func (w *Writer) Success(msg string) {
	if w.isJSON() {
		w.writeEnvelope(map[string]any{"message": msg})
		return
	}
	if msg == "" {
		msg = "OK"
	}
	w.writeln(w.out, msg)
}

// Error writes an error: a JSON error envelope to stdout in JSON mode, stderr otherwise.
func (w *Writer) Error(e *CmdError) {
	if w.isJSON() {
		errorData := map[string]any{"code": e.Code, "hint": e.Hint, "action": e.Action}
		if len(e.Details) > 0 {
			errorData["details"] = e.Details
		}
		envelope := map[string]any{
			"error": errorData,
		}
		b, _ := json.Marshal(envelope)
		w.writeln(w.out, string(b))
		return
	}
	w.writeln(w.err, "Error: "+e.Hint)
	if e.Action != "" {
		w.writeln(w.err, "  -> "+e.Action)
	}
}

func (w *Writer) writeEnvelope(data any) {
	data = normalizeNilSlice(data)
	envelope := map[string]any{
		"data": data,
	}
	if result, ok := data.(ExitCoder); ok {
		if exitCode, present := result.RemoteExitCode(); present {
			envelope["exit_code"] = exitCode
		}
	}
	b, _ := json.Marshal(envelope)
	w.writeln(w.out, string(b))
}

func normalizeNilSlice(data any) any {
	if data == nil {
		return data
	}
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Slice && v.IsNil() {
		return reflect.MakeSlice(v.Type(), 0, 0).Interface()
	}
	return data
}

func (w *Writer) writeln(dest io.Writer, s string) {
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	fmt.Fprint(dest, s)
}

type CmdError struct {
	Hint    string
	Action  string
	Details map[string]any
	Code    string
}

func (e *CmdError) Error() string {
	if e.Action != "" {
		return fmt.Sprintf("%s (-> %s)", e.Hint, e.Action)
	}
	return e.Hint
}

func Errorf(hint, action string) *CmdError {
	return &CmdError{Hint: hint, Action: action, Code: "internal_error"}
}

func (e *CmdError) WithCode(code string) *CmdError {
	e.Code = code
	return e
}

func (e *CmdError) ProcessExitCode() int {
	return 2
}

func (e *CmdError) WithDetails(details map[string]any) *CmdError {
	e.Details = details
	return e
}

func DetectEnvJSONMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("SSHQ_OUTPUT")), "json")
}
