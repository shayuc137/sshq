package transfer

import (
	"fmt"
	"strings"
)

type Direction int

const (
	Upload Direction = iota
	Download
	Relay
)

func (d Direction) String() string {
	switch d {
	case Upload:
		return "upload"
	case Download:
		return "download"
	case Relay:
		return "relay"
	}
	return "unknown"
}

type Endpoint struct {
	Alias string
	Path  string
}

func (e Endpoint) IsLocal() bool  { return e.Alias == "" }
func (e Endpoint) IsRemote() bool { return e.Alias != "" }

func (e Endpoint) String() string {
	if e.IsLocal() {
		return e.Path
	}
	return e.Alias + ":" + e.Path
}

type ParsedArgs struct {
	Direction Direction
	Src       Endpoint
	Dst       Endpoint
}

func ParseArgs(src, dst string) (ParsedArgs, error) {
	srcEp := parseEndpoint(src)
	dstEp := parseEndpoint(dst)

	if srcEp.Path == "" || dstEp.Path == "" {
		return ParsedArgs{}, fmt.Errorf("path cannot be empty")
	}

	switch {
	case srcEp.IsLocal() && dstEp.IsRemote():
		return ParsedArgs{Upload, srcEp, dstEp}, nil
	case srcEp.IsRemote() && dstEp.IsLocal():
		return ParsedArgs{Download, srcEp, dstEp}, nil
	case srcEp.IsRemote() && dstEp.IsRemote():
		return ParsedArgs{Relay, srcEp, dstEp}, nil
	default:
		return ParsedArgs{}, fmt.Errorf("at least one argument must be alias:path")
	}
}

func parseEndpoint(s string) Endpoint {
	if len(s) >= 2 && s[1] == ':' && (len(s) == 2 || s[2] == '/' || s[2] == '\\') {
		return Endpoint{Path: s}
	}
	if i := strings.IndexByte(s, ':'); i > 0 {
		return Endpoint{Alias: s[:i], Path: s[i+1:]}
	}
	return Endpoint{Path: s}
}
