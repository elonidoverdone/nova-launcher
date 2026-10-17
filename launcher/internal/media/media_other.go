//go:build !windows

package media

import (
	"context"
	"errors"

	"nova/launcher/internal/lyrics"
)

func run(ctx context.Context, fn func(lyrics.Sample)) error {
	<-ctx.Done()
	return errors.New("media controls are only supported on Windows")
}
