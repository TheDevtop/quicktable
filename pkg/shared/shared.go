package shared

// Form types
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
