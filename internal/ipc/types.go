package ipc

import "encoding/json"

const (
	ProtocolVersion   = 2
	ProtocolVersionV1 = 1
)

// Envelope is the v2 request format: action + version + typed payload.
type Envelope struct {
	Action  string          `json:"action"`
	Version int             `json:"v"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Frame is a streaming response from daemon to CLI.
type Frame struct {
	Type    string          `json:"type"`
	Data    string          `json:"data,omitempty"`
	Code    int             `json:"code,omitempty"`
	Hint    string          `json:"hint,omitempty"`
	Action  string          `json:"action,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Action payloads — one type per action, decoupled from each other.

type ExecPayload struct {
	Alias   string `json:"alias"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

type ScriptPayload struct {
	Alias   string `json:"alias"`
	Script  []byte `json:"script"`
	Shell   string `json:"shell,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

type TransferPayload struct {
	Direction  string `json:"direction"`
	Alias      string `json:"alias"`
	LocalPath  string `json:"local_path"`
	RemotePath string `json:"remote_path"`
	Recursive  bool   `json:"recursive,omitempty"`
}

type RelayPayload struct {
	SrcAlias  string `json:"src_alias"`
	SrcPath   string `json:"src_path"`
	DstAlias  string `json:"dst_alias"`
	DstPath   string `json:"dst_path"`
	Recursive bool   `json:"recursive,omitempty"`
}

type ProfilePayload struct {
	Alias   string `json:"alias"`
	Refresh bool   `json:"refresh,omitempty"`
}

// Result payloads — returned inside Frame{Type:"result"}.

type ProfileResult struct {
	OS       string `json:"os"`
	Shell    string `json:"shell"`
	Encoding string `json:"encoding,omitempty"`
	HomeDir  string `json:"home_dir,omitempty"`
}

type TransferResult struct {
	Direction string `json:"direction"`
	Remote    string `json:"remote"`
	Size      int64  `json:"size"`
	Duration  string `json:"duration"`
	Engine    string `json:"engine"`
	Files     int    `json:"files"`
}

// StatusResponse is returned by the "status" action.
type StatusResponse struct {
	Running     bool       `json:"running"`
	Uptime      int64      `json:"uptime_seconds"`
	Connections []ConnInfo `json:"connections"`
}

type ConnInfo struct {
	Key       string `json:"key"`
	Alias     string `json:"alias"`
	Host      string `json:"host"`
	Port      string `json:"port"`
	IdleSince int64  `json:"idle_since"`
}

// v1Request is used only for v1 detection — never construct new instances.
type v1Request struct {
	ProtocolVersion int `json:"protocol_version"`
}

// MakeEnvelope creates a v2 envelope with a typed payload.
func MakeEnvelope(action string, payload any) (Envelope, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return Envelope{}, err
		}
		raw = b
	}
	return Envelope{Action: action, Version: ProtocolVersion, Payload: raw}, nil
}

// MakeResultFrame wraps a result payload in a "result" frame.
func MakeResultFrame(result any) (Frame, error) {
	b, err := json.Marshal(result)
	if err != nil {
		return Frame{}, err
	}
	return Frame{Type: "result", Payload: json.RawMessage(b)}, nil
}
