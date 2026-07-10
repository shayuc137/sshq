package powershell

import (
	"encoding/base64"
	"unicode/utf16"
)

const Prefix = "powershell -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass"

// EncodedCommand is safe to submit through either cmd.exe or PowerShell as the
// Windows OpenSSH default shell; the payload bypasses both shells' quoting rules.
func EncodedCommand(script []byte) string {
	return Prefix + " -EncodedCommand " + EncodePayload(script)
}

// EncodePayload returns the UTF-16LE Base64 payload expected by PowerShell's
// -EncodedCommand flag.
func EncodePayload(script []byte) string {
	// A same-line prefix preserves user script line numbers in remote errors and
	// suppresses PowerShell 5.1 CLIXML progress noise.
	prefixed := append([]byte("$ProgressPreference='SilentlyContinue'; "), script...)
	runes := utf16.Encode([]rune(string(prefixed)))
	raw := make([]byte, len(runes)*2)
	for i, codeUnit := range runes {
		raw[i*2] = byte(codeUnit)
		raw[i*2+1] = byte(codeUnit >> 8)
	}
	return base64.StdEncoding.EncodeToString(raw)
}
