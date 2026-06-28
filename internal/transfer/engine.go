package transfer

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/shayuc137/sshq/internal/humanize"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
)

type Result struct {
	Direction string `json:"direction"`
	Remote    string `json:"remote"`
	Size      int64  `json:"size"`
	Duration  string `json:"duration"`
	Engine    string `json:"engine"`
	Files     int    `json:"files"`
}

// Pretty renders a transfer result summary for output.Writer.Render in non-JSON mode.
func (r *Result) Pretty() string {
	if r.Files > 1 {
		return fmt.Sprintf("%s %d files %s %s %s",
			r.Remote, r.Files, humanize.Bytes(r.Size), r.Duration, r.Engine)
	}
	return fmt.Sprintf("%s %s %s %s",
		r.Remote, humanize.Bytes(r.Size), r.Duration, r.Engine)
}

type Engine interface {
	Upload(ctx context.Context, localPath, remotePath string, progress ProgressFunc) (*Result, error)
	Download(ctx context.Context, remotePath, localPath string, progress ProgressFunc) (*Result, error)
	UploadRecursive(ctx context.Context, localDir, remoteDir string, progress ProgressFunc) (*Result, error)
	DownloadRecursive(ctx context.Context, remoteDir, localDir string, progress ProgressFunc) (*Result, error)
	OpenRead(ctx context.Context, remotePath string) (io.ReadCloser, int64, error)
	OpenWrite(ctx context.Context, remotePath string) (io.WriteCloser, func() error, func(), error)
	Close() error
	Name() string
}

func NewEngine(client *sshclient.Client, profile *remote.Profile, info func(string)) (Engine, error) {
	eng, err := newSFTPEngine(client)
	if err == nil {
		return eng, nil
	}
	if profile != nil && profile.IsWindows() && profile.NeedsStdinInjection() {
		return nil, fmt.Errorf("SFTP required on Windows — raw stream fallback not available; enable sftp-server on the remote host")
	}
	if info != nil {
		info("sftp unavailable, using raw stream")
	}
	return newRawEngine(client), nil
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
