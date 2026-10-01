package observer

import (
	"context"
	"errors"
	"io"
	"slices"
	"time"
)

type Closer interface {
	Close(ctx context.Context) error
}

type Registry struct {
	closers []any
}

func New() *Registry {
	return &Registry{}
}

func (r *Registry) Register(c ...any) {
	r.closers = append(r.closers, c...)
}

// CloseAll closes registered resources in reverse (LIFO) order, continues past
// failures and returns every error joined.
func (r *Registry) CloseAll(ctx context.Context) error {
	var errs []error
	for _, v := range slices.Backward(r.closers) {
		switch v := v.(type) {
		case Closer:
			errs = append(errs, v.Close(ctx))
		case io.Closer:
			errs = append(errs, v.Close())
		}
	}
	return errors.Join(errs...)
}

func (r *Registry) CloseWithTimeout(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return r.CloseAll(ctx)
}
