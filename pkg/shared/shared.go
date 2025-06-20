package shared

import "github.com/TheDevtop/quicktable/pkg/shared/core"

// Generic report form
type Report[T any] struct {
	Failed bool   `json:"failed"`
	Mesg   string `json:"mesg"`
	Data   T      `json:"data"`
}

// Regular form
type Form = struct {
	Keys   core.List `json:"keys"`
	Values core.List `json:"values"`
}

// Keys form
type KeyForm = struct {
	Keys core.List `json:"keys"`
}

// Ping signature
const Signature = "quicktable:pong"

// API routes
const (
	RoutePing    = "/ping"
	RouteMetrics = "/metrics"

	RouteIndex        = "/api/index"
	RouteIndexRanged  = "/api/index/ranged"
	RouteInsert       = "/api/insert"
	RouteInsertRanged = "/api/insert/ranged"
	RouteAppend       = "/api/append"
	RouteCopy         = "/api/copy"
	RouteMove         = "/api/move"
	RouteDelete       = "/api/delete"
	RouteDeleteRanged = "/api/delete/ranged"
	RouteQuery        = "/api/query"
	RouteQueryRanged  = "/api/query/ranged"
	RouteGenerateId   = "/api/generate/id"
	RouteGenerateHash = "/api/generate/hash"
	RouteGenerateKey  = "/api/generate/key"
	RouteGenerateList = "/api/generate/list"
)
