package pipe

import (
	"context"

	"github.com/expr-lang/expr"
)

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

func (f Filter[T]) Run(ctx context.Context, in <-chan T, out chan T) {
	defer close(out)
	for {
		select {
		case <-ctx.Done():
			return
		case t, ok := <-in:
			if !ok {
				return
			}
			if f(t) {
				out <- t
			}
		}
	}
}
