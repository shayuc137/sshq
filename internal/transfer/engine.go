package transfer

import (
	"context"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/ssh"
)

type Result struct {
	Direction string `json:"direction"`
	Remote    string `json:"remote"`
	Size      int64  `json:"size"`
	Duration  string `json:"duration"`
	Engine    string `json:"engine"`
	Files     int    `json:"files"`
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

func NewEngine(client *ssh.Client, info func(string)) (Engine, error) {
	eng, err := newSFTPEngine(client)
	if err == nil {
		return eng, nil
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
