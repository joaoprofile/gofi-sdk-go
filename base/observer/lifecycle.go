package observer

import (
	"context"
	"io"
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

func (r *Registry) CloseAll(ctx context.Context) error {
	for _, c := range r.closers {
		switch v := c.(type) {

		case Closer:
			if err := v.Close(ctx); err != nil {
				return err
			}

		case io.Closer:
			if err := v.Close(); err != nil {
				return err
			}

		default:
		}
	}

	return nil
}

func (r *Registry) CloseWithTimeout(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return r.CloseAll(ctx)
}
