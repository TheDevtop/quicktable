package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/TheDevtop/quicktable/pkg/qte"
)

// writeJSON writes a JSON response with status code and Content-Type header.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes an error response in JSON format.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeQTEError converts engine errors to HTTP status codes and messages.
func writeQTEError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, qte.ErrRowNotFound), errors.Is(err, qte.ErrColumnNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, qte.ErrInvalidTable), errors.Is(err, qte.ErrInvalidRowKey),
		errors.Is(err, qte.ErrInvalidColumn), errors.Is(err, qte.ErrUnsupportedVal),
		errors.Is(err, qte.ErrNonNumericValue), errors.Is(err, qte.ErrNonStringValue),
		errors.Is(err, qte.ErrInvalidOp):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, qte.ErrConditionFailed):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// RegisterRESTRoutes registers all wide-column REST API handlers onto the given ServeMux.
func RegisterRESTRoutes(mux *http.ServeMux, db *qte.DB) {
	h := &restHandler{db: db}

	// Liveness: answers whenever the server is serving requests.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	// Table operations: /api/tables, /api/tables/{table}
	mux.HandleFunc("GET /api/tables", h.handleListTables)
	mux.HandleFunc("DELETE /api/tables/{table}", h.handleDropTable)
	mux.HandleFunc("GET /api/tables/{table}/columns", h.handleListColumns)

	// Single cell operations: /api/tables/{table}/rows/{rowKey}/columns/{column}
	mux.HandleFunc("GET /api/tables/{table}/rows/{rowKey}/columns/{column}", h.handleGetCell)
	mux.HandleFunc("PUT /api/tables/{table}/rows/{rowKey}/columns/{column}", h.handlePutCell)
	mux.HandleFunc("DELETE /api/tables/{table}/rows/{rowKey}/columns/{column}", h.handleDeleteCell)

	// Single row operations: /api/tables/{table}/rows/{rowKey}
	mux.HandleFunc("GET /api/tables/{table}/rows/{rowKey}", h.handleGetRow)
	mux.HandleFunc("HEAD /api/tables/{table}/rows/{rowKey}", h.handleRowExists)
	mux.HandleFunc("PUT /api/tables/{table}/rows/{rowKey}", h.handlePutRow)
	mux.HandleFunc("DELETE /api/tables/{table}/rows/{rowKey}", h.handleDeleteRow)

	// Rows collection: /api/tables/{table}/rows
	mux.HandleFunc("GET /api/tables/{table}/rows", h.handleListRows)
	mux.HandleFunc("POST /api/tables/{table}/rows", h.handleInsertRow)

	// Query table: /api/tables/{table}/query
	mux.HandleFunc("POST /api/tables/{table}/query", h.handleQuery)
	mux.HandleFunc("POST /api/tables/{table}/query/delete", h.handleQueryDelete)

	// Batch operations: /api/tables/{table}/batch
	mux.HandleFunc("POST /api/tables/{table}/batch", h.handleBatch)

	// Atomic operations: /api/tables/{table}/rows/{rowKey}/incr, append, prepend, cas, cad
	mux.HandleFunc("POST /api/tables/{table}/rows/{rowKey}/incr", h.handleIncr)
	mux.HandleFunc("POST /api/tables/{table}/rows/{rowKey}/append", h.handleAppend)
	mux.HandleFunc("POST /api/tables/{table}/rows/{rowKey}/prepend", h.handlePrepend)
	mux.HandleFunc("POST /api/tables/{table}/rows/{rowKey}/cas", h.handleCAS)
	mux.HandleFunc("POST /api/tables/{table}/rows/{rowKey}/cad", h.handleCAD)
}

type restHandler struct {
	db *qte.DB
}

