package exec

import (
	"encoding/xml"
	"strings"
)

const clixmlHeader = "#< CLIXML"

// DecodeCLIXMLStderr converts a PowerShell CLIXML stderr payload back to plain
// text. PowerShell 5.1 serializes its error/verbose/warning streams as CLIXML
// when run non-interactively over stdio; agents cannot read that XML. Error
// stream entries are decoded and joined; pure progress/noise payloads decode
// to an empty string. Non-CLIXML input is returned unchanged.
//
// Input must already be UTF-8: encoding/xml rejects other charsets, so callers
// apply remote codepage transcoding (remote.DecodeString) first.
func DecodeCLIXMLStderr(s string) string {
	rest, ok := strings.CutPrefix(s, clixmlHeader)
	if !ok {
		return s
	}

	var doc struct {
		Entries []struct {
			Stream string `xml:"S,attr"`
			Text   string `xml:",chardata"`
		} `xml:"S"`
	}
	if err := xml.Unmarshal([]byte(strings.TrimSpace(rest)), &doc); err != nil {
		// Unparseable CLIXML: return the original payload rather than hide it.
		return s
	}

	var out strings.Builder
	for _, e := range doc.Entries {
		if e.Stream != "Error" && e.Stream != "Warning" {
			continue
		}
		out.WriteString(decodePSEscapes(e.Text))
	}
	return out.String()
}

// decodePSEscapes reverses CLIXML's _xHHHH_ character escapes (e.g. _x000D_
// for CR, _x000A_ for LF).
func decodePSEscapes(s string) string {
	var out strings.Builder
	for {
		i := strings.Index(s, "_x")
		if i < 0 || len(s) < i+7 || s[i+6] != '_' {
			out.WriteString(s)
			return out.String()
		}
		r, ok := parseHex4(s[i+2 : i+6])
		if !ok {
			out.WriteString(s[:i+2])
			s = s[i+2:]
			continue
		}
		out.WriteString(s[:i])
		out.WriteRune(r)
		s = s[i+7:]
	}
}

func parseHex4(hex string) (rune, bool) {
	var r rune
	for _, c := range hex {
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r += c - '0'
		case c >= 'a' && c <= 'f':
			r += c - 'a' + 10
		case c >= 'A' && c <= 'F':
			r += c - 'A' + 10
		default:
			return 0, false
		}
	}
	return r, true
}
