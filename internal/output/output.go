package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const SchemaVersion = 1

type Writer struct {
	out      io.Writer
	err      io.Writer
	jsonMode bool
}

func New(out, err io.Writer) *Writer {
	return &Writer{out: out, err: err}
}

func (w *Writer) SetJSONMode(enabled bool) { w.jsonMode = enabled }
func (w *Writer) IsJSONMode() bool         { return w.jsonMode }

func (w *Writer) Success(msg string) {
	if w.jsonMode {
		w.writeEnvelope(map[string]any{"message": msg})
		return
	}
	if msg == "" {
		msg = "OK"
	}
	w.writeln(w.out, msg)
}

func (w *Writer) Value(v string) {
	if w.jsonMode {
		w.writeEnvelope(map[string]any{"value": v})
		return
	}
	w.writeln(w.out, v)
}

func (w *Writer) Info(msg string) {
	w.writeln(w.err, msg)
}

func (w *Writer) JSONOut(data any) {
	w.writeEnvelope(data)
}

func (w *Writer) RenderError(e *CmdError) {
	if w.jsonMode {
		envelope := map[string]any{
			"ok":             false,
			"error":          map[string]string{"hint": e.Hint, "action": e.Action},
			"schema_version": SchemaVersion,
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
	envelope := map[string]any{
		"ok":             true,
		"data":           data,
		"schema_version": SchemaVersion,
	}
	b, _ := json.Marshal(envelope)
	w.writeln(w.out, string(b))
}

func (w *Writer) writeln(dest io.Writer, s string) {
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	fmt.Fprint(dest, s)
}

type CmdError struct {
	Hint   string
	Action string
}

func (e *CmdError) Error() string {
	if e.Action != "" {
		return fmt.Sprintf("%s (-> %s)", e.Hint, e.Action)
	}
	return e.Hint
}

func Errorf(hint, action string) *CmdError {
	return &CmdError{Hint: hint, Action: action}
}

func DetectEnvJSONMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("SSHQ_OUTPUT")), "json")
}
