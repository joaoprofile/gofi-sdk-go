package netx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers

type trackingCloser struct {
	io.Reader
	closed bool
}

func (t *trackingCloser) Close() error {
	t.closed = true
	return nil
}

type errorReader struct{}

func (errorReader) Read(_ []byte) (int, error) { return 0, errors.New("read error") }
func (errorReader) Close() error               { return nil }

func newJSONBody(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}

// ReadBody

func TestReadBody_ReturnsContent(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`hello world`))
	body, err := ReadBody(req)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(body))
}

func TestReadBody_EmptyBodyReturnsEmptySlice(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", strings.NewReader(""))
	body, err := ReadBody(req)
	require.NoError(t, err)
	assert.Empty(t, body)
}

func TestReadBody_ClosesBodyOnSuccess(t *testing.T) {
	tc := &trackingCloser{Reader: strings.NewReader("data")}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Body = tc

	_, err := ReadBody(req)
	require.NoError(t, err)
	assert.True(t, tc.closed)
}

func TestReadBody_ClosesBodyOnReadError(t *testing.T) {
	tc := &trackingCloser{Reader: errorReader{}}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Body = tc

	_, err := ReadBody(req)
	assert.Error(t, err)
	assert.True(t, tc.closed)
}

// ParseRequestBody

type samplePayload struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func jsonRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", newJSONBody(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestParseRequestBody_ValidJSON_PopulatesStruct(t *testing.T) {
	for _, ct := range []string{"application/json", "application/json; charset=utf-8", "Application/JSON", "application/merge-patch+json"} {
		req := jsonRequest(`{"name":"Emilia","age":30}`)
		req.Header.Set("Content-Type", ct)
		var out samplePayload
		require.NoError(t, ParseRequestBody(httptest.NewRecorder(), req, &out), ct)
		assert.Equal(t, "Emilia", out.Name)
		assert.Equal(t, 30, out.Age)
	}
}

func TestParseRequestBody_InvalidJSON_ReturnsError(t *testing.T) {
	var out samplePayload
	err := ParseRequestBody(httptest.NewRecorder(), jsonRequest(`{not valid`), &out)
	assert.Error(t, err)
}

func assertRequestError(t *testing.T, err error, status int, kind error, message string) {
	t.Helper()
	var re *RequestError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, status, re.Status)
	assert.Equal(t, message, re.Error())
	assert.ErrorIs(t, err, kind)
}

// Regression: no Content-Type check, unknown fields and trailing data
// accepted, raw encoding/json errors (Go type and field names) returned.
func TestParseRequestBody_Strict(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Admin bool   `json:"-"`
		Age   int8   `json:"age"`
	}
	cases := []struct {
		name, contentType, body string
		status                  int
		kind                    error
		message                 string
	}{
		{"missing content type", "", `{"name":"a"}`, 415, ErrUnsupportedMediaType, "content type must be application/json"},
		{"form content type", "application/x-www-form-urlencoded", `{"name":"a"}`, 415, ErrUnsupportedMediaType, "content type must be application/json"},
		{"text/json", "text/json", `{"name":"a"}`, 415, ErrUnsupportedMediaType, "content type must be application/json"},
		{"unknown field", "application/json", `{"name":"a","role":"admin"}`, 400, ErrInvalidJSON, "invalid JSON body"},
		{"trailing value", "application/json", `{"name":"a"}{"name":"b"}`, 400, ErrInvalidJSON, "invalid JSON body"},
		{"trailing garbage", "application/json", `{"name":"a"} x`, 400, ErrInvalidJSON, "invalid JSON body"},
		{"type mismatch", "application/json", `{"age":"old"}`, 400, ErrInvalidJSON, "invalid JSON body"},
		{"overflow", "application/json", `{"age":300}`, 400, ErrInvalidJSON, "invalid JSON body"},
		{"empty", "application/json", ``, 400, ErrInvalidJSON, "invalid JSON body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			var out payload
			err := ParseRequestBody(httptest.NewRecorder(), req, &out)
			assertRequestError(t, err, tc.status, tc.kind, tc.message)
			assert.NotContains(t, err.Error(), "payload", "no Go type names")
			assert.NotContains(t, err.Error(), "int8")
		})
	}
}