// GET /api/tables
func (h *restHandler) handleListTables(w http.ResponseWriter, r *http.Request) {
	tables, err := h.db.ListTables()
	if err != nil {
		writeQTEError(w, err)
		return
	}
	if tables == nil {
		tables = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tables": tables})
}

// DELETE /api/tables/{table}
func (h *restHandler) handleDropTable(w http.ResponseWriter, r *http.Request) {
	if err := h.db.DropTable(r.PathValue("table")); err != nil {
		writeQTEError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// GET /api/tables/{table}/columns
func (h *restHandler) handleListColumns(w http.ResponseWriter, r *http.Request) {
	cols, err := h.db.ListColumns(r.PathValue("table"))
	if err != nil {
		writeQTEError(w, err)
		return
	}
	if cols == nil {
		cols = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"columns": cols})
}

// GET /api/tables/{table}/rows
func (h *restHandler) handleListRows(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.ListRows(r.PathValue("table"))
	if err != nil {
		writeQTEError(w, err)
		return
	}
	if rows == nil {
		rows = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}

// GET /api/tables/{table}/rows/{rowKey}/columns/{column}
func (h *restHandler) handleGetCell(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	rowKey := r.PathValue("rowKey")
	column := r.PathValue("column")

	val, err := h.db.GetCell(table, rowKey, column)
	if err != nil {
		writeQTEError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"table":  table,
		"rowKey": rowKey,
		"column": column,
		"value":  val,
	})
}

// PUT /api/tables/{table}/rows/{rowKey}/columns/{column}
type putCellReq struct {
	Value any `json:"value"`
}

func (h *restHandler) handlePutCell(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	rowKey := r.PathValue("rowKey")
	column := r.PathValue("column")

	var req putCellReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json request body: "+err.Error())
		return
	}

	if err := h.db.SetCell(table, rowKey, column, req.Value); err != nil {
		writeQTEError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// DELETE /api/tables/{table}/rows/{rowKey}/columns/{column}
func (h *restHandler) handleDeleteCell(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	rowKey := r.PathValue("rowKey")
	column := r.PathValue("column")

	if err := h.db.DeleteCell(table, rowKey, column); err != nil {
		writeQTEError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// GET /api/tables/{table}/rows/{rowKey}[?column=a&column=b]
func (h *restHandler) handleGetRow(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	rowKey := r.PathValue("rowKey")

	var row qte.Row
	var err error
	if cols := r.URL.Query()["column"]; len(cols) > 0 {
		row, err = h.db.GetCells(table, rowKey, cols)
	} else {
		row, err = h.db.GetRow(table, rowKey)
	}
	if err != nil {
		writeQTEError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"table":   table,
		"rowKey":  rowKey,
		"columns": row,
	})
}

// HEAD /api/tables/{table}/rows/{rowKey}
func (h *restHandler) handleRowExists(w http.ResponseWriter, r *http.Request) {
	ok, err := h.db.RowExists(r.PathValue("table"), r.PathValue("rowKey"))
	switch {
	case errors.Is(err, qte.ErrInvalidTable), errors.Is(err, qte.ErrInvalidRowKey):
		w.WriteHeader(http.StatusBadRequest)
	case err != nil:
		w.WriteHeader(http.StatusInternalServerError)
	case !ok:
		w.WriteHeader(http.StatusNotFound)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

// PUT /api/tables/{table}/rows/{rowKey}
func (h *restHandler) handlePutRow(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	rowKey := r.PathValue("rowKey")

	var row qte.Row
	if err := json.NewDecoder(r.Body).Decode(&row); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json row body: "+err.Error())
		return
	}

	if err := h.db.SetRow(table, rowKey, row); err != nil {
		writeQTEError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// DELETE /api/tables/{table}/rows/{rowKey}
func (h *restHandler) handleDeleteRow(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	rowKey := r.PathValue("rowKey")

	if err := h.db.DeleteRow(table, rowKey); err != nil {
		writeQTEError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// POST /api/tables/{table}/rows
func (h *restHandler) handleInsertRow(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")

	var row qte.Row
	if err := json.NewDecoder(r.Body).Decode(&row); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json row body: "+err.Error())
		return
	}

	rowKey, err := h.db.InsertRow(table, row)
	if err != nil {
		writeQTEError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"table":  table,
		"rowKey": rowKey,
	})
}

// QueryFilter selects the rows of a query or a delete by query.
type QueryFilter struct {
	Prefix    string          `json:"prefix,omitempty"`
	StartKey  string          `json:"startKey,omitempty"`
	EndKey    string          `json:"endKey,omitempty"`
	OrderDesc bool            `json:"orderDesc,omitempty"`
	Offset    int             `json:"offset,omitempty"`
	Limit     int             `json:"limit,omitempty"`
	Where     []PredicateIn   `json:"where,omitempty"`
	Or        [][]PredicateIn `json:"or,omitempty"`
}

// QueryRequest defines query parameters for POST /api/tables/{table}/query
type QueryRequest struct {
	QueryFilter
	SelectCols []string `json:"select,omitempty"`
	CountOnly  bool     `json:"countOnly,omitempty"`
	Aggregate  string   `json:"aggregate,omitempty"`
}

type PredicateIn struct {
	Column string        `json:"column"`
	Op     qte.CompareOp `json:"op"`
	Value  any           `json:"value,omitempty"`
}

// POST /api/tables/{table}/query
func (h *restHandler) handleQuery(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")

	var req QueryRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json query body: "+err.Error())
			return
		}
	}

	if req.CountOnly && req.Aggregate != "" {
		writeError(w, http.StatusBadRequest, "countOnly and aggregate cannot be combined")
		return
	}
	qb, err := h.buildQuery(table, req.QueryFilter)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	qb.Select(req.SelectCols...)

	if req.Aggregate != "" {
		stats, err := qb.Aggregate(req.Aggregate)
		if err != nil {
			writeQTEError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"table":  table,
			"column": req.Aggregate,
			"stats":  stats,
		})
		return
	}

	if req.CountOnly {
		cnt, err := qb.Count()
		if err != nil {
			writeQTEError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": cnt})
		return
	}

	rows, err := qb.Execute()
	if err != nil {
		writeQTEError(w, err)
		return
	}
	if rows == nil {
		rows = []qte.QueryRow{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"table": table,
		"rows":  rows,
		"count": len(rows),
	})
}

// POST /api/tables/{table}/query/delete
func (h *restHandler) handleQueryDelete(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")

	// A body is required and unknown fields are rejected: a misspelled "where" must not delete every row.
	var req QueryFilter
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json delete body: "+err.Error())
		return
	}

	qb, err := h.buildQuery(table, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := qb.Delete()
	if err != nil {
		writeQTEError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"table": table, "deleted": n})
}

// buildQuery applies a filter to a new query on table.
// The predicate groups of "or" are alternatives, so they cannot be combined with "where".
func (h *restHandler) buildQuery(table string, f QueryFilter) (*qte.QueryBuilder, error) {
	if len(f.Where) > 0 && len(f.Or) > 0 {
		return nil, errors.New("where and or cannot be combined")
	}

	qb := h.db.Query(table).
		WithPrefix(f.Prefix).
		WithKeyRange(f.StartKey, f.EndKey).
		Offset(f.Offset).
		Limit(f.Limit)
	if f.OrderDesc {
		qb.OrderDesc()
	}
	for _, p := range f.Where {
		qb.Where(p.Column, p.Op, p.Value)
	}
	for i, g := range f.Or {
		if len(g) == 0 {
			return nil, fmt.Errorf("or group %d is empty", i)
		}
		qb.Or()
		for _, p := range g {
			qb.Where(p.Column, p.Op, p.Value)
		}
	}
	return qb, nil
}

// BatchRequest models POST /api/tables/{table}/batch
type BatchRequest struct {
	Action    string              `json:"action"` // "write", "get", "delete", "insert"
	RowKeys   []string            `json:"rowKeys,omitempty"`
	Mutations []qte.BatchMutation `json:"mutations,omitempty"`
	Rows      []qte.Row           `json:"rows,omitempty"`
}

// POST /api/tables/{table}/batch
func (h *restHandler) handleBatch(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")

	// Unknown fields are rejected: a misspelled "setCols" must not be silently dropped.
	var req BatchRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json batch body: "+err.Error())
		return
	}

	switch strings.ToLower(req.Action) {
	case "write":
		if err := h.db.BatchWrite(table, req.Mutations); err != nil {
			writeQTEError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	case "get":
		rows, err := h.db.BatchGetRows(table, req.RowKeys)
		if err != nil {
			writeQTEError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"table": table,
			"rows":  rows,
		})
	case "delete":
		if err := h.db.BatchDeleteRows(table, req.RowKeys); err != nil {
			writeQTEError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	case "insert":
		keys, err := h.db.BatchInsert(table, req.Rows)
		if err != nil {
			writeQTEError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"table":   table,
			"rowKeys": keys,
		})
	default:
		writeError(w, http.StatusBadRequest, "unknown batch action, must be 'write', 'get', 'delete' or 'insert'")
	}
}

// POST /api/tables/{table}/rows/{rowKey}/incr
type incrReq struct {
	Column string `json:"column"`
	Delta  any    `json:"delta"`
}

func (h *restHandler) handleIncr(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	rowKey := r.PathValue("rowKey")

	var req incrReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json incr body: "+err.Error())
		return
	}

	delta, ok := req.Delta.(float64)
	if !ok {
		writeError(w, http.StatusBadRequest, "delta must be numeric")
		return
	}

	newVal, err := h.db.Incr(table, rowKey, req.Column, delta)
	if err != nil {
		writeQTEError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"value": newVal})
}

// POST /api/tables/{table}/rows/{rowKey}/append
type stringModReq struct {
	Column string `json:"column"`
	Value  string `json:"value"`
}

func (h *restHandler) handleAppend(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	rowKey := r.PathValue("rowKey")

	var req stringModReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json append body: "+err.Error())
		return
	}

	newVal, err := h.db.AppendCell(table, rowKey, req.Column, req.Value)
	if err != nil {
		writeQTEError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"value": newVal})
}

