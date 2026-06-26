package transfer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type rawEngine struct {
	client *ssh.Client
}

func newRawEngine(client *ssh.Client) *rawEngine {
	return &rawEngine{client: client}
}

func (e *rawEngine) Name() string { return "raw" }
func (e *rawEngine) Close() error { return nil }

func (e *rawEngine) Upload(ctx context.Context, localPath, remotePath string, progress ProgressFunc) (*Result, error) {
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

	if remotePath[len(remotePath)-1] == '/' {
		remotePath += filepath.Base(localPath)
	}
	tmpPath := remotePath + ".sshq.tmp"

	session, err := e.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	remoteDir := remotePath[:strings.LastIndex(remotePath, "/")]
	cmd := fmt.Sprintf("mkdir -p '%s' && cat > '%s'", shellEscape(remoteDir), shellEscape(tmpPath))
	if err := session.Start(cmd); err != nil {
		session.Close()
		return nil, fmt.Errorf("start remote write: %w", err)
	}

	tracker := NewProgressTracker(filepath.Base(remotePath), stat.Size(), progress)
	written, copyErr := copyWithProgress(ctx, stdin, local, tracker)
	stdin.Close()
	waitErr := session.Wait()

	if copyErr != nil || ctx.Err() != nil {
		e.remoteCleanup(tmpPath)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, copyErr
	}
	if waitErr != nil {
		e.remoteCleanup(tmpPath)
		return nil, fmt.Errorf("remote write: %w", waitErr)
	}

	if err := e.remoteRename(tmpPath, remotePath); err != nil {
		e.remoteCleanup(tmpPath)
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

func (e *rawEngine) Download(ctx context.Context, remotePath, localPath string, progress ProgressFunc) (*Result, error) {
	start := time.Now()

	size, err := e.remoteFileSize(remotePath)
	if err != nil {
		return nil, fmt.Errorf("remote file not found: %s", remotePath)
	}

	localStat, statErr := os.Stat(localPath)
	if statErr == nil && localStat.IsDir() {
		base := remotePath[strings.LastIndex(remotePath, "/")+1:]
		localPath = filepath.Join(localPath, base)
	}
	tmpPath := localPath + ".sshq.tmp"

	if dir := filepath.Dir(localPath); dir != "." {
		os.MkdirAll(dir, 0755)
	}

	session, err := e.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	cmd := fmt.Sprintf("cat '%s'", shellEscape(remotePath))
	if err := session.Start(cmd); err != nil {
		session.Close()
		return nil, fmt.Errorf("start remote read: %w", err)
	}

	local, err := os.Create(tmpPath)
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("create local file: %w", err)
	}

	tracker := NewProgressTracker(filepath.Base(localPath), size, progress)
	written, copyErr := copyWithProgress(ctx, local, stdout, tracker)
	local.Close()
	waitErr := session.Wait()

	if copyErr != nil || ctx.Err() != nil {
		os.Remove(tmpPath)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, copyErr
	}
	if waitErr != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("remote read: %w", waitErr)
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

func (e *rawEngine) UploadRecursive(ctx context.Context, localDir, remoteDir string, progress ProgressFunc) (*Result, error) {
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
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(localDir, localPath)
		if err != nil {
			return err
		}
		remotePath := remoteDir + "/" + filepath.ToSlash(rel)

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

func (e *rawEngine) DownloadRecursive(ctx context.Context, remoteDir, localDir string, progress ProgressFunc) (*Result, error) {
	files, err := e.remoteListFiles(remoteDir)
	if err != nil {
		return nil, fmt.Errorf("list remote directory: %w", err)
	}

	start := time.Now()
	var totalSize int64
	var totalFiles int

	for _, remotePath := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		rel := strings.TrimPrefix(remotePath, remoteDir+"/")
		localPath := filepath.Join(localDir, rel)

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

func (e *rawEngine) OpenRead(_ context.Context, remotePath string) (io.ReadCloser, int64, error) {
	size, _ := e.remoteFileSize(remotePath)

	session, err := e.client.NewSession()
	if err != nil {
		return nil, 0, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, 0, err
	}

	cmd := fmt.Sprintf("cat '%s'", shellEscape(remotePath))
	if err := session.Start(cmd); err != nil {
		session.Close()
		return nil, 0, err
	}

	return &sessionReader{session: session, reader: stdout}, size, nil
}

func (e *rawEngine) OpenWrite(_ context.Context, remotePath string) (io.WriteCloser, func() error, func(), error) {
	tmpPath := remotePath + ".sshq.tmp"
	remoteDir := remotePath[:strings.LastIndex(remotePath, "/")]

	session, err := e.client.NewSession()
	if err != nil {
		return nil, nil, nil, err
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, nil, nil, err
	}

	cmd := fmt.Sprintf("mkdir -p '%s' && cat > '%s'", shellEscape(remoteDir), shellEscape(tmpPath))
	if err := session.Start(cmd); err != nil {
		session.Close()
		return nil, nil, nil, err
	}

	writer := &sessionWriter{session: session, writer: stdin}
	commit := func() error {
		stdin.Close()
		if err := session.Wait(); err != nil {
			e.remoteCleanup(tmpPath)
			return err
		}
		return e.remoteRename(tmpPath, remotePath)
	}
	rollback := func() {
		stdin.Close()
		session.Wait()
		e.remoteCleanup(tmpPath)
	}

	return writer, commit, rollback, nil
}

func (e *rawEngine) remoteRename(src, dst string) error {
	session, err := e.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run(fmt.Sprintf("mv -f '%s' '%s'", shellEscape(src), shellEscape(dst)))
}

func (e *rawEngine) remoteCleanup(path string) {
	session, err := e.client.NewSession()
	if err != nil {
		return
	}
	defer session.Close()
	session.Run(fmt.Sprintf("rm -f '%s'", shellEscape(path)))
}

func (e *rawEngine) remoteFileSize(path string) (int64, error) {
	session, err := e.client.NewSession()
	if err != nil {
		return 0, err
	}
	defer session.Close()

	out, err := session.Output(fmt.Sprintf("stat -c %%s '%s' 2>/dev/null || wc -c < '%s'", shellEscape(path), shellEscape(path)))
	if err != nil {
		return 0, err
	}

	var size int64
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &size)
	return size, nil
}

func (e *rawEngine) remoteListFiles(dir string) ([]string, error) {
	session, err := e.client.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()

	out, err := session.Output(fmt.Sprintf("find '%s' -type f 2>/dev/null", shellEscape(dir)))
	if err != nil {
		return nil, err
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func shellEscape(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}

type sessionReader struct {
	session *ssh.Session
	reader  io.Reader
}

func (r *sessionReader) Read(p []byte) (int, error) { return r.reader.Read(p) }
func (r *sessionReader) Close() error {
	r.session.Close()
	return nil
}

type sessionWriter struct {
	session *ssh.Session
	writer  io.WriteCloser
}

func (w *sessionWriter) Write(p []byte) (int, error) { return w.writer.Write(p) }
func (w *sessionWriter) Close() error {
	w.writer.Close()
	return w.session.Wait()
}
