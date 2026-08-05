// Package csilservices contains the hand-written implementations of the
// generated CSIL service interfaces.
//
// # Application errors vs. transport errors
//
// Every generated service method has the shape:
//
//	Method(ctx context.Context, req Req) (Resp, error)
//
// The CSIL declaration determines how the dispatcher handles an error. An
// operation with a `/ ServiceError` arm can return an *AppError. The caller
// then receives a typed ServiceError response. Use an AppError for expected
// failures such as validation, authorization, conflicts, and missing data.
//
// An operation without a declared error arm cannot return a typed failure.
// The dispatcher maps any error from that operation to an internal transport
// error. Such operations must use normal empty responses for expected empty
// results.
//
// The dispatcher also maps plain Go errors to internal transport errors. It
// does not expose their details to the caller.
package csilservices
