package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
)

// clusterFrame builds the {type:"cluster"} envelope the daemon streams per host.
func clusterFrame(t *testing.T, cf ipc.ClusterFrame) ipc.Frame {
	t.Helper()
	b, err := json.Marshal(cf)
	if err != nil {
		t.Fatal(err)
	}
	return ipc.Frame{Type: "cluster", Payload: b}
}

func resultFrame(t *testing.T, s ipc.ClusterSummary) ipc.Frame {
	t.Helper()
	f, err := ipc.MakeResultFrame(s)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// runRecvCluster streams frames over an in-memory pipe into recvClusterFrames
// and returns the captured stdout/stderr plus its result.
func runRecvCluster(t *testing.T, jsonMode bool, frames ...ipc.Frame) (out, errOut *bytes.Buffer, err error) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	w := output.New(outBuf, errBuf)
	w.SetJSONMode(jsonMode)

	// Safety net so a malformed scenario fails fast instead of hanging.
	clientConn.SetDeadline(time.Now().Add(5 * time.Second))

	go func() {
		defer serverConn.Close()
		for _, f := range frames {
			if e := ipc.Send(serverConn, f); e != nil {
				return
			}
		}
	}()

	err = recvClusterFrames(w, clientConn)
	clientConn.Close()
	return outBuf, errBuf, err
}

type clusterEnvelope struct {
	OK   bool `json:"ok"`
	Data struct {
		Results []struct {
			Alias    string `json:"alias"`
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			ExitCode int    `json:"exit_code"`
			Error    string `json:"error"`
		} `json:"results"`
		Summary ipc.ClusterSummary `json:"summary"`
	} `json:"data"`
}

func TestRecvClusterFramesMultiHostJSON(t *testing.T) {
	out, _, err := runRecvCluster(t, true,
		clusterFrame(t, ipc.ClusterFrame{Alias: "web1", Type: "stdout", Data: "out1"}),
		clusterFrame(t, ipc.ClusterFrame{Alias: "web1", Type: "exit", Code: 0}),
		clusterFrame(t, ipc.ClusterFrame{Alias: "web2", Type: "stdout", Data: "out2"}),
		clusterFrame(t, ipc.ClusterFrame{Alias: "web2", Type: "exit", Code: 0}),
		resultFrame(t, ipc.ClusterSummary{Total: 2, Success: 2, Failed: 0}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env clusterEnvelope
	if e := json.Unmarshal(out.Bytes(), &env); e != nil {
		t.Fatalf("invalid JSON output: %v\n%s", e, out.String())
	}
	if !env.OK {
		t.Error("envelope ok = false")
	}
	if len(env.Data.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(env.Data.Results))
	}
	if env.Data.Results[0].Alias != "web1" || env.Data.Results[0].Stdout != "out1" {
		t.Errorf("result[0] = %+v", env.Data.Results[0])
	}
	if env.Data.Results[1].Alias != "web2" || env.Data.Results[1].Stdout != "out2" {
		t.Errorf("result[1] = %+v", env.Data.Results[1])
	}
	if env.Data.Summary.Success != 2 || env.Data.Summary.Total != 2 {
		t.Errorf("summary = %+v", env.Data.Summary)
	}
}

func TestRecvClusterFramesPartialFailureJSON(t *testing.T) {
	out, _, err := runRecvCluster(t, true,
		clusterFrame(t, ipc.ClusterFrame{Alias: "web1", Type: "stdout", Data: "ok"}),
		clusterFrame(t, ipc.ClusterFrame{Alias: "web1", Type: "exit", Code: 0}),
		clusterFrame(t, ipc.ClusterFrame{Alias: "web2", Type: "error", Hint: "connect refused"}),
		resultFrame(t, ipc.ClusterSummary{Total: 2, Success: 1, Failed: 1}),
	)

	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.Code != 1 {
		t.Fatalf("expected *exec.ExitError{Code:1} on partial failure, got %v", err)
	}

	var env clusterEnvelope
	if e := json.Unmarshal(out.Bytes(), &env); e != nil {
		t.Fatalf("invalid JSON output: %v", e)
	}
	if len(env.Data.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(env.Data.Results))
	}
	var web2 *struct {
		Alias    string `json:"alias"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
		Error    string `json:"error"`
	}
	for i := range env.Data.Results {
		if env.Data.Results[i].Alias == "web2" {
			web2 = &env.Data.Results[i]
		}
	}
	if web2 == nil || web2.Error != "connect refused" {
		t.Errorf("expected web2 to carry the error, got %+v", web2)
	}
}

func TestRecvClusterFramesNonZeroExitText(t *testing.T) {
	out, errOut, err := runRecvCluster(t, false,
		clusterFrame(t, ipc.ClusterFrame{Alias: "web1", Type: "stdout", Data: "hello"}),
		clusterFrame(t, ipc.ClusterFrame{Alias: "web1", Type: "exit", Code: 3}),
		resultFrame(t, ipc.ClusterSummary{Total: 1, Success: 0, Failed: 1}),
	)

	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.Code != 1 {
		t.Fatalf("expected *exec.ExitError{Code:1} on non-zero exit, got %v", err)
	}
	if !strings.Contains(out.String(), "[web1] hello") {
		t.Errorf("stdout missing host output: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "[web1] exit=3") {
		t.Errorf("stderr missing exit notice: %q", errOut.String())
	}
}

func TestRecvClusterFramesEmptyResults(t *testing.T) {
	out, _, err := runRecvCluster(t, false,
		resultFrame(t, ipc.ClusterSummary{Total: 0, Success: 0, Failed: 0}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "total=0 success=0 failed=0") {
		t.Errorf("expected summary line, got %q", out.String())
	}
}

func TestRecvClusterFramesErrorFrame(t *testing.T) {
	_, _, err := runRecvCluster(t, false,
		ipc.Frame{Type: "error", Hint: "daemon exploded", Action: "restart daemon"},
	)
	var ce *output.CmdError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *output.CmdError, got %v", err)
	}
	if ce.Hint != "daemon exploded" || ce.Action != "restart daemon" {
		t.Errorf("error frame not propagated: %+v", ce)
	}
}

func TestRecvClusterFramesMultiHostText(t *testing.T) {
	out, _, err := runRecvCluster(t, false,
		clusterFrame(t, ipc.ClusterFrame{Alias: "web1", Type: "stdout", Data: "out1"}),
		clusterFrame(t, ipc.ClusterFrame{Alias: "web1", Type: "exit", Code: 0}),
		clusterFrame(t, ipc.ClusterFrame{Alias: "web2", Type: "stdout", Data: "out2"}),
		clusterFrame(t, ipc.ClusterFrame{Alias: "web2", Type: "exit", Code: 0}),
		resultFrame(t, ipc.ClusterSummary{Total: 2, Success: 2, Failed: 0}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "[web1] out1") || !strings.Contains(s, "[web2] out2") {
		t.Errorf("stdout missing host lines: %q", s)
	}
	if !strings.Contains(s, "total=2 success=2 failed=0") {
		t.Errorf("stdout missing summary line: %q", s)
	}
}
