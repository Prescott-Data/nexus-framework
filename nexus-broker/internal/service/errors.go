package service

import "net/http"

// ServiceError is a typed error that carries HTTP status code and an API error code.
// Handlers can unwrap this to restore the correct HTTP response semantics
// without leaking service-layer details into the transport layer.
type ServiceError struct {
	HTTPStatus int    // HTTP status code (e.g. 404, 400, 500)
	Code       string // Machine-readable error code (e.g. "provider_not_found")
	Message    string // Human-readable message
	Err        error  // Underlying cause
}

func (e *ServiceError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *ServiceError) Unwrap() error {
	return e.Err
}

// NewServiceError creates a new ServiceError.
func NewServiceError(httpStatus int, err error, code, message string) *ServiceError {
	return &ServiceError{
		HTTPStatus: httpStatus,
		Code:       code,
		Message:    message,
		Err:        err,
	}
}

// Convenience constructors for common error classes

func ErrBadRequest(code, message string) *ServiceError {
	return NewServiceError(http.StatusBadRequest, nil, code, message)
}

func ErrBadRequestWithErr(err error, code, message string) *ServiceError {
	return NewServiceError(http.StatusBadRequest, err, code, message)
}

func ErrNotFound(code, message string) *ServiceError {
	return NewServiceError(http.StatusNotFound, nil, code, message)
}

func ErrNotFoundWithErr(err error, code, message string) *ServiceError {
	return NewServiceError(http.StatusNotFound, err, code, message)
}

func ErrInternal(code, message string) *ServiceError {
	return NewServiceError(http.StatusInternalServerError, nil, code, message)
}

func ErrInternalWithErr(err error, code, message string) *ServiceError {
	return NewServiceError(http.StatusInternalServerError, err, code, message)
}

func ErrConflict(code, message string) *ServiceError {
	return NewServiceError(http.StatusConflict, nil, code, message)
}

// ErrBadGatewayWithErr is for failures reaching an upstream provider (e.g. the
// broker cannot reach a provider's API to validate a credential). Status 502 so
// the cause is logged by writeServiceError and clients can distinguish it from
// a genuine credential rejection.
func ErrBadGatewayWithErr(err error, code, message string) *ServiceError {
	return NewServiceError(http.StatusBadGateway, err, code, message)
}
