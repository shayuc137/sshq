package cli

import (
	"context"
	"os"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/output"
)

type writerKey struct{}
type configKey struct{}

func withWriter(ctx context.Context, w *output.Writer) context.Context {
	return context.WithValue(ctx, writerKey{}, w)
}

func writerFrom(ctx context.Context) *output.Writer {
	if w, ok := ctx.Value(writerKey{}).(*output.Writer); ok {
		return w
	}
	return output.New(os.Stdout, os.Stderr)
}

func withConfig(ctx context.Context, s *config.Store) context.Context {
	return context.WithValue(ctx, configKey{}, s)
}

func configFrom(ctx context.Context) *config.Store {
	if s, ok := ctx.Value(configKey{}).(*config.Store); ok {
		return s
	}
	return nil
}