func TestParseRequestBody_CauseStaysLoggable(t *testing.T) {
	var out samplePayload
	err := ParseRequestBody(httptest.NewRecorder(), jsonRequest(`{"name":"a","role":"x"}`), &out)
	assert.Equal(t, "invalid JSON body", err.Error())
	assert.Contains(t, errors.Unwrap(err).Error(), `unknown field "role"`)
}

func TestParseRequestBody_TooLarge(t *testing.T) {
	var out samplePayload
	body := `{"name":"` + strings.Repeat("a", 100) + `"}`
	err := ParseRequestBody(httptest.NewRecorder(), jsonRequest(body), &out, WithMaxBodyBytes(32))
	assertRequestError(t, err, http.StatusRequestEntityTooLarge, ErrBodyTooLarge, "request body too large")

	// The server-wide cap surfaces the same way.
	req := jsonRequest(body)
	rec := httptest.NewRecorder()
	req.Body = http.MaxBytesReader(rec, req.Body, 16)
	err = ParseRequestBody(rec, req, &out)
	assertRequestError(t, err, http.StatusRequestEntityTooLarge, ErrBodyTooLarge, "request body too large")

	rec = httptest.NewRecorder()
	Error(rec, http.StatusBadRequest, err)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestParseRequestBody_AllowUnknownFields(t *testing.T) {
	var out samplePayload
	require.NoError(t, ParseRequestBody(nil, jsonRequest(`{"name":"a","extra":1}`), &out, AllowUnknownFields()))
	assert.Equal(t, "a", out.Name)
}

func TestParseRequestBody_NilBody(t *testing.T) {
	req := jsonRequest("")
	req.Body = nil
	var out samplePayload
	assertRequestError(t, ParseRequestBody(nil, req, &out), http.StatusBadRequest, ErrInvalidJSON, "invalid JSON body")
}

func TestParseRequestBody_NonPointer_ReturnsErrInvalidStruct(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", newJSONBody(`{}`))
	var out samplePayload
	err := ParseRequestBody(httptest.NewRecorder(), req, out)
	assert.ErrorIs(t, err, ErrInvalidStruct)
}

func TestParseRequestBody_PointerToNonStruct_ReturnsErrInvalidStruct(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", newJSONBody(`{}`))
	s := "not a struct"
	err := ParseRequestBody(httptest.NewRecorder(), req, &s)
	assert.ErrorIs(t, err, ErrInvalidStruct)
}

func TestParseRequestBody_ReadError_ReturnsError(t *testing.T) {
	// errorReader is already defined in this test file.
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Body = &trackingCloser{Reader: errorReader{}}

	var out samplePayload
	err := ParseRequestBody(httptest.NewRecorder(), req, &out)
	assert.Error(t, err)
}

//  GetQueryParam ─

func TestGetQueryParam_ReturnsEmptyWhenAbsent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.Empty(t, GetQueryParam("missing", req))
}

//  GetPathParam

//  BindQueryParamsToStruct ─

type filterParams struct {
	Name   string `form:"name"`
	Status string `form:"status"`
	Page   int    `form:"page"`
	Limit  uint   `form:"limit"`
}

func TestBindQueryParams_StringFieldsPopulated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?name=widgets&status=active", nil)
	var out filterParams
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Equal(t, "widgets", out.Name)
	assert.Equal(t, "active", out.Status)
}

func TestBindQueryParams_IntFieldPopulated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?page=3", nil)
	var out filterParams
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Equal(t, 3, out.Page)
}

func TestBindQueryParams_UintFieldPopulated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?limit=25", nil)
	var out filterParams
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Equal(t, uint(25), out.Limit)
}

func TestBindQueryParams_NoFormTag_FallsBackToLowercasedFieldName(t *testing.T) {
	type noTagParams struct {
		Name string
		Page int
	}
	req := httptest.NewRequest(http.MethodGet, "/?name=widgets&page=3", nil)
	var out noTagParams
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Equal(t, "widgets", out.Name)
	assert.Equal(t, 3, out.Page)
}

func TestBindQueryParams_NonPointer_ReturnsError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	var out filterParams
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), out)
	assert.Error(t, err)
}

