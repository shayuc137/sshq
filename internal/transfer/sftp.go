package transfer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const chunkSize = 32 * 1024

type sftpEngine struct {
	client *sftp.Client
}

func newSFTPEngine(sshClient *ssh.Client) (*sftpEngine, error) {
	c, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, err
	}
	return &sftpEngine{client: c}, nil
}

func (e *sftpEngine) Name() string { return "sftp" }

func (e *sftpEngine) Close() error {
	return e.client.Close()
}

func (e *sftpEngine) Upload(ctx context.Context, localPath, remotePath string, progress ProgressFunc) (*Result, error) {
	start := time.Now()

	local, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("open local file: %w", err)
	}
	defer local.Close()

	stat, err := local.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat local file: %w", err)
	}
	if stat.IsDir() {
		return nil, fmt.Errorf("%q is a directory, use --recursive", localPath)
	}

	remotePath = resolveRemoteDir(e, remotePath, filepath.Base(localPath))
	tmpPath := remotePath + ".sshq.tmp"

	remoteDir := path.Dir(remotePath)
	e.client.MkdirAll(remoteDir)

	remote, err := e.client.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("create remote file: %w", err)
	}

	tracker := NewProgressTracker(path.Base(remotePath), stat.Size(), progress)
	written, err := copyWithProgress(ctx, remote, local, tracker)
	remote.Close()

	if err != nil {
		e.client.Remove(tmpPath)
		return nil, err
	}

	if err := e.atomicRename(tmpPath, remotePath); err != nil {
		e.client.Remove(tmpPath)
		return nil, fmt.Errorf("rename temp file: %w", err)
	}

	tracker.Finish()
	return &Result{
		Direction: "upload",
		Remote:    remotePath,
		Size:      written,
		Duration:  formatDuration(time.Since(start)),
		Engine:    e.Name(),
		Files:     1,
	}, nil
}

func (e *sftpEngine) Download(ctx context.Context, remotePath, localPath string, progress ProgressFunc) (*Result, error) {
	start := time.Now()

	remote, err := e.client.Open(remotePath)
	if err != nil {
		return nil, fmt.Errorf("open remote file: %w", err)
	}
	defer remote.Close()

	stat, err := remote.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat remote file: %w", err)
	}
	if stat.IsDir() {
		return nil, fmt.Errorf("%q is a directory, use --recursive", remotePath)
	}

	localPath = resolveLocalDir(localPath, stat.Name())
	tmpPath := localPath + ".sshq.tmp"

	if dir := filepath.Dir(localPath); dir != "." {
		os.MkdirAll(dir, 0755)
	}

	local, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("create local file: %w", err)
	}

	tracker := NewProgressTracker(stat.Name(), stat.Size(), progress)
	written, err := copyWithProgress(ctx, local, remote, tracker)
	local.Close()

	if err != nil {
		os.Remove(tmpPath)
		return nil, err
	}

	if err := os.Rename(tmpPath, localPath); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("rename temp file: %w", err)
	}

	tracker.Finish()
	return &Result{
		Direction: "download",
		Remote:    remotePath,
		Size:      written,
		Duration:  formatDuration(time.Since(start)),
		Engine:    e.Name(),
		Files:     1,
	}, nil
}

func (e *sftpEngine) UploadRecursive(ctx context.Context, localDir, remoteDir string, progress ProgressFunc) (*Result, error) {
	start := time.Now()
	var totalSize int64
	var totalFiles int

	err := filepath.Walk(localDir, func(localPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		rel, err := filepath.Rel(localDir, localPath)
		if err != nil {
			return err
		}
		remotePath := path.Join(remoteDir, filepath.ToSlash(rel))

		if info.IsDir() {
			e.client.MkdirAll(remotePath)
			return nil
		}

		r, err := e.Upload(ctx, localPath, remotePath, progress)
		if err != nil {
			return fmt.Errorf("upload %s: %w", rel, err)
		}
		totalSize += r.Size
		totalFiles++
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &Result{
		Direction: "upload",
		Remote:    remoteDir,
		Size:      totalSize,
		Duration:  formatDuration(time.Since(start)),
		Engine:    e.Name(),
		Files:     totalFiles,
	}, nil
}

func (e *sftpEngine) DownloadRecursive(ctx context.Context, remoteDir, localDir string, progress ProgressFunc) (*Result, error) {
	start := time.Now()
	var totalSize int64
	var totalFiles int

	walker := e.client.Walk(remoteDir)
	for walker.Step() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err := walker.Err(); err != nil {
			return nil, err
		}

		remotePath := walker.Path()
		rel, err := filepath.Rel(remoteDir, remotePath)
		if err != nil {
			continue
		}
		localPath := filepath.Join(localDir, rel)

		if walker.Stat().IsDir() {
			os.MkdirAll(localPath, 0755)
			continue
		}

		r, err := e.Download(ctx, remotePath, localPath, progress)
		if err != nil {
			return nil, fmt.Errorf("download %s: %w", rel, err)
		}
		totalSize += r.Size
		totalFiles++
	}

	return &Result{
		Direction: "download",
		Remote:    remoteDir,
		Size:      totalSize,
		Duration:  formatDuration(time.Since(start)),
		Engine:    e.Name(),
		Files:     totalFiles,
	}, nil
}

func (e *sftpEngine) OpenRead(_ context.Context, remotePath string) (io.ReadCloser, int64, error) {
	f, err := e.client.Open(remotePath)
	if err != nil {
		return nil, 0, err
	}
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, stat.Size(), nil
}

func (e *sftpEngine) OpenWrite(_ context.Context, remotePath string) (io.WriteCloser, func() error, func(), error) {
	remoteDir := path.Dir(remotePath)
	e.client.MkdirAll(remoteDir)

	tmpPath := remotePath + ".sshq.tmp"
	f, err := e.client.Create(tmpPath)
	if err != nil {
		return nil, nil, nil, err
	}

	commit := func() error {
		return e.atomicRename(tmpPath, remotePath)
	}
	rollback := func() {
		e.client.Remove(tmpPath)
	}

	return f, commit, rollback, nil
}

func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, tracker *ProgressTracker) (int64, error) {
	buf := make([]byte, chunkSize)
	var written int64

	for {
		if ctx.Err() != nil {
			return written, ctx.Err()
		}

		n, err := src.Read(buf)
		if n > 0 {
			nw, ew := dst.Write(buf[:n])
			if nw > 0 {
				written += int64(nw)
				if tracker != nil {
					tracker.Update(nw)
				}
			}
			if ew != nil {
				return written, ew
			}
		}
		if err == io.EOF {
			return written, nil
		}
		if err != nil {
			return written, err
		}
	}
}

func (e *sftpEngine) atomicRename(src, dst string) error {
	if err := e.client.PosixRename(src, dst); err == nil {
		return nil
	}
	e.client.Remove(dst)
	return e.client.Rename(src, dst)
}

func resolveRemoteDir(e *sftpEngine, remotePath, basename string) string {
	stat, err := e.client.Stat(remotePath)
	if err == nil && stat.IsDir() {
		return path.Join(remotePath, basename)
	}
	return remotePath
}

func resolveLocalDir(localPath, basename string) string {
	stat, err := os.Stat(localPath)
	if err == nil && stat.IsDir() {
		return filepath.Join(localPath, basename)
	}
	return localPath
}
