package shared

import "github.com/TheDevtop/quicktable/pkg/shared/core"

// Generic report form
type Report[T any] struct {
	Failed bool
	Mesg   string
	Data   T
}

// Input form
type Form = struct {
	Keys   core.List
	Values core.List
}

// API routes
const (
	RoutePing    = "/ping"
	RouteMetrics = "/metrics"

	RouteIndex        = "/api/index"
	RouteIndexRanged  = "/api/index/ranged"
	RouteInsert       = "/api/insert"
	RouteInsertRanged = "/api/insert/ranged"
	RouteAppend       = "/api/append"
	RouteDelete       = "/api/delete"
	RouteDeleteRanged = "/api/delete/ranged"
	RouteQuery        = "/api/query"
	RouteQueryRanged  = "/api/query/ranged"
)
