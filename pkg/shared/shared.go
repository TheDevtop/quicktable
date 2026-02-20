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

// Function names
const (
	FnIndex          = "index"
	FnIndexPrefixed  = "index_prefixed"
	FnInsert         = "insert"
	FnInsertSelected = "insert_selected"
	FnCopy           = "copy"
	FnMove           = "move"
	FnDelete         = "delete"
	FnDeleteSelected = "delete_selected"
	FnDeletePrefixed = "delete_prefixed"
	FnQuery          = "query"
	FnQuerySelected  = "query_selected"
	FnQueryPrefixed  = "query_prefixed"
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
	ErrInvalidArgs  = errors.New("Invalid arguments specified")
	ErrInvalidCast  = errors.New("Could not cast argument to desired type")
	ErrInvalidFn    = errors.New("Invalid function specified")
	ErrNoQuicktable = errors.New("Could not connect to Quicktable")
	ErrFormNotOk    = errors.New("Form did not return OK")
)
