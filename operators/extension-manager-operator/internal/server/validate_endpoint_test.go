package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/go-multierror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"

	"go.platform-mesh.io/extension-manager-operator/pkg/validation"
	"go.platform-mesh.io/extension-manager-operator/pkg/validation/mocks"
)

type responseError struct {
	ValidationErrors []validationError `json:"validationErrors,omitempty"`
}

type responseSuccess struct {
	ParsedConfiguration string `json:"parsedConfiguration,omitempty"`
}

func TestHandlerValidate_Error(t *testing.T) {

	logcfg := logger.DefaultConfig()
	log, _ := logger.New(logcfg)

	handler := NewHttpValidateHandler(log, validation.NewContentConfiguration())

	reqBody := ERROR_INVALID_JSON_CONTENT
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	handler.HandlerValidate(w, req)

	resp := w.Result()

	r := &responseError{}
	decoder := json.NewDecoder(resp.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(r)
	defer func() {
		err := resp.Body.Close()
		assert.NoError(t, err)
	}()

	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.GreaterOrEqual(t, len(r.ValidationErrors), 1)
}

func TestHandlerValidate_Success(t *testing.T) {

	logcfg := logger.DefaultConfig()
	log, _ := logger.New(logcfg)

	handler := NewHttpValidateHandler(log, validation.NewContentConfiguration())

	reqBody := OK_VALID_JSON_CONTENT
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	handler.HandlerValidate(w, req)

	resp := w.Result()
	defer func() {
		err := resp.Body.Close()
		assert.NoError(t, err)
	}()

	decoder := json.NewDecoder(resp.Body)
	decoder.DisallowUnknownFields()

	r := &responseSuccess{}
	decoder.DisallowUnknownFields()
	err := decoder.Decode(r)

	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.GreaterOrEqual(t, len(r.ParsedConfiguration), 0)
}

func TestYAML_Success(t *testing.T) {

	logcfg := logger.DefaultConfig()
	log, _ := logger.New(logcfg)

	handler := NewHttpValidateHandler(log, validation.NewContentConfiguration())

	reqBody := OK_VALID_YAML_CONTENT
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	handler.HandlerValidate(w, req)

	resp := w.Result()
	defer func() {
		err := resp.Body.Close()
		assert.NoError(t, err)
	}()

	decoder := json.NewDecoder(resp.Body)
	decoder.DisallowUnknownFields()
	r := &responseSuccess{}
	err := decoder.Decode(r)
	assert.Nil(t, err)
	assert.Greater(t, len(r.ParsedConfiguration), 0)

	decoder = json.NewDecoder(resp.Body)
	decoder.DisallowUnknownFields()
	re := &responseError{}
	err = decoder.Decode(re)
	assert.NotNil(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, len(re.ValidationErrors), 0)
}

func TestYAML_FailureWrongType(t *testing.T) {

	logcfg := logger.DefaultConfig()
	log, _ := logger.New(logcfg)

	handler := NewHttpValidateHandler(log, validation.NewContentConfiguration())

	reqBody := ERROR_INVALID_JSON_CONTENT_WRONG_TYPE
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	handler.HandlerValidate(w, req)

	resp := w.Result()
	defer func() {
		err := resp.Body.Close()
		assert.NoError(t, err)
	}()

	decoder := json.NewDecoder(resp.Body)
	decoder.DisallowUnknownFields()
	re := &responseError{}
	err := decoder.Decode(re)
	assert.Nil(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.GreaterOrEqual(t, len(re.ValidationErrors), 1)
}

func TestValidation_Error(t *testing.T) {

	logcfg := logger.DefaultConfig()
	log, _ := logger.New(logcfg)

	mockValidator := mocks.NewExtensionConfiguration(t)
	merr := &multierror.Error{Errors: []error{errors.New("error")}}
	mockValidator.On("Validate", mock.Anything, mock.Anything).Return("", merr)
	handler := NewHttpValidateHandler(log, mockValidator)

	// handler := NewHttpValidateHandler(log, validation.NewContentConfiguration())

	reqBody := ERROR_INVALID_JSON_CONTENT2
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	handler.HandlerValidate(w, req)

	resp := w.Result()

	r := &responseError{}
	decoder := json.NewDecoder(resp.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(r)
	defer func() {
		err := resp.Body.Close()
		assert.NoError(t, err)
	}()

	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.GreaterOrEqual(t, len(r.ValidationErrors), 1)
}

type errorReadCloser struct {
	io.Reader
}

func (e *errorReadCloser) Close() error {
	return errors.New("simulated close error")
}

func TestHandlerValidate_BodyCloseError(t *testing.T) {
	logcfg := logger.DefaultConfig()
	log, _ := logger.New(logcfg)
	handler := NewHttpValidateHandler(log, validation.NewContentConfiguration())

	reqBody := OK_VALID_JSON_CONTENT // or any valid JSON
	req := httptest.NewRequest(http.MethodPost, "/validate", &errorReadCloser{Reader: bytes.NewBufferString(reqBody)})
	w := httptest.NewRecorder()

	handler.HandlerValidate(w, req)

	resp := w.Result()
	defer func() {
		err := resp.Body.Close()
		assert.NoError(t, err)
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// Optionally, check logs or sentry if you have hooks/mocks for them
}

func TestValidation_ErrorMarshallingValidatedResponse(t *testing.T) {

	logcfg := logger.DefaultConfig()
	log, _ := logger.New(logcfg)

	mockValidator := mocks.NewExtensionConfiguration(t)
	merr := &multierror.Error{Errors: []error{errors.New("error")}}
	mockValidator.On("Validate", mock.Anything, mock.Anything).Return("{ field: }", merr)
	handler := NewHttpValidateHandler(log, mockValidator)

	// handler := NewHttpValidateHandler(log, validation.NewContentConfiguration())

	reqBody := ERROR_INVALID_JSON_CONTENT_MARSHALLINGVALIDATEDRESPONSE
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	handler.HandlerValidate(w, req)

	resp := w.Result()

	r := &responseError{}
	decoder := json.NewDecoder(resp.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(r)
	defer func() {
		err := resp.Body.Close()
		assert.NoError(t, err)
	}()

	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.GreaterOrEqual(t, len(r.ValidationErrors), 1)
}
func TestHandlerHealthz(t *testing.T) {
	logcfg := logger.DefaultConfig()
	log, _ := logger.New(logcfg)

	handler := NewHttpValidateHandler(log, validation.NewContentConfiguration())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	handler.HandlerHealthz(w, req)

	resp := w.Result()
	defer func() {
		err := resp.Body.Close()
		assert.NoError(t, err)
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	assert.Nil(t, err)
	assert.Equal(t, "OK", string(body))
}

func TestHandlerHealthz_Error(t *testing.T) {
	logcfg := logger.DefaultConfig()
	log, _ := logger.New(logcfg)

	handler := NewHttpValidateHandler(log, validation.NewContentConfiguration())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := mocks.NewResponseWriter(t)
	w.EXPECT().WriteHeader(mock.Anything)
	w.EXPECT().Write([]byte("OK")).Return(0, errors.New("error"))

	handler.HandlerHealthz(w, req)

	w.AssertCalled(t, "Write", []byte("OK"))
}

func TestValidation_Error2(t *testing.T) {

	logcfg := logger.DefaultConfig()
	log, _ := logger.New(logcfg)

	mockValidator := mocks.NewExtensionConfiguration(t)
	// merr := &multierror.Error{Errors: []error{errors.New("error")}}
	// mockValidator.On("Validate", mock.Anything, mock.Anything).Return("", merr)
	handler := NewHttpValidateHandler(log, mockValidator)

	// handler := NewHttpValidateHandler(log, validation.NewContentConfiguration())

	reqBody := ERROR_INVALID_JSON_CONTENT3
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewBufferString(reqBody))
	w := mocks.NewResponseWriter(t)
	w.EXPECT().WriteHeader(mock.Anything)
	w.EXPECT().Write(mock.Anything).Return(0, errors.New("error"))

	handler.HandlerValidate(w, req)

	w.AssertNumberOfCalls(t, "WriteHeader", 1)
	w.AssertNumberOfCalls(t, "Write", 1)
}

func TestHandlerValidate_WriteErrorOnValidationErrorResponse(t *testing.T) {
	logcfg := logger.DefaultConfig()
	log, _ := logger.New(logcfg)

	mockValidator := mocks.NewExtensionConfiguration(t)
	merr := &multierror.Error{Errors: []error{errors.New("validation error")}}
	mockValidator.On("Validate", mock.Anything, mock.Anything).Return("", merr)
	handler := NewHttpValidateHandler(log, mockValidator)

	w := mocks.NewResponseWriter(t)
	w.EXPECT().Header().Return(http.Header{})
	w.EXPECT().WriteHeader(http.StatusOK)
	w.EXPECT().Write(mock.Anything).Return(0, errors.New("simulated write error"))

	reqBody := OK_VALID_JSON_CONTENT
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewBufferString(reqBody))

	handler.HandlerValidate(w, req)

	w.AssertCalled(t, "Write", mock.Anything)
	w.AssertCalled(t, "WriteHeader", http.StatusOK)
}
