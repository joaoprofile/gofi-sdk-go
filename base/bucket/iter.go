package bucket

import (
	"context"
	"iter"
)

// Walker is implemented by stores that stream listings page by page.
type Walker interface {
	All(ctx context.Context, prefix string) iter.Seq2[Object, error]
}

// All yields the objects whose key starts with prefix without loading the
// whole listing in memory when the store is a Walker. A listing error is
// yielded once and ends the sequence.
func All(ctx context.Context, s Store, prefix string) iter.Seq2[Object, error] {
	if w, ok := s.(Walker); ok {
		return w.All(ctx, prefix)
	}
	return func(yield func(Object, error) bool) {
		objs, err := s.List(ctx, prefix)
		if err != nil {
			yield(Object{}, err)
			return
		}
		for _, o := range objs {
			if !yield(o, nil) {
				return
			}
		}
	}
}

// Collect drains a listing sequence; stores use it to implement List on top
// of All. The result is never nil.
func Collect(seq iter.Seq2[Object, error]) ([]Object, error) {
	out := make([]Object, 0)
	for o, err := range seq {
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}
