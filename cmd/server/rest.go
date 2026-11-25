package main

import (
	"encoding/json"
	"errors"
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

// mapQTEError converts engine errors to HTTP status codes and messages.
func writeQTEError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, qte.ErrRowNotFound), errors.Is(err, qte.ErrColumnNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, qte.ErrInvalidTable), errors.Is(err, qte.ErrInvalidRowKey),
		errors.Is(err, qte.ErrInvalidColumn), errors.Is(err, qte.ErrUnsupportedVal),
		errors.Is(err, qte.ErrNonNumericValue), errors.Is(err, qte.ErrNonBinaryValue),
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

	// Single cell operations: /api/tables/{table}/rows/{rowKey}/columns/{column}
	mux.HandleFunc("GET /api/tables/{table}/rows/{rowKey}/columns/{column}", h.handleGetCell)
	mux.HandleFunc("PUT /api/tables/{table}/rows/{rowKey}/columns/{column}", h.handlePutCell)
	mux.HandleFunc("DELETE /api/tables/{table}/rows/{rowKey}/columns/{column}", h.handleDeleteCell)

	// Single row operations: /api/tables/{table}/rows/{rowKey}
	mux.HandleFunc("GET /api/tables/{table}/rows/{rowKey}", h.handleGetRow)
	mux.HandleFunc("PUT /api/tables/{table}/rows/{rowKey}", h.handlePutRow)
	mux.HandleFunc("DELETE /api/tables/{table}/rows/{rowKey}", h.handleDeleteRow)

	// Rows collection: /api/tables/{table}/rows
	mux.HandleFunc("GET /api/tables/{table}/rows", h.handleListRows)
	mux.HandleFunc("POST /api/tables/{table}/rows", h.handleInsertRow)

	// Query table: /api/tables/{table}/query
	mux.HandleFunc("POST /api/tables/{table}/query", h.handleQuery)

	// Batch operations: /api/tables/{table}/batch
	mux.HandleFunc("POST /api/tables/{table}/batch", h.handleBatch)

	// Atomic operations: /api/tables/{table}/rows/{rowKey}/incr, append, prepend, cas
	mux.HandleFunc("POST /api/tables/{table}/rows/{rowKey}/incr", h.handleIncr)
	mux.HandleFunc("POST /api/tables/{table}/rows/{rowKey}/append", h.handleAppend)
	mux.HandleFunc("POST /api/tables/{table}/rows/{rowKey}/prepend", h.handlePrepend)
	mux.HandleFunc("POST /api/tables/{table}/rows/{rowKey}/cas", h.handleCAS)
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

// GET /api/tables/{table}/rows/{rowKey}
func (h *restHandler) handleGetRow(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	rowKey := r.PathValue("rowKey")

	row, err := h.db.GetRow(table, rowKey)
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

// QueryRequest defines query parameters for POST /api/tables/{table}/query
type QueryRequest struct {
	Prefix     string        `json:"prefix,omitempty"`
	StartKey   string        `json:"startKey,omitempty"`
	EndKey     string        `json:"endKey,omitempty"`
	SelectCols []string      `json:"select,omitempty"`
	OrderDesc  bool          `json:"orderDesc,omitempty"`
	Offset     int           `json:"offset,omitempty"`
	Limit      int           `json:"limit,omitempty"`
	Where      []PredicateIn `json:"where,omitempty"`
	CountOnly  bool          `json:"countOnly,omitempty"`
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

	qb := h.db.Query(table)
	if req.Prefix != "" {
		qb.WithPrefix(req.Prefix)
	}
	if req.StartKey != "" || req.EndKey != "" {
		qb.WithKeyRange(req.StartKey, req.EndKey)
	}
	if len(req.SelectCols) > 0 {
		qb.Select(req.SelectCols...)
	}
	if req.OrderDesc {
		qb.OrderDesc()
	}
	if req.Offset > 0 {
		qb.Offset(req.Offset)
	}
	if req.Limit > 0 {
		qb.Limit(req.Limit)
	}

	for _, p := range req.Where {
		switch p.Op {
		case qte.OpExists:
			qb.WhereHasColumn(p.Column)
		case qte.OpMissing:
			qb.WhereMissingColumn(p.Column)
		default:
			qb.Where(p.Column, p.Op, p.Value)
		}
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

// BatchRequest models POST /api/tables/{table}/batch
type BatchRequest struct {
	Action    string              `json:"action"` // "write", "get", "delete"
	RowKeys   []string            `json:"rowKeys,omitempty"`
	Mutations []qte.BatchMutation `json:"mutations,omitempty"`
}

// POST /api/tables/{table}/batch
func (h *restHandler) handleBatch(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")

	var req BatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	default:
		writeError(w, http.StatusBadRequest, "unknown batch action, must be 'write', 'get', or 'delete'")
	}
}

// POST /api/tables/{table}/rows/{rowKey}/incr
type incrReq struct {
	Column string `json:"column"`
	Delta  any    `json:"delta"` // int, int64 or float64
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
	Value  any    `json:"value"` // string or []byte
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
