package cli

import (
	"strings"
	"testing"

	"github.com/shayuc137/sshq/internal/transfer"
)

func TestTransferPayloadCarriesMkdirs(t *testing.T) {
	parsed, err := transfer.ParseArgs("./local file.txt", "win:C:/Work Files/out.txt")
	if err != nil {
		t.Fatal(err)
	}
	payload := transferPayload(parsed, false, true, false)
	if !payload.Mkdirs || payload.Direction != "upload" || payload.RemotePath != "C:/Work Files/out.txt" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestCpMkdirsActionIsExecutable(t *testing.T) {
	parsed, err := transfer.ParseArgs("./local file.txt", "win:C:/Work Files/out.txt")
	if err != nil {
		t.Fatal(err)
	}
	action := cpMkdirsAction(parsed, false)
	for _, want := range []string{"sshq cp --mkdirs", "'./local file.txt'", "'win:C:/Work Files/out.txt'"} {
		if !strings.Contains(action, want) {
			t.Fatalf("action %q missing %q", action, want)
		}
	}
}

func TestCpCommandRegistersMkdirs(t *testing.T) {
	flag := newCpCommand().Flags().Lookup("mkdirs")
	if flag == nil || flag.DefValue != "false" {
		t.Fatalf("mkdirs flag = %+v", flag)
	}
}
