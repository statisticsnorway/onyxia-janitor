package filter

import "github.com/expr-lang/expr"

type Filter[T any] func(T) bool

func (f *Filter[T]) UnmarshalText(data []byte) error {
	var err error
	*f, err = Parse[T](string(data))
	if err != nil {
		return err
	}
	return nil
}

func (f Filter[T]) FilterSlice(s []T) []T {
	return FilterSlice(nil, s, f)
}

func Parse[T any](filter string) (Filter[T], error) {
	prg, err := expr.Compile(filter, expr.Env(new(T)), expr.AsBool(), expr.WarnOnAny())
	if err != nil {
		return nil, err
	}

	return func(t T) bool {
		res, _ := expr.Run(prg, t)
		return res.(bool)
	}, nil
}

func FilterSlice[T any](d []T, s []T, keep Filter[T]) []T {
	for _, t := range s {
		if keep(t) {
			d = append(d, t)
		}
	}
	return d
}
