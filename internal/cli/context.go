package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
)

type writerKey struct{}
type configKey struct{}
type profileCacheKey struct{}

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

func withProfileCache(ctx context.Context, c *remote.Cache) context.Context {
	return context.WithValue(ctx, profileCacheKey{}, c)
}

func profileCacheFrom(ctx context.Context) *remote.Cache {
	if c, ok := ctx.Value(profileCacheKey{}).(*remote.Cache); ok {
		return c
	}
	return nil
}

func connErrorToOutput(err error, alias string) *output.CmdError {
	var ce *sshclient.ConnError
	if !errors.As(err, &ce) {
		return output.Errorf(err.Error(), "check connectivity and credentials")
	}
	switch ce.Kind {
	case sshclient.ErrHostKeyMismatch:
		return output.Errorf(
			fmt.Sprintf("host key CHANGED for %s (%s:%s)", alias, ce.Host, ce.Port),
			fmt.Sprintf("if expected, run: sshq trust %s --replace", alias),
		)
	case sshclient.ErrHostKeyUnknown:
		return output.Errorf(
			fmt.Sprintf("host key unknown for %s (%s:%s)", alias, ce.Host, ce.Port),
			fmt.Sprintf("run: sshq trust %s", alias),
		)
	case sshclient.ErrAuth:
		return output.Errorf(ce.Error(), "check credentials and key file")
	case sshclient.ErrNetwork:
		return output.Errorf(ce.Error(), "check network connectivity")
	default:
		return output.Errorf(ce.Error(), "check connectivity and credentials")
	}
}
