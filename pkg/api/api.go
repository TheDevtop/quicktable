package api

// Forms
type (
	FormExact struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	FormPrefix struct {
		Key   string            `json:"key"`
		Value map[string]string `json:"value"`
	}

	FormResponse[T any] struct {
		Route  string `json:"route"`
		Status int    `json:"status"`
		Data   T      `json:"data"`
	}
)

// Ping signature
const Signature = "quicktable:healthy"

// API routes
const (
	RouteHealth = "/health"

	RouteIndexExact  = "/index/exact"
	RouteIndexPrefix = "/index/prefix"

	RouteQueryExact  = "/query/exact"
	RouteQueryPrefix = "/query/prefix"

	RouteInsertExact  = "/insert/exact"
	RouteInsertPrefix = "/insert/prefix"

	RouteDeleteExact  = "/delete/exact"
	RouteDeletePrefix = "/delete/prefix"
)

// Status codes
const (
	StatusEngineError = -2
	StatusApiError    = -1
	StatusOk          = 0
)
