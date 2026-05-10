package service

import "net/http"

// ServiceError is a typed error that carries HTTP status code and an API error code.
// Handlers can unwrap this to restore the correct HTTP response semantics
// without leaking service-layer details into the transport layer.
type ServiceError struct {
	HTTPStatus int    // HTTP status code (e.g. 404, 400, 500)
	Code       string // Machine-readable error code (e.g. "provider_not_found")
	Message    string // Human-readable message
}

func (e *ServiceError) Error() string {
	return e.Message
}

// NewServiceError creates a new ServiceError.
func NewServiceError(httpStatus int, code, message string) *ServiceError {
	return &ServiceError{
		HTTPStatus: httpStatus,
		Code:       code,
		Message:    message,
	}
}

// Convenience constructors for common error classes

func ErrBadRequest(code, message string) *ServiceError {
	return NewServiceError(http.StatusBadRequest, code, message)
}

func ErrNotFound(code, message string) *ServiceError {
	return NewServiceError(http.StatusNotFound, code, message)
}

func ErrInternal(code, message string) *ServiceError {
	return NewServiceError(http.StatusInternalServerError, code, message)
}

func ErrConflict(code, message string) *ServiceError {
	return NewServiceError(http.StatusConflict, code, message)
}