func TestBindQueryParams_InvalidIntValue_ReturnsError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?page=abc", nil)
	var out filterParams
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	assert.Error(t, err)
}

//  BindQueryParamsToStruct: slice support

type filterWithSlice struct {
	IDs   []int32  `form:"ids"`
	Tags  []string `form:"tags"`
	Names []string
	Page  uint16 `form:"page"`
}

func TestBindQueryParams_SliceInt32_RepeatedParams(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?ids=1&ids=2&ids=3", nil)
	var out filterWithSlice
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Equal(t, []int32{1, 2, 3}, out.IDs)
}

func TestBindQueryParams_SliceInt32_JSONLiteral(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?ids=[1,2,3]", nil)
	var out filterWithSlice
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Equal(t, []int32{1, 2, 3}, out.IDs)
}

func TestBindQueryParams_SliceString_RepeatedParams(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?tags=a&tags=b", nil)
	var out filterWithSlice
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, out.Tags)
}

func TestBindQueryParams_SliceAbsent_LeavesNil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?page=2", nil)
	var out filterWithSlice
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Nil(t, out.IDs)
	assert.Nil(t, out.Tags)
	assert.Equal(t, uint16(2), out.Page)
}

func TestBindQueryParams_SliceInvalidElement_ReturnsErrorWithIndex(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?ids=1&ids=abc", nil)
	var out filterWithSlice
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	assertRequestError(t, err, http.StatusBadRequest, ErrInvalidQueryParam, "invalid query parameter: ids")
	assert.Contains(t, errors.Unwrap(err).Error(), "ids: [1]", "the index stays in the logged cause")
}

// Regression: the error echoed the client's input.
func TestBindQueryParams_ErrorDoesNotEchoInput(t *testing.T) {
	for _, q := range []string{"/?page=<script>", "/?ids=[1,%22<x>%22]", "/?ids=1,<x>"} {
		var out filterWithSlice
		err := BindQueryParamsToStruct(httptest.NewRequest(http.MethodGet, q, nil), nil, &out)
		require.Error(t, err, q)
		assert.NotContains(t, err.Error(), "<", q)
	}
}

// Regression: an unexported field matching a parameter panicked (SetString).
func TestBindQueryParams_IgnoresUnexportedAndSkipped(t *testing.T) {
	type params struct {
		Name   string
		secret string
		Hidden string `form:"-"`
	}
	req := httptest.NewRequest(http.MethodGet, "/?name=a&secret=x&hidden=y&-=z", nil)
	var out params
	require.NotPanics(t, func() { require.NoError(t, BindQueryParamsToStruct(req, nil, &out)) })
	assert.Equal(t, params{Name: "a"}, out)
	assert.Empty(t, out.secret)
}

// Regression: ParseInt(s, 10, 64) + SetInt wrapped silently on small ints.
func TestBindQueryParams_RejectsOverflow(t *testing.T) {
	type params struct {
		I8  int8    `form:"i8"`
		I32 int32   `form:"i32"`
		U8  uint8   `form:"u8"`
		U16 *uint16 `form:"u16"`
		S   []int8  `form:"s"`
	}
	for _, q := range []string{"i8=128", "i8=-129", "i32=2147483648", "u8=256", "u8=-1", "u16=65536", "s=1,200"} {
		var out params
		err := BindQueryParamsToStruct(httptest.NewRequest(http.MethodGet, "/?"+q, nil), nil, &out)
		assertRequestError(t, err, http.StatusBadRequest, ErrInvalidQueryParam, "invalid query parameter: "+strings.Split(q, "=")[0])
	}
	var out params
	req := httptest.NewRequest(http.MethodGet, "/?i8=-128&i32=2147483647&u8=255&u16=65535&s=127,-128", nil)
	require.NoError(t, BindQueryParamsToStruct(req, nil, &out))
	assert.Equal(t, int8(-128), out.I8)
	assert.Equal(t, int32(2147483647), out.I32)
	assert.Equal(t, uint8(255), out.U8)
	assert.Equal(t, uint16(65535), *out.U16)
	assert.Equal(t, []int8{127, -128}, out.S)
}

