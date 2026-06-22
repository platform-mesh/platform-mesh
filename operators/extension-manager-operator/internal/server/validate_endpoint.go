package server

import (
	"encoding/json"
	"net/http"

	"go.platform-mesh.io/golang-commons/logger"

	"go.platform-mesh.io/extension-manager-operator/pkg/validation"

	"go.platform-mesh.io/golang-commons/sentry"
)

type requestValidate struct {
	ContentType          string `json:"contentType,omitempty"`
	ContentConfiguration string `json:"contentConfiguration"`
}

type Response struct {
	ParsedConfiguration string            `json:"parsedConfiguration,omitempty"`
	ValidationErrors    []validationError `json:"validationErrors,omitempty"`
}

type validationError struct {
	Message string `json:"message"`
}

func NewHttpValidateHandler(log *logger.Logger, validator validation.ExtensionConfiguration) *HttpValidateHandler {
	return &HttpValidateHandler{
		validator: validator,
		log:       log,
	}
}

type HttpValidateHandler struct {
	validator validation.ExtensionConfiguration
	log       *logger.Logger
}

func (h *HttpValidateHandler) HandlerHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte("OK"))
	if err != nil {
		h.log.Error().Err(err).Msg("Writing response failed")
		sentry.CaptureError(err, sentry.Tags{"error": "Writing response failed"})
	}
}

func (h *HttpValidateHandler) writeErrorHelper(w http.ResponseWriter, errcode int, err error) (int, error) {
	w.WriteHeader(errcode)
	bytes, errWrite := w.Write([]byte(err.Error()))
	if errWrite != nil {
		return 0, errWrite
	}
	return bytes, nil
}

func (h *HttpValidateHandler) HandlerValidate(w http.ResponseWriter, r *http.Request) {
	// decode request
	var request requestValidate
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&request)
	defer func() {
		err := r.Body.Close()
		if err != nil {
			h.log.Error().Err(err).Msg("Closing request body failed")
			sentry.CaptureError(err, sentry.Tags{"error": "Closing request body failed"})
		}
	}()

	if err != nil {
		_, errResponse := h.writeErrorHelper(w, http.StatusInternalServerError, err)
		if errResponse != nil {
			h.log.Error().Err(errResponse).Msg("Writing response failed")
			sentry.CaptureError(errResponse, sentry.Tags{"error": "Writing 'StatusInternalServerError' response failed"})
		}
		return
	}

	// validation
	parsedConfig, merr := h.validator.Validate([]byte(request.ContentConfiguration), request.ContentType)
	if merr != nil && merr.Len() > 0 {
		var responseErr Response
		for _, e := range merr.Errors {
			responseErr.ValidationErrors = append(responseErr.ValidationErrors, validationError{
				Message: e.Error(),
			})
		}

		responseBytes, _ := json.Marshal(responseErr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write(responseBytes)
		if err != nil {
			h.log.Error().Err(err).Msg("Writing response failed")
			sentry.CaptureError(err, sentry.Tags{"error": "Writing response failed"}, sentry.Extras{"data": responseErr})
		}
		return
	}

	// send response
	var rValid Response
	rValid.ParsedConfiguration = parsedConfig
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(&rValid)
	if err != nil {
		h.log.Error().Err(err).Msg("Writing response failed")
		sentry.CaptureError(err, sentry.Tags{"error": "Writing response failed"}, sentry.Extras{"data": rValid})
	}
}
