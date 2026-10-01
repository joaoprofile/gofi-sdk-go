// Package redact renders configuration structs without their secrets, for fmt,
// log/slog and encoding/json alike. A field tagged `redact:"true"` prints as
// Mask when it holds a non-zero value and as "" otherwise; `redact:"url"`
// masks only the password of a URL. A struct opts in by delegating to this
// package from its own methods (value receivers, so pointers work too):
//
//	func (c Config) String() string                { return redact.Sprint(c) }
//	func (c Config) GoString() string              { return redact.GoSprint(c) }
//	func (c Config) Format(f fmt.State, verb rune) { redact.Format(f, verb, c) }
//	func (c Config) LogValue() slog.Value          { return redact.LogValue(c) }
//	func (c Config) MarshalJSON() ([]byte, error)  { return redact.JSON(c) }
//
// Unexported fields are never rendered.
package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"
	"strings"
)

// Mask replaces a secret value.
const Mask = "[REDACTED]"

// String returns Mask for a non-empty s and "" otherwise.
func String(s string) string {
	if s == "" {
		return ""
	}
	return Mask
}

// URL masks the password of a URL, keeping the rest readable. A value that
// does not parse is masked entirely, since it cannot be inspected.
func URL(s string) string {
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil {
		return Mask
	}
	if _, ok := u.User.Password(); !ok {
		return s
	}
	// Mask is swapped in after encoding, which would escape its brackets.
	u.User = url.UserPassword(u.User.Username(), "xxxxx")
	return strings.Replace(u.String(), ":xxxxx@", ":"+Mask+"@", 1)
}

type field struct {
	name     string
	jsonName string
	secret   bool
	value    any // already masked when secret
	zero     bool
	omitZero bool
}

// fields lists the exported fields of the struct v (or *v), masking secrets.
func fields(v any) (reflect.Type, []field) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return rv.Type(), nil
		}
		rv = rv.Elem()
	}
	rt := rv.Type()
	if rt.Kind() != reflect.Struct {
		panic("redact: " + rt.String() + " is not a struct")
	}
	out := make([]field, 0, rt.NumField())
	for i := range rt.NumField() {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		fv := rv.Field(i)
		f := field{name: sf.Name, jsonName: sf.Name, zero: fv.IsZero()}
		f.applyJSONTag(sf)
		f.secret, f.value = redactValue(sf.Tag.Get("redact"), fv, f.zero)
		out = append(out, f)
	}
	return rt, out
}

// applyJSONTag reads the json tag of sf into f's name and omit-zero flag.
func (f *field) applyJSONTag(sf reflect.StructField) {
	tag, ok := sf.Tag.Lookup("json")
	if !ok {
		return
	}
	name, opts, _ := strings.Cut(tag, ",")
	if name == "-" && opts == "" {
		f.jsonName = ""
	} else if name != "" {
		f.jsonName = name
	}
	f.omitZero = strings.Contains(","+opts+",", ",omitempty,") || strings.Contains(","+opts+",", ",omitzero,")
}

// redactValue returns the value to render for fv under the given redact tag,
// and whether it is a secret.
func redactValue(tag string, fv reflect.Value, zero bool) (bool, any) {
	switch tag {
	case "true":
		if zero {
			return true, ""
		}
		return true, Mask
	case "url":
		return true, URL(fv.String())
	default:
		return false, fv.Interface()
	}
}

// Sprint renders v like %+v: {Name:value ...}.
func Sprint(v any) string {
	var b strings.Builder
	write(&b, v, true)
	return b.String()
}

// GoSprint renders v like %#v: pkg.Type{Name:value, ...}.
func GoSprint(v any) string {
	rt, fs := fields(v)
	var b strings.Builder
	if rt.Kind() == reflect.Pointer {
		return fmt.Sprintf("(%s)(nil)", rt)
	}
	b.WriteString(rt.String())
	b.WriteByte('{')
	for i, f := range fs {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s:%#v", f.name, f.value)
	}
	b.WriteByte('}')
	return b.String()
}

// Format implements fmt.Formatter for v, so every verb (%v, %+v, %#v, %s, %d,
// ...) goes through the masked rendering.
func Format(f fmt.State, verb rune, v any) {
	if verb == 'v' && f.Flag('#') {
		_, _ = f.Write([]byte(GoSprint(v)))
		return
	}
	var b strings.Builder
	write(&b, v, f.Flag('+'))
	_, _ = f.Write([]byte(b.String()))
}

func write(b *strings.Builder, v any, names bool) {
	rt, fs := fields(v)
	if rt.Kind() == reflect.Pointer {
		b.WriteString("<nil>")
		return
	}
	b.WriteByte('{')
	for i, f := range fs {
		if i > 0 {
			b.WriteByte(' ')
		}
		if names {
			b.WriteString(f.name)
			b.WriteByte(':')
			fmt.Fprintf(b, "%+v", f.value)
		} else {
			fmt.Fprintf(b, "%v", f.value)
		}
	}
	b.WriteByte('}')
}

// LogValue renders v as a slog group, one attribute per exported field.
func LogValue(v any) slog.Value {
	_, fs := fields(v)
	attrs := make([]slog.Attr, 0, len(fs))
	for _, f := range fs {
		attrs = append(attrs, slog.Any(f.name, f.value))
	}
	return slog.GroupValue(attrs...)
}

// JSON encodes v as an object of its exported fields, honouring json tag
// names, "-", omitempty and omitzero (by zero value).
func JSON(v any) ([]byte, error) {
	rt, fs := fields(v)
	if rt.Kind() == reflect.Pointer {
		return []byte("null"), nil
	}
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for _, f := range fs {
		if f.jsonName == "" || (f.omitZero && f.zero) {
			continue
		}
		key, err := json.Marshal(f.jsonName)
		if err != nil {
			return nil, err
		}
		val, err := json.Marshal(f.value)
		if err != nil {
			return nil, fmt.Errorf("redact: field %s: %w", f.name, err)
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.Write(key)
		b.WriteByte(':')
		b.Write(val)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
