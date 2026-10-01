package logging

import (
	"context"
	"log/slog"
	"slices"
	"strings"
)

// RedactedValue replaces the value of a sensitive attribute.
const RedactedValue = "[REDACTED]"

// defaultRedactKeys are always masked; a key matches when, lowercased and with
// "-" read as "_", it equals or ends with an entry (db_password, X-Api-Key).
var defaultRedactKeys = []string{
	"password", "passwd", "secret", "token", "access_token", "refresh_token",
	"authorization", "cookie", "set-cookie", "api_key", "apikey", "private_key",
	"passphrase", "client_secret", "credential", "credentials",
}

// DefaultRedactKeys returns the attribute keys masked by every logger.
func DefaultRedactKeys() []string { return slices.Clone(defaultRedactKeys) }

// redactor matches attribute keys against the sensitive list.
type redactor struct{ keys []string }

func newRedactor(extra []string) *redactor {
	r := &redactor{}
	for _, k := range slices.Concat(defaultRedactKeys, extra) {
		if k = normalizeKey(k); k != "" && !slices.Contains(r.keys, k) {
			r.keys = append(r.keys, k)
		}
	}
	return r
}

func normalizeKey(k string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(k)), "-", "_")
}

func (r *redactor) sensitive(key string) bool {
	k := normalizeKey(key)
	for _, s := range r.keys {
		if strings.HasSuffix(k, s) {
			return true
		}
	}
	return false
}

// replaceAttr is a slog.HandlerOptions.ReplaceAttr: it masks an attribute whose
// key, or the key of an enclosing group, is sensitive.
func (r *redactor) replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		return a // built-in handlers call ReplaceAttr on each member instead
	}
	if r.sensitive(a.Key) || slices.ContainsFunc(groups, r.sensitive) {
		return mask(a)
	}
	return a
}

// redact resolves a and masks it, recursing into groups; used where
// ReplaceAttr is not available (attached handlers).
func (r *redactor) redact(a slog.Attr, inSensitiveGroup bool) slog.Attr {
	a.Value = a.Value.Resolve()
	sensitive := inSensitiveGroup || r.sensitive(a.Key)
	if a.Value.Kind() != slog.KindGroup {
		if sensitive {
			return mask(a)
		}
		return a
	}
	members := a.Value.Group()
	out := make([]slog.Attr, len(members))
	for i, m := range members {
		out[i] = r.redact(m, sensitive)
	}
	return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
}

func mask(a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindString && a.Value.String() == "" {
		return a
	}
	return slog.String(a.Key, RedactedValue)
}

// RedactAttr returns a slog.HandlerOptions.ReplaceAttr that masks the values
// of sensitive keys (DefaultRedactKeys plus extra), for handlers built outside
// this package.
func RedactAttr(extra ...string) func(groups []string, a slog.Attr) slog.Attr {
	return newRedactor(extra).replaceAttr
}

// NewRedactHandler wraps h so records reach it with the values of sensitive
// keys (DefaultRedactKeys plus extra) masked; use it for handlers that take no
// ReplaceAttr, such as exporters. Attach already applies it.
func NewRedactHandler(h slog.Handler, extra ...string) slog.Handler {
	return &redactHandler{Handler: h, r: newRedactor(extra)}
}

// redactHandler masks sensitive attributes before they reach Handler.
type redactHandler struct {
	slog.Handler
	r *redactor
	// sensitiveGroup is set once a WithGroup name is sensitive.
	sensitiveGroup bool
}

func (h *redactHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.r.redact(a, h.sensitiveGroup))
		return true
	})
	return h.Handler.Handle(ctx, out)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	masked := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		masked[i] = h.r.redact(a, h.sensitiveGroup)
	}
	return &redactHandler{Handler: h.Handler.WithAttrs(masked), r: h.r, sensitiveGroup: h.sensitiveGroup}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{
		Handler:        h.Handler.WithGroup(name),
		r:              h.r,
		sensitiveGroup: h.sensitiveGroup || h.r.sensitive(name),
	}
}
