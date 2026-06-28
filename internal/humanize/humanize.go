// Package humanize formats quantities into human-readable strings.
package humanize

import "fmt"

// Bytes formats a byte count using binary units (KB/MB/GB), e.g. 1536 -> "1.5KB".
func Bytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
