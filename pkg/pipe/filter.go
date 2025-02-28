package pipe

import (
	"context"
	"log/slog"

	"github.com/expr-lang/expr"
)

// Filter is a custom type in order to support unmarshalling
type Filter[T any] func(T) bool

func (f *Filter[T]) UnmarshalText(data []byte) error {
	var err error
	*f, err = ParseFilter[T](string(data))
	if err != nil {
		return err
	}
	return nil
}

func ParseFilter[T any](filter string) (Filter[T], error) {
	prg, err := expr.Compile(filter, expr.Env(new(T)), expr.AsBool(), expr.WarnOnAny())
	if err != nil {
		return nil, err
	}

	return func(t T) bool {
		res, _ := expr.Run(prg, t)
		return res.(bool)
	}, nil
}

func (f Filter[T]) Run(ctx context.Context, in <-chan T, out chan T, log *slog.Logger) {
	var filtered, total int
	defer func() {
		close(out)
		if log != nil {
			log.Info("filter summary", slog.Int("total", total), slog.Int("filtered", filtered))
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case t, ok := <-in:
			if !ok {
				return
			}
			total++
			if f(t) {
				filtered++
				out <- t
			}
		}
	}
}
