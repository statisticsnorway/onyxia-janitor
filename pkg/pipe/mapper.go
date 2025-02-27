package pipe

import (
	"context"
	"log/slog"
)

func Map[T, U any](ctx context.Context, mapper func(T) (*U, error), in <-chan T, out chan U) {
	defer close(out)
	for {
		select {
		case <-ctx.Done():
			return
		case t, ok := <-in:
			if !ok {
				return
			}
			u, err := mapper(t)
			if err != nil {
				slog.Error("mapping error", "err", err)
				continue
			}
			out <- *u
		}
	}
}
