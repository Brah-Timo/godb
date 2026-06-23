package godb

import (
	"errors"
	"fmt"
)

// ─────────────────────────────────────────────────────────────
//  Sentinel errors
// ─────────────────────────────────────────────────────────────

// ErrRecordNotFound is returned when Find/First/Last returns no rows.
var ErrRecordNotFound = errors.New("godb: record not found")

// ErrNoCondition is returned when Update/Delete is called without a WHERE clause.
var ErrNoCondition = errors.New("godb: Update/Delete requires at least one WHERE condition — use Unscoped() to bypass")

// ErrDuplicateKey is returned when a unique constraint is violated.
var ErrDuplicateKey = errors.New("godb: duplicate key violation")

// ErrInvalidModel is returned when a non-struct value is passed to Model().
var ErrInvalidModel = errors.New("godb: invalid model — must be a pointer to struct")

// ErrClosed is returned when a method is called on a closed *DB.
var ErrClosed = errors.New("godb: database connection is closed")

// ─────────────────────────────────────────────────────────────
//  Error type
// ─────────────────────────────────────────────────────────────

// Error wraps an underlying database error and carries the SQL that caused it.
type Error struct {
	Code    ErrCode
	Message string
	SQL     string
	Cause   error
}

// ErrCode categorises godb errors.
type ErrCode uint8

const (
	ErrCodeUnknown      ErrCode = iota
	ErrCodeNotFound             // no rows returned
	ErrCodeDuplicate            // unique constraint violated
	ErrCodeNoCondition          // missing WHERE on write
	ErrCodeInvalidModel         // bad struct passed to Parse
	ErrCodeConnection           // network / connection issue
)

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("godb [%s]: %s (cause: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("godb [%s]: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func (c ErrCode) String() string {
	switch c {
	case ErrCodeNotFound:
		return "NOT_FOUND"
	case ErrCodeDuplicate:
		return "DUPLICATE"
	case ErrCodeNoCondition:
		return "NO_CONDITION"
	case ErrCodeInvalidModel:
		return "INVALID_MODEL"
	case ErrCodeConnection:
		return "CONNECTION"
	default:
		return "UNKNOWN"
	}
}

// ─────────────────────────────────────────────────────────────
//  Helpers
// ─────────────────────────────────────────────────────────────

// IsNotFound reports whether err represents a "record not found" error.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRecordNotFound) {
		return true
	}
	var ge *Error
	if errors.As(err, &ge) {
		return ge.Code == ErrCodeNotFound
	}
	return false
}

// IsDuplicate reports whether err represents a unique-constraint violation.
func IsDuplicate(err error) bool {
	if err == nil {
		return false
	}
	var ge *Error
	if errors.As(err, &ge) {
		return ge.Code == ErrCodeDuplicate
	}
	return errors.Is(err, ErrDuplicateKey)
}

// wrapError wraps a raw database error in an *Error with the originating SQL.
func wrapError(err error, sqlStr string) error {
	if err == nil {
		return nil
	}
	code := ErrCodeUnknown
	msg := err.Error()
	return &Error{Code: code, Message: msg, SQL: sqlStr, Cause: err}
}