type fuzzParams struct {
	S      string   `form:"s"`
	B      bool     `form:"b"`
	I      int      `form:"i"`
	I8     int8     `form:"i8"`
	I16    int16    `form:"i16"`
	U      uint     `form:"u"`
	U8     uint8    `form:"u8"`
	U64    uint64   `form:"u64"`
	PI     *int32   `form:"pi"`
	PS     *string  `form:"ps"`
	SI     []int16  `form:"si"`
	SS     []string `form:"ss"`
	SP     []*uint8 `form:"sp"`
	F      float64  `form:"f"`
	M      map[string]string
	Nested struct{ A int }
	hidden int
	lower  string
}

func FuzzBindQueryParams(f *testing.F) {
	for _, seed := range []string{
		"s=x&b=true&i=1&i8=127&u8=255", "hidden=1&lower=x", "si=[1,2]&ss=a,b&sp=1,2",
		"i8=999&u=-1&pi=x", "f=1.5&m=x&nested=y", "si=[1,&ss=[", "%zz", "sp=[null,1]",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, rawQuery string) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.RawQuery = rawQuery
		var out fuzzParams
		err := BindQueryParamsToStruct(req, nil, &out)
		var re *RequestError
		if err != nil && !errors.As(err, &re) {
			t.Fatalf("unexpected error type %T: %v", err, err)
		}
		if out.hidden != 0 || out.lower != "" {
			t.Fatal("unexported field was set")
		}
	})
}

func TestBindQueryParams_SliceMalformedJSON_ReturnsError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?ids=[1,abc]", nil)
	var out filterWithSlice
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ids")
}

func TestBindQueryParams_SliceMixedWithScalar(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?ids=1&ids=2&page=5", nil)
	var out filterWithSlice
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Equal(t, []int32{1, 2}, out.IDs)
	assert.Equal(t, uint16(5), out.Page)
}

func TestBindQueryParams_SliceCommaSeparatedAndRepeated(t *testing.T) {
	// Acceptance criterion: ?ids=1,2&ids=2&page=15 must populate both fields.
	req := httptest.NewRequest(http.MethodGet, "/?ids=1,2&ids=2&page=15", nil)
	var out filterWithSlice
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Equal(t, []int32{1, 2, 2}, out.IDs)
	assert.Equal(t, uint16(15), out.Page)
}

func TestBindQueryParams_SliceFallbackFieldName(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?names=foo&names=bar", nil)
	var out filterWithSlice
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Equal(t, []string{"foo", "bar"}, out.Names)
}

//  BindQueryParamsToStruct: bool and pointer support

type filterWithBoolAndPtr struct {
	Active  bool    `form:"active"`
	Verbose *bool   `form:"verbose"`
	Page    *int    `form:"page"`
	Search  *string `form:"search"`
	Flags   []bool  `form:"flags"`
	Sizes   []*int  `form:"sizes"`
}

func TestBindQueryParams_Bool(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?active=true", nil)
	var out filterWithBoolAndPtr
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.True(t, out.Active)
}

func TestBindQueryParams_PtrBool(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?verbose=false", nil)
	var out filterWithBoolAndPtr
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	require.NotNil(t, out.Verbose)
	assert.False(t, *out.Verbose)
}

func TestBindQueryParams_PtrInt(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?page=7", nil)
	var out filterWithBoolAndPtr
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	require.NotNil(t, out.Page)
	assert.Equal(t, 7, *out.Page)
}

func TestBindQueryParams_PtrString(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?search=hello", nil)
	var out filterWithBoolAndPtr
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	require.NotNil(t, out.Search)
	assert.Equal(t, "hello", *out.Search)
}

func TestBindQueryParams_PtrAbsent_LeavesNil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?active=true", nil)
	var out filterWithBoolAndPtr
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Nil(t, out.Verbose)
	assert.Nil(t, out.Page)
	assert.Nil(t, out.Search)
}

func TestBindQueryParams_SliceBool_RepeatedParams(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?flags=true&flags=false&flags=1", nil)
	var out filterWithBoolAndPtr
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	assert.Equal(t, []bool{true, false, true}, out.Flags)
}

