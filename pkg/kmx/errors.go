package kmx

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	CodeInvalid        ErrorCode = "invalid"
	CodeUnsupported    ErrorCode = "unsupported"
	CodeConflict       ErrorCode = "conflict"
	CodeNotFound       ErrorCode = "not_found"
	CodeOutcomeUnknown ErrorCode = "outcome_unknown"
)

type CodedError interface {
	error
	Code() ErrorCode
}

type InvalidError struct {
	Field  string
	Reason string
}

func (e *InvalidError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Reason)
}

func (e *InvalidError) Code() ErrorCode { return CodeInvalid }

type UnsupportedError struct {
	Operation  string
	Capability Capability
}

func (e *UnsupportedError) Error() string {
	if e.Capability == "" {
		return fmt.Sprintf("%s is unsupported", e.Operation)
	}
	return fmt.Sprintf("%s requires unsupported capability %q", e.Operation, e.Capability)
}

func (e *UnsupportedError) Code() ErrorCode { return CodeUnsupported }

type ConflictError struct {
	Field  string
	Reason string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("conflict on %s: %s", e.Field, e.Reason)
}

func (e *ConflictError) Code() ErrorCode { return CodeConflict }

type NotFoundError struct {
	Subject string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s was not found", e.Subject)
}

func (e *NotFoundError) Code() ErrorCode { return CodeNotFound }

type OutcomeUnknownError struct {
	Operation string
	Reason    string
}

func (e *OutcomeUnknownError) Error() string {
	return fmt.Sprintf("%s outcome is unknown: %s", e.Operation, e.Reason)
}

func (e *OutcomeUnknownError) Code() ErrorCode { return CodeOutcomeUnknown }

func IsCode(err error, code ErrorCode) bool {
	var coded CodedError
	return errors.As(err, &coded) && coded.Code() == code
}
