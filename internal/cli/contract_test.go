package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/policy"
	"github.com/shayuc137/sshq/internal/pool"
	"github.com/shayuc137/sshq/internal/probe"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/shayuc137/sshq/internal/transfer"
)

var updateContractGoldens = flag.Bool("update", false, "update CLI contract golden files")

func TestContractGoldenOutputs(t *testing.T) {
	zero := 0
	tests := []struct {
		name       string
		definition string
		render     func(*output.Writer)
	}{
		{
			name:       "exec",
			definition: "execData",
			render: func(w *output.Writer) {
				w.Exec(&output.ExecResult{
					ExitCode:   0,
					Stdout:     "web-1\n",
					Stderr:     "",
					Host:       "web-1",
					DurationMs: 42,
				})
			},
		},
		{
			name:       "cluster-exec",
			definition: "clusterData",
			render: func(w *output.Writer) {
				w.Render(clusterResult{
					Results: []clusterHostResult{
						{Alias: "web-1", Stdout: "web-1", ExitCode: &zero},
						{Alias: "web-2", Error: "network error connecting to 192.0.2.20:22"},
					},
					Summary: ipc.ClusterSummary{Total: 2, Success: 1, Failed: 1},
				})
			},
		},
		{
			name:       "cp",
			definition: "cpData",
			render: func(w *output.Writer) {
				w.Render(&transfer.Result{
					Direction: "upload",
					Remote:    "web-1:/srv/app.tar",
					Size:      4096,
					Duration:  "125ms",
					Engine:    "sftp",
					Files:     1,
				})
			},
		},
		{
			name:       "probe",
			definition: "probeData",
			render: func(w *output.Writer) {
				w.Render(probeView{Result: probe.Result{
					Alias:            "web-1",
					Host:             "192.0.2.10",
					Port:             "22",
					ResolvedHostname: "192.0.2.10",
					ProxyJump:        "",
					ProbePath:        "direct",
					Reachable:        true,
					LatencyMs:        7,
				}})
			},
		},
		{
			name:       "ls",
			definition: "hostListData",
			render: func(w *output.Writer) {
				w.Render(config.HostList{
					{
						Alias:    "web-1",
						HostName: "192.0.2.10",
						User:     "deploy",
						Port:     "22",
						Metadata: map[string]string{"environment": "production", "tags": "web"},
					},
				})
			},
		},
	}

	schema := loadContractSchema(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			tt.render(output.New(&out, &bytes.Buffer{}, output.WithJSON()))
			got := formatContractJSON(t, out.Bytes())
			validateContractJSON(t, schema, got)
			validateContractDataDefinition(t, tt.definition, got)

			goldenPath := filepath.Join("testdata", "contract", tt.name+".golden.json")
			if *updateContractGoldens {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(goldenPath, got, 0644); err != nil {
					t.Fatal(err)
				}
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden file: %v; run go test ./internal/cli -run Contract -update", err)
			}
			want = formatContractJSON(t, want)
			validateContractJSON(t, schema, want)
			validateContractDataDefinition(t, tt.definition, want)
			if !bytes.Equal(got, want) {
				t.Fatalf("contract output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
			}
		})
	}
}

func TestContractEveryGoldenMatchesSchema(t *testing.T) {
	schema := loadContractSchema(t)
	paths, err := filepath.Glob(filepath.Join("testdata", "contract", "*.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 5 {
		t.Fatalf("golden file count = %d, want 5", len(paths))
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			validateContractJSON(t, schema, raw)
		})
	}
}

func TestContractFailureMatrix(t *testing.T) {
	tests := []struct {
		name string
		err  func(*testing.T) *output.CmdError
		code string
	}{
		{
			name: "network unreachable",
			err: func(*testing.T) *output.CmdError {
				return connErrorToOutput(contractConnError(sshclient.ErrNetwork), "web-1")
			},
			code: output.CodeNetworkError,
		},
		{
			name: "execution timeout",
			err: func(*testing.T) *output.CmdError {
				return connErrorToOutput(fmt.Errorf("execution cancelled: %w", context.DeadlineExceeded), "web-1")
			},
			code: output.CodeTimeout,
		},
		{
			name: "cp timeout",
			err: func(t *testing.T) *output.CmdError {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer cancel()
				parsed, err := transfer.ParseArgs("local.bin", "web-1:/tmp/local.bin")
				if err != nil {
					t.Fatal(err)
				}
				return cpErrorToOutput(ctx, context.DeadlineExceeded, parsed, false)
			},
			code: output.CodeTimeout,
		},
		{
			name: "host not found",
			err:  contractHostNotFoundError,
			code: output.CodeHostNotFound,
		},
		{
			name: "authentication failed",
			err: func(*testing.T) *output.CmdError {
				return connErrorToOutput(contractConnError(sshclient.ErrAuth), "web-1")
			},
			code: output.CodeAuthFailed,
		},
		{
			name: "host key unknown",
			err: func(*testing.T) *output.CmdError {
				return connErrorToOutput(contractConnError(sshclient.ErrHostKeyUnknown), "web-1")
			},
			code: output.CodeHostKeyUnknown,
		},
		{
			name: "host key mismatch",
			err: func(*testing.T) *output.CmdError {
				return connErrorToOutput(contractConnError(sshclient.ErrHostKeyMismatch), "web-1")
			},
			code: output.CodeHostKeyMismatch,
		},
		{
			name: "policy blocked",
			err: func(*testing.T) *output.CmdError {
				return (&policy.BlockedError{
					Alias:  "web-1",
					Kind:   policy.KindCommand,
					Reason: policy.ReasonWhitelistMiss,
					Input:  "rm -rf /tmp/work",
				}).ToOutputError()
			},
			code: output.CodePolicyBlocked,
		},
	}

	schema := loadContractSchema(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdErr := tt.err(t)
			if cmdErr.Code == output.CodeInternalError {
				t.Fatalf("expected failure returned %q", output.CodeInternalError)
			}
			if cmdErr.Code != tt.code {
				t.Fatalf("error.code = %q, want %q", cmdErr.Code, tt.code)
			}

			var out bytes.Buffer
			output.New(&out, &bytes.Buffer{}, output.WithJSON()).Error(cmdErr)
			validateContractJSON(t, schema, out.Bytes())

			var envelope struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error.Code != tt.code {
				t.Fatalf("rendered error.code = %q, want %q", envelope.Error.Code, tt.code)
			}
		})
	}
}

