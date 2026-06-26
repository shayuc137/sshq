package ipc

const ProtocolVersion = 1

type Request struct {
	Action          string `json:"action"`
	ProtocolVersion int    `json:"protocol_version"`
	Alias           string `json:"alias,omitempty"`
	Command         string `json:"command,omitempty"`
	Timeout         int    `json:"timeout,omitempty"`
}

type Frame struct {
	Type   string `json:"type"`
	Data   string `json:"data,omitempty"`
	Code   int    `json:"code,omitempty"`
	Hint   string `json:"hint,omitempty"`
	Action string `json:"action,omitempty"`
}

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
