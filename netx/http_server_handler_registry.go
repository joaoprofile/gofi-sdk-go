package netx

import (
	"io"

	"github.com/joaoprofile/gofi/base/observer"
)

type Module interface {
	Register(reg *HandlerRegistry)
}

type HandlerRegistry struct {
	handlers  []RouterHandler
	lifecycle *observer.Registry
}

func NewHandlerRegistry(lc *observer.Registry) *HandlerRegistry {
	return &HandlerRegistry{
		lifecycle: lc,
	}
}

func (r *HandlerRegistry) Register(h ...RouterHandler) {
	for _, handler := range h {
		r.handlers = append(r.handlers, handler)

		if c, ok := handler.(io.Closer); ok {
			r.lifecycle.Register(c)
		}
	}
}

func (r *HandlerRegistry) Handlers() []RouterHandler {
	return r.handlers
}