func TestContractIPCAndDirectErrorCodesMatch(t *testing.T) {
	injected := contractConnError(sshclient.ErrNetwork)
	directCode := connErrorToOutput(injected, "web-1").Code

	store := clusterStoreForTest(t, "Host web-1\n    HostName 192.0.2.10\n    User deploy\n")
	connPool := pool.New(time.Minute)
	connPool.DialFunc = func(context.Context, sshclient.ConnConfig) (*sshclient.Client, error) {
		return nil, injected
	}
	dc := &daemonContext{
		store:   store,
		checker: policy.NewChecker(nil, nil),
		pool:    connPool,
	}

	raw, err := json.Marshal(ipc.ExecPayload{Alias: "web-1", Command: "hostname", Timeout: 1})
	if err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		clientConn.Close()
		serverConn.Close()
	})
	go func() {
		dc.handleExec(serverConn, raw)
		_ = serverConn.Close()
	}()

	msg, err := ipc.Recv(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	var frame ipc.Frame
	if err := json.Unmarshal(msg, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != "error" {
		t.Fatalf("daemon frame type = %q, want error", frame.Type)
	}
	if daemonCode := frame.ErrorCode(); daemonCode != directCode {
		t.Fatalf("daemon error.code = %q, direct error.code = %q", daemonCode, directCode)
	}
	if directCode == output.CodeInternalError {
		t.Fatalf("IPC and direct paths both returned %q", output.CodeInternalError)
	}
}

func contractHostNotFoundError(t *testing.T) *output.CmdError {
	t.Helper()
	store := clusterStoreForTest(t, "")
	cmd := newExecCommand()
	cmd.SetContext(withConfig(context.Background(), store))
	cmd.SetArgs([]string{"missing", "hostname"})

	err := cmd.Execute()
	var cmdErr *output.CmdError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("host-not-found error = %T %v, want *output.CmdError", err, err)
	}
	return cmdErr
}

func contractConnError(kind sshclient.ConnErrorKind) *sshclient.ConnError {
	return &sshclient.ConnError{
		Kind:       kind,
		Alias:      "web-1",
		Host:       "192.0.2.10",
		Port:       "22",
		User:       "deploy",
		LookupKeys: []string{"web-1", "192.0.2.10"},
		Cause:      errors.New("injected contract failure"),
	}
}

func loadContractSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	path := filepath.Join(repositoryRoot(t), "schemas", "envelope-v3.schema.json")
	schema, err := jsonschema.NewCompiler().Compile(path)
	if err != nil {
		t.Fatalf("compile envelope schema: %v", err)
	}
	return schema
}

func validateContractJSON(t *testing.T, schema *jsonschema.Schema, raw []byte) {
	t.Helper()
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode contract JSON: %v\n%s", err, raw)
	}
	if err := schema.Validate(instance); err != nil {
		t.Fatalf("JSON does not match envelope schema: %v\n%s", err, raw)
	}
}

func validateContractDataDefinition(t *testing.T, definition string, raw []byte) {
	t.Helper()
	path := filepath.Join(repositoryRoot(t), "schemas", "envelope-v3.schema.json") + "#/$defs/" + definition
	schema, err := jsonschema.NewCompiler().Compile(path)
	if err != nil {
		t.Fatalf("compile %s schema definition: %v", definition, err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode contract JSON: %v\n%s", err, raw)
	}
	envelope, ok := instance.(map[string]any)
	if !ok {
		t.Fatalf("contract JSON is %T, want object", instance)
	}
	if err := schema.Validate(envelope["data"]); err != nil {
		t.Fatalf("data does not match %s: %v\n%s", definition, err, raw)
	}
}

func formatContractJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, raw)
	}
	formatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(formatted, '\n')
}