// POST /api/tables/{table}/rows/{rowKey}/prepend
func (h *restHandler) handlePrepend(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	rowKey := r.PathValue("rowKey")

	var req stringModReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json prepend body: "+err.Error())
		return
	}

	newVal, err := h.db.PrependCell(table, rowKey, req.Column, req.Value)
	if err != nil {
		writeQTEError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"value": newVal})
}

// POST /api/tables/{table}/rows/{rowKey}/cas
type casReq struct {
	Condition *qte.Condition `json:"condition,omitempty"`
	SetCols   qte.Row        `json:"setCols"`
}

func (h *restHandler) handleCAS(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	rowKey := r.PathValue("rowKey")

	// Unknown fields are rejected: a misspelled "condition" must not become an unconditional write.
	var req casReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json cas body: "+err.Error())
		return
	}

	if err := h.db.CheckAndSet(table, rowKey, req.Condition, req.SetCols); err != nil {
		writeQTEError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// POST /api/tables/{table}/rows/{rowKey}/cad
type cadReq struct {
	Condition *qte.Condition `json:"condition,omitempty"`
	DelCols   []string       `json:"delCols,omitempty"`
}

func (h *restHandler) handleCAD(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	rowKey := r.PathValue("rowKey")

	// Unknown fields are rejected: a misspelled "delCols" must not delete the whole row.
	var req cadReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json cad body: "+err.Error())
		return
	}

	if err := h.db.CheckAndDelete(table, rowKey, req.Condition, req.DelCols); err != nil {
		writeQTEError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
