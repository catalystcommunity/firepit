package csilservices

import "github.com/catalystcommunity/firepit/api/internal/csil"

// Application error codes carried in ServiceError.code — distinct from the
// CSIL-RPC transport status (api/internal/transport/conventions.go), which
// never carries application semantics (csil/types/errors.csil's doc
// comment). This is the small, fixed enumeration that doc comment promises;
// keep it in sync with docs/OPERATING.md's error code table once that
// document exists. Deliberately not a CSIL enum, so new codes are additive.
const (
	CodeUnimplemented   uint64 = 1
	CodeValidation      uint64 = 2
	CodeUnauthenticated uint64 = 3
	CodeForbidden       uint64 = 4
	CodeNotFound        uint64 = 5
	CodeConflict        uint64 = 6
	CodeInternal        uint64 = 7
)

// AppError is the single application-level error type a service
// implementation returns for an expected failure — see the package doc
// comment (doc.go) for the full contract of when this is (and isn't) the
// right thing to return. It carries exactly the fields of the generated
// csil.ServiceError wire type.
type AppError struct {
	Code         uint64
	Message      string
	Field        string
	ResourceType string
}

// Error implements the error interface.
func (e *AppError) Error() string { return e.Message }

// ServiceError converts the AppError to the generated wire type, encoding
// the optional Field/ResourceType only when set.
func (e *AppError) ServiceError() csil.ServiceError {
	se := csil.ServiceError{Code: e.Code, Message: e.Message}
	if e.Field != "" {
		field := e.Field
		se.Field = &field
	}
	if e.ResourceType != "" {
		resourceType := e.ResourceType
		se.ResourceType = &resourceType
	}
	return se
}

// Unimplemented builds the AppError every stub in this package returns.
// op should identify the failing operation as "<Service>.<op-name>" (e.g.
// "AuthService.begin-login") so it's identifiable in logs and error
// messages without extra context.
func Unimplemented(op string) *AppError {
	return &AppError{Code: CodeUnimplemented, Message: op + " is not implemented yet"}
}

// NotFound builds an AppError for a missing (or hidden-existence) resource.
// Per csil/types/errors.csil: a resource the caller isn't permitted to know
// exists (e.g. a private board to a non-member) should also read as
// NotFound, never Forbidden — firepit doesn't leak existence.
func NotFound(resourceType, message string) *AppError {
	return &AppError{Code: CodeNotFound, Message: message, ResourceType: resourceType}
}

// Forbidden builds an AppError for an authorization denial where the
// resource's existence is not itself secret (e.g. "not a board maintainer").
func Forbidden(message string) *AppError {
	return &AppError{Code: CodeForbidden, Message: message}
}

// Unauthenticated builds an AppError for a missing/invalid session or a
// failed linkkeys verification step.
func Unauthenticated(message string) *AppError {
	return &AppError{Code: CodeUnauthenticated, Message: message}
}

// Validation builds an AppError for a single-field validation failure.
func Validation(field, message string) *AppError {
	return &AppError{Code: CodeValidation, Message: message, Field: field}
}

// Conflict builds an AppError for a conflicting-state failure (e.g.
// double-endorsing, a slug collision).
func Conflict(message string) *AppError {
	return &AppError{Code: CodeConflict, Message: message}
}

// Internal builds an AppError for an unexpected failure a caller should
// still receive as a typed ServiceError (as opposed to letting a plain
// error fall back to an opaque transport-level failure).
func Internal(message string) *AppError {
	return &AppError{Code: CodeInternal, Message: message}
}