func TestBindQueryParams_SlicePtrInt(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?sizes=1,2,3", nil)
	var out filterWithBoolAndPtr
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	require.NoError(t, err)
	require.Len(t, out.Sizes, 3)
	for i, want := range []int{1, 2, 3} {
		require.NotNil(t, out.Sizes[i])
		assert.Equal(t, want, *out.Sizes[i])
	}
}

func TestBindQueryParams_InvalidBool_ReturnsError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?active=not-a-bool", nil)
	var out filterWithBoolAndPtr
	err := BindQueryParamsToStruct(req, httptest.NewRecorder(), &out)
	assert.Error(t, err)
}

//  setFieldValue ─

func makeValue(v any) reflect.Value {
	ptr := reflect.New(reflect.TypeOf(v))
	ptr.Elem().Set(reflect.ValueOf(v))
	return ptr.Elem()
}

func TestSetFieldValue_String(t *testing.T) {
	v := makeValue("")
	require.NoError(t, setFieldValue(v, "hello"))
	assert.Equal(t, "hello", v.String())
}

func TestSetFieldValue_Int(t *testing.T) {
	v := makeValue(int(0))
	require.NoError(t, setFieldValue(v, "-42"))
	assert.Equal(t, int64(-42), v.Int())
}

func TestSetFieldValue_Uint(t *testing.T) {
	v := makeValue(uint(0))
	require.NoError(t, setFieldValue(v, "99"))
	assert.Equal(t, uint64(99), v.Uint())
}

func TestSetFieldValue_Bool(t *testing.T) {
	v := makeValue(false)
	require.NoError(t, setFieldValue(v, "true"))
	assert.True(t, v.Bool())
}

func TestSetFieldValue_PtrString_AllocatesAndSets(t *testing.T) {
	var s *string
	v := reflect.ValueOf(&s).Elem()
	require.NoError(t, setFieldValue(v, "hello"))
	require.NotNil(t, s)
	assert.Equal(t, "hello", *s)
}

func TestSetFieldValue_PtrInt_AllocatesAndSets(t *testing.T) {
	var n *int
	v := reflect.ValueOf(&n).Elem()
	require.NoError(t, setFieldValue(v, "-42"))
	require.NotNil(t, n)
	assert.Equal(t, -42, *n)
}

func TestSetFieldValue_PtrUint_AllocatesAndSets(t *testing.T) {
	var n *uint
	v := reflect.ValueOf(&n).Elem()
	require.NoError(t, setFieldValue(v, "99"))
	require.NotNil(t, n)
	assert.Equal(t, uint(99), *n)
}

func TestSetFieldValue_PtrBool_AllocatesAndSets(t *testing.T) {
	var b *bool
	v := reflect.ValueOf(&b).Elem()
	require.NoError(t, setFieldValue(v, "true"))
	require.NotNil(t, b)
	assert.True(t, *b)
}

func TestSetFieldValue_PtrBool_ReusesExistingPointer(t *testing.T) {
	existing := false
	b := &existing
	v := reflect.ValueOf(&b).Elem()
	require.NoError(t, setFieldValue(v, "true"))
	assert.Same(t, &existing, b)
	assert.True(t, *b)
}

func TestSetFieldValue_UnsupportedKind_ReturnsError(t *testing.T) {
	v := makeValue(float64(0))
	assert.Error(t, setFieldValue(v, "1.5"))
}

func TestSetFieldValue_InvalidInt_ReturnsError(t *testing.T) {
	v := makeValue(int(0))
	assert.Error(t, setFieldValue(v, "not-a-number"))
}

func TestSetFieldValue_InvalidUint_ReturnsError(t *testing.T) {
	v := makeValue(uint(0))
	assert.Error(t, setFieldValue(v, "-1"))
}

func TestSetFieldValue_InvalidBool_ReturnsError(t *testing.T) {
	v := makeValue(false)
	assert.Error(t, setFieldValue(v, "not-a-bool"))
}

func TestSetFieldValue_InvalidPtrInt_ReturnsError(t *testing.T) {
	var n *int
	v := reflect.ValueOf(&n).Elem()
	assert.Error(t, setFieldValue(v, "not-a-number"))
}
