package transfer

import (
	"context"
	"fmt"
	"io"

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

func (e *rawEngine) Upload(_ context.Context, _, _ string, _ ProgressFunc) (*Result, error) {
	return nil, fmt.Errorf("raw stream upload not yet implemented")
}

func (e *rawEngine) Download(_ context.Context, _, _ string, _ ProgressFunc) (*Result, error) {
	return nil, fmt.Errorf("raw stream download not yet implemented")
}

func (e *rawEngine) UploadRecursive(_ context.Context, _, _ string, _ ProgressFunc) (*Result, error) {
	return nil, fmt.Errorf("raw stream recursive upload not yet implemented")
}

func (e *rawEngine) DownloadRecursive(_ context.Context, _, _ string, _ ProgressFunc) (*Result, error) {
	return nil, fmt.Errorf("raw stream recursive download not yet implemented")
}

func (e *rawEngine) OpenRead(_ context.Context, _ string) (io.ReadCloser, int64, error) {
	return nil, 0, fmt.Errorf("raw stream OpenRead not yet implemented")
}

func (e *rawEngine) OpenWrite(_ context.Context, _ string) (io.WriteCloser, func() error, func(), error) {
	return nil, nil, nil, fmt.Errorf("raw stream OpenWrite not yet implemented")
}
