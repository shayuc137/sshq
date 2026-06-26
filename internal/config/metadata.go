package config

import (
	"bufio"
	"bytes"
	"strings"
)

func parseMetadataBlocks(raw []byte) map[string]map[string]string {
	result := make(map[string]map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(raw))

	var pendingMeta []commentLine
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "#") {
			pendingMeta = append(pendingMeta, parseComment(trimmed))
			continue
		}

		if strings.HasPrefix(trimmed, "Host ") {
			aliases := strings.Fields(trimmed[5:])
			if len(aliases) == 1 && !strings.ContainsAny(aliases[0], "*?") {
				meta := extractMeta(pendingMeta)
				if len(meta) > 0 {
					result[aliases[0]] = meta
				}
			}
			pendingMeta = nil
			continue
		}

		if trimmed == "" {
			pendingMeta = nil
		}
	}
	return result
}

type commentLine struct {
	key   string
	value string
	sshq  bool
}

func parseComment(line string) commentLine {
	content := strings.TrimPrefix(line, "#")
	content = strings.TrimSpace(content)

	if strings.HasPrefix(content, "sshq:") {
		kv := strings.TrimPrefix(content, "sshq:")
		kv = strings.TrimSpace(kv)
		if idx := strings.IndexByte(kv, '='); idx > 0 {
			return commentLine{
				key:   strings.TrimSpace(kv[:idx]),
				value: strings.TrimSpace(kv[idx+1:]),
				sshq:  true,
			}
		}
	}

	if idx := strings.IndexByte(content, ':'); idx > 0 {
		key := strings.TrimSpace(content[:idx])
		if !strings.Contains(key, " ") && key != "=====" {
			return commentLine{
				key:   key,
				value: strings.TrimSpace(content[idx+1:]),
			}
		}
	}

	return commentLine{}
}

func extractMeta(comments []commentLine) map[string]string {
	meta := make(map[string]string)
	for _, c := range comments {
		if c.key == "" {
			continue
		}
		if c.key == "password" {
			continue
		}
		if c.sshq {
			meta[c.key] = c.value
		} else if _, exists := meta[c.key]; !exists {
			meta[c.key] = c.value
		}
	}
	return meta
}
