package bucket

import "context"

// Statter is implemented by stores that read metadata without the body.
type Statter interface {
	Stat(ctx context.Context, key string) (Object, error)
}

// Stat uses Statter when available; otherwise Get and closes the body. It
// returns ErrNotFound when the key does not exist.
func Stat(ctx context.Context, s Store, key string) (Object, error) {
	if st, ok := s.(Statter); ok {
		return st.Stat(ctx, key)
	}
	o, rc, err := s.Get(ctx, key)
	if err != nil {
		return Object{}, err
	}
	rc.Close()
	return o, nil
}
