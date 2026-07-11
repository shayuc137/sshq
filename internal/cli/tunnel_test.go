package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/tunnel"
)

func TestTunnelListNilRendersEmptyJSONArray(t *testing.T) {
	var out bytes.Buffer
	output.New(&out, &bytes.Buffer{}, output.WithJSON()).Render(tunnelList(nil))
	var envelope struct {
		Data []tunnel.TunnelInfo `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if envelope.Data == nil || len(envelope.Data) != 0 {
		t.Fatalf("data = %#v, want []", envelope.Data)
	}
}
