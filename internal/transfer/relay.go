package transfer

import (
	"context"
	"fmt"

	"path"
	"time"

	"github.com/shayuc137/sshq/internal/remote"
	"golang.org/x/crypto/ssh"
)

func RunRelay(ctx context.Context, srcClient, dstClient *ssh.Client, srcPath, dstPath string, srcProfile, dstProfile *remote.Profile, info func(string), progress ProgressFunc) (*Result, error) {
	start := time.Now()

	srcEngine, err := NewEngine(srcClient, srcProfile, func(msg string) {
		if info != nil {
			info("source: " + msg)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("source engine: %w", err)
	}
	defer srcEngine.Close()

	dstEngine, err := NewEngine(dstClient, dstProfile, func(msg string) {
		if info != nil {
			info("destination: " + msg)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("destination engine: %w", err)
	}
	defer dstEngine.Close()

	reader, size, err := srcEngine.OpenRead(ctx, srcPath)
	if err != nil {
		return nil, fmt.Errorf("open source: %w", err)
	}
	defer reader.Close()

	if dstPath[len(dstPath)-1] == '/' {
		dstPath += path.Base(srcPath)
	}

	writer, commit, rollback, err := dstEngine.OpenWrite(ctx, dstPath)
	if err != nil {
		return nil, fmt.Errorf("open destination: %w", err)
	}

	tracker := NewProgressTracker(path.Base(srcPath), size, progress)
	written, copyErr := copyWithProgress(ctx, writer, reader, tracker)
	writer.Close()

	if copyErr != nil || ctx.Err() != nil {
		rollback()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, copyErr
	}

	if err := commit(); err != nil {
		rollback()
		return nil, fmt.Errorf("commit destination: %w", err)
	}

	tracker.Finish()
	engineDesc := srcEngine.Name() + "→" + dstEngine.Name()
	return &Result{
		Direction: "relay",
		Remote:    dstPath,
		Size:      written,
		Duration:  formatDuration(time.Since(start)),
		Engine:    engineDesc,
		Files:     1,
	}, nil
}

func RunRelayRecursive(ctx context.Context, srcClient, dstClient *ssh.Client, srcDir, dstDir string, srcProfile, dstProfile *remote.Profile, info func(string), progress ProgressFunc) (*Result, error) {
	start := time.Now()

	srcEngine, err := NewEngine(srcClient, srcProfile, func(msg string) {
		if info != nil {
			info("source: " + msg)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("source engine: %w", err)
	}
	defer srcEngine.Close()

	dstEngine, err := NewEngine(dstClient, dstProfile, func(msg string) {
		if info != nil {
			info("destination: " + msg)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("destination engine: %w", err)
	}
	defer dstEngine.Close()

	files, err := listRemoteFiles(srcClient, srcDir)
	if err != nil {
		return nil, fmt.Errorf("list source directory: %w", err)
	}

	var totalSize int64
	var totalFiles int
	engineDesc := srcEngine.Name() + "→" + dstEngine.Name()

	for _, srcPath := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		rel := srcPath[len(srcDir):]
		remoteDst := dstDir + rel

		reader, size, err := srcEngine.OpenRead(ctx, srcPath)
		if err != nil {
			return nil, fmt.Errorf("open source %s: %w", rel, err)
		}

		writer, commit, rollback, err := dstEngine.OpenWrite(ctx, remoteDst)
		if err != nil {
			reader.Close()
			return nil, fmt.Errorf("open destination %s: %w", rel, err)
		}

		tracker := NewProgressTracker(path.Base(srcPath), size, progress)
		written, copyErr := copyWithProgress(ctx, writer, reader, tracker)
		writer.Close()
		reader.Close()

		if copyErr != nil {
			rollback()
			return nil, fmt.Errorf("relay %s: %w", rel, copyErr)
		}

		if err := commit(); err != nil {
			rollback()
			return nil, fmt.Errorf("commit %s: %w", rel, err)
		}

		tracker.Finish()
		totalSize += written
		totalFiles++
	}

	return &Result{
		Direction: "relay",
		Remote:    dstDir,
		Size:      totalSize,
		Duration:  formatDuration(time.Since(start)),
		Engine:    engineDesc,
		Files:     totalFiles,
	}, nil
}

func listRemoteFiles(client *ssh.Client, dir string) ([]string, error) {
	eng := newRawEngine(client)
	return eng.remoteListFiles(dir)
}
