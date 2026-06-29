package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const SummaryLimit = 200

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|passphrase|token|secret|api[_-]?key)=\S+`),
	regexp.MustCompile(`(?i)(password|passwd|passphrase|token|secret|api[_-]?key)\s+\S+`),
}

func RedactSummary(s string) string {
	s = sanitizeSummary(s)
	for _, re := range sensitivePatterns {
		s = re.ReplaceAllStringFunc(s, func(match string) string {
			if i := strings.IndexAny(match, "= \t"); i >= 0 {
				return match[:i+1] + "<redacted>"
			}
			return "<redacted>"
		})
	}
	if utf8.RuneCountInString(s) <= SummaryLimit {
		return s
	}
	runes := []rune(s)
	return string(runes[:SummaryLimit])
}

func ExecSummary(command string) string {
	return RedactSummary(command)
}

func ScriptSummary(script []byte) string {
	sum := sha256.Sum256(script)
	first := firstLine(string(script))
	if first == "" {
		first = "<empty>"
	}
	return RedactSummary(fmt.Sprintf("script sha256=%s bytes=%d first_line=%s",
		hex.EncodeToString(sum[:]), len(script), first))
}

func TransferSummary(direction, localPath, remotePath string) string {
	return RedactSummary(fmt.Sprintf("%s local=%s remote=%s", direction, localPath, remotePath))
}

func RelaySummary(srcAlias, srcPath, dstAlias, dstPath string) string {
	return RedactSummary(fmt.Sprintf("relay %s:%s -> %s:%s", srcAlias, srcPath, dstAlias, dstPath))
}

func TunnelSummary(direction, localAddr, remoteAddr, action string) string {
	return RedactSummary(fmt.Sprintf("%s %s local=%s remote=%s", direction, action, localAddr, remoteAddr))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return RedactSummary(strings.TrimSuffix(s, "\r"))
}

func sanitizeSummary(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
