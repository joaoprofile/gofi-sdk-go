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

func TestParseRequestBody_ValidJSON_PopulatesStruct(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", newJSONBody(`{"name":"Emilia","age":30}`))
	var out samplePayload
	err := ParseRequestBody(httptest.NewRecorder(), req, &out)
	require.NoError(t, err)
	assert.Equal(t, "Emilia", out.Name)
	assert.Equal(t, 30, out.Age)
}

func TestParseRequestBody_InvalidJSON_ReturnsError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", newJSONBody(`{not valid`))
	var out samplePayload
	err := ParseRequestBody(httptest.NewRecorder(), req, &out)
	assert.Error(t, err)
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

func TestGetQueryParam_ReturnsLowercasedValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?status=ACTIVE", nil)
	assert.Equal(t, "active", GetQueryParam("status", req))
}

func TestGetQueryParam_ReturnsEmptyWhenAbsent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.Empty(t, GetQueryParam("missing", req))
}

//  GetPathParam

func TestGetPathParam_ReturnsLowercasedValue(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /products/{id}", func(w http.ResponseWriter, r *http.Request) {
		captured = GetPathParam("id", r)
		w.WriteHeader(http.StatusOK)
	})
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/products/SKU-ABC", nil))
	assert.Equal(t, "sku-abc", captured)
}

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
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ids[1]")
	assert.Contains(t, err.Error(), "int32")
	assert.Contains(t, err.Error(), "abc")
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

//  setFieldValue ─

func makeValue(v interface{}) reflect.Value {
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

func TestSetFieldValue_UnsupportedKind_ReturnsError(t *testing.T) {
	v := makeValue(false)
	assert.Error(t, setFieldValue(v, "true"))
}

func TestSetFieldValue_InvalidInt_ReturnsError(t *testing.T) {
	v := makeValue(int(0))
	assert.Error(t, setFieldValue(v, "not-a-number"))
}

func TestSetFieldValue_InvalidUint_ReturnsError(t *testing.T) {
	v := makeValue(uint(0))
	assert.Error(t, setFieldValue(v, "-1"))
}
