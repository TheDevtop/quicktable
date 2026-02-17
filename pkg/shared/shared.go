package shared

import "errors"

// API forms
type (
	FormResult[T any] = struct {
		Status int `json:"status"`
		Data   T   `json:"data"`
	}

	FormQuery = struct {
		Fn   string `json:"fn"`
		Args []any  `json:"args"`
	}
)

// Ping signature
const Signature = "quicktable:healthy"

// API routes
const (
	RouteHealth = "/api/health"
	RouteQuery  = "/api/query"
	RouteHash   = "/api/hash"
)

// Status codes
const (
	StatusEngineError = -2
	StatusApiError    = -1
	StatusOk          = 0
)

// Error messages
var (
	ErrInvalidArgs = errors.New("invalid arguments specified")
	ErrInvalidFn   = errors.New("invalid function specified")
)
