package powershell

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestEncodedCommandUsesUTF16LEAndExplicitInterpreter(t *testing.T) {
	command := EncodedCommand([]byte(`Write-Output "OS=Windows"`))
	prefix := Prefix + " -EncodedCommand "
	if !strings.HasPrefix(command, prefix) {
		t.Fatalf("command = %q", command)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("UTF-16LE payload length = %d", len(raw))
	}
	words := make([]uint16, len(raw)/2)
	for i := range words {
		words[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	decoded := string(utf16.Decode(words))
	if !strings.Contains(decoded, `Write-Output "OS=Windows"`) {
		t.Fatalf("decoded payload = %q", decoded)
	}
}

func TestEncodePayloadOmitsInterpreterPrefix(t *testing.T) {
	payload := EncodePayload([]byte(`Copy-Item -LiteralPath 'source' -Destination 'target' -Force`))
	if strings.Contains(payload, "powershell") || strings.Contains(payload, " ") {
		t.Fatalf("payload = %q", payload)
	}
	if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
		t.Fatalf("payload is not Base64: %v", err)
	}
}
