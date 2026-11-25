package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/TheDevtop/quicktable/pkg/qte"
)

// MCPHandler integrates the MCP server into a standard http.ServeMux using SSE transport.
type MCPHandler struct {
	mcpServer *server.MCPServer
	sessions  sync.Map
}

// sseClient is one open SSE stream. Writes from concurrent POSTs are
// serialised by mu, and closed stops writes once the stream handler returns.
type sseClient struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
	closed  bool
	done    chan struct{}
	once    sync.Once
}

// send writes one SSE event, reporting false if the stream is gone.
func (c *sseClient) send(event string, data []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	fmt.Fprintf(c.w, "event: %s\ndata: %s\r\n\r\n", event, data)
	c.flusher.Flush()
	return true
}

func (c *sseClient) stop() {
	c.once.Do(func() { close(c.done) })
}

// RegisterMCPRoutes creates and registers the MCP server endpoints on mux.
// Endpoints:
//
//	GET  /mcp/sse     - SSE stream endpoint for clients
//	POST /mcp/message - JSON-RPC message endpoint
func RegisterMCPRoutes(mux *http.ServeMux, db *qte.DB) *MCPHandler {
	s := server.NewMCPServer("quicktable-mcp", "0.1.0")
	registerMCPTools(s, db)

	h := &MCPHandler{
		mcpServer: s,
	}

	mux.HandleFunc("GET /mcp/sse", h.handleSSE)
	mux.HandleFunc("POST /mcp/message", h.handleMessage)

	return h
}

func (h *MCPHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// The stream outlives the server's WriteTimeout, so lift the deadline for it.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	sessionID := uuid.New().String()
	client := &sseClient{
		w:       w,
		flusher: flusher,
		done:    make(chan struct{}),
	}

	h.sessions.Store(sessionID, client)
	defer func() {
		h.sessions.Delete(sessionID)
		client.mu.Lock()
		client.closed = true
		client.mu.Unlock()
	}()

	// Send endpoint notification so client knows where to POST JSON-RPC messages
	client.send("endpoint", []byte("/mcp/message?sessionId="+sessionID))

	select {
	case <-client.done:
	case <-r.Context().Done():
	}
}

func (h *MCPHandler) handleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "Missing sessionId parameter", http.StatusBadRequest)
		return
	}

	clientVal, ok := h.sessions.Load(sessionID)
	if !ok {
		http.Error(w, "Invalid or expired session ID", http.StatusBadRequest)
		return
	}
	client := clientVal.(*sseClient)

	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "Invalid JSON-RPC request", http.StatusBadRequest)
		return
	}

	// Responses travel only over the SSE stream; the POST is just acknowledged.
	resp := h.mcpServer.HandleMessage(r.Context(), raw)
	if resp != nil {
		data, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
		if !client.send("message", data) {
			http.Error(w, "Invalid or expired session ID", http.StatusBadRequest)
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

// Close ends every open SSE stream so their handlers return.
func (h *MCPHandler) Close() {
	h.sessions.Range(func(key, value any) bool {
		value.(*sseClient).stop()
		return true
	})
}

// registerMCPTools defines all wide-column engine and query tools.
func registerMCPTools(s *server.MCPServer, db *qte.DB) {
	// 1. qte_get_cell
	s.AddTool(
		mcp.NewTool("qte_get_cell",
			mcp.WithDescription("Get a cell value from a wide-column table by table, rowKey, and column."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
			mcp.WithString("column", mcp.Required(), mcp.Description("Column name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rowKey, _ := req.GetArguments()["rowKey"].(string)
			col, _ := req.GetArguments()["column"].(string)

			val, err := db.GetCell(table, rowKey, col)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			out, _ := json.Marshal(val)
			return mcp.NewToolResultText(string(out)), nil
		},
	)

	// 2. qte_set_cell
	s.AddTool(
		mcp.NewTool("qte_set_cell",
			mcp.WithDescription("Set a cell scalar value in a wide-column table."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
			mcp.WithString("column", mcp.Required(), mcp.Description("Column name")),
			mcp.WithAny("value", mcp.Required(), mcp.Description("Scalar value: number, string or boolean")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rowKey, _ := req.GetArguments()["rowKey"].(string)
			col, _ := req.GetArguments()["column"].(string)
			val := req.GetArguments()["value"]

			if err := db.SetCell(table, rowKey, col, val); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(`{"status":"ok"}`), nil
		},
	)

	// 3. qte_delete_cell
	s.AddTool(
		mcp.NewTool("qte_delete_cell",
			mcp.WithDescription("Delete a single cell from a row."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
			mcp.WithString("column", mcp.Required(), mcp.Description("Column name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rowKey, _ := req.GetArguments()["rowKey"].(string)
			col, _ := req.GetArguments()["column"].(string)

			if err := db.DeleteCell(table, rowKey, col); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(`{"status":"ok"}`), nil
		},
	)

	// 4. qte_get_row
	s.AddTool(
		mcp.NewTool("qte_get_row",
			mcp.WithDescription("Retrieve all columns for a row."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rowKey, _ := req.GetArguments()["rowKey"].(string)

			row, err := db.GetRow(table, rowKey)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			out, _ := json.Marshal(row)
			return mcp.NewToolResultText(string(out)), nil
		},
	)

	// 5. qte_set_row
	s.AddTool(
		mcp.NewTool("qte_set_row",
			mcp.WithDescription("Set or overwrite multiple columns in a row."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
			mcp.WithObject("columns", mcp.Required(), mcp.Description("Column name to scalar value pairs")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rowKey, _ := req.GetArguments()["rowKey"].(string)
			cols, ok := req.GetArguments()["columns"].(map[string]any)
			if !ok {
				return mcp.NewToolResultError("columns must be an object of key/value pairs"), nil
			}

			if err := db.SetRow(table, rowKey, cols); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(`{"status":"ok"}`), nil
		},
	)

	// 6. qte_insert_row
	s.AddTool(
		mcp.NewTool("qte_insert_row",
			mcp.WithDescription("Insert a new row with auto-generated 8-digit hex key."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithObject("columns", mcp.Description("Column name to scalar value pairs")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			cols, ok := req.GetArguments()["columns"].(map[string]any)
			if !ok {
				cols = make(map[string]any)
			}

			rowKey, err := db.InsertRow(table, cols)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			res, _ := json.Marshal(map[string]string{"rowKey": rowKey})
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 7. qte_delete_row
	s.AddTool(
		mcp.NewTool("qte_delete_row",
			mcp.WithDescription("Delete an entire row and all its columns."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rowKey, _ := req.GetArguments()["rowKey"].(string)

			if err := db.DeleteRow(table, rowKey); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(`{"status":"ok"}`), nil
		},
	)

	// 8. qte_query
	s.AddTool(
		mcp.NewTool("qte_query",
			mcp.WithDescription("Query, scan, filter and project rows in a table."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("prefix", mcp.Description("Filter by row key prefix")),
			mcp.WithString("startKey", mcp.Description("Start range key")),
			mcp.WithString("endKey", mcp.Description("End range key")),
			mcp.WithBoolean("orderDesc", mcp.Description("Sort row keys in descending order")),
			mcp.WithNumber("offset", mcp.Description("Skip first n rows")),
			mcp.WithNumber("limit", mcp.Description("Cap maximum rows returned")),
			mcp.WithBoolean("countOnly", mcp.Description("Return only count of matching rows")),
			mcp.WithArray("select", mcp.WithStringItems(), mcp.Description("Columns to return; all columns if omitted")),
			mcp.WithArray("where", mcp.Description(`Predicates that must all hold, each {"column": name, "op": op, "value": v}. `+
				`op is one of =, !=, >, >=, <, <=, CONTAINS, PREFIX, EXISTS, MISSING; EXISTS and MISSING take no value`),
				mcp.Items(map[string]any{
					"type":       "object",
					"properties": predicateProps(),
					"required":   []string{"column", "op"},
				})),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			prefix, _ := req.GetArguments()["prefix"].(string)
			startKey, _ := req.GetArguments()["startKey"].(string)
			endKey, _ := req.GetArguments()["endKey"].(string)
			orderDesc, _ := req.GetArguments()["orderDesc"].(bool)
			offset, _ := req.GetArguments()["offset"].(float64)
			limit, _ := req.GetArguments()["limit"].(float64)
			countOnly, _ := req.GetArguments()["countOnly"].(bool)

			qb := db.Query(table)
			if prefix != "" {
				qb.WithPrefix(prefix)
			}
			if startKey != "" || endKey != "" {
				qb.WithKeyRange(startKey, endKey)
			}
			if orderDesc {
				qb.OrderDesc()
			}
			if offset > 0 {
				qb.Offset(int(offset))
			}
			if limit > 0 {
				qb.Limit(int(limit))
			}

			if raw, ok := req.GetArguments()["select"]; ok {
				cols, ok := raw.([]any)
				if !ok {
					return mcp.NewToolResultError("select must be an array of column names"), nil
				}
				for _, c := range cols {
					s, ok := c.(string)
					if !ok {
						return mcp.NewToolResultError("select must be an array of column names"), nil
					}
					qb.Select(s)
				}
			}

			if raw, ok := req.GetArguments()["where"]; ok {
				preds, ok := raw.([]any)
				if !ok {
					return mcp.NewToolResultError("where must be an array of predicates"), nil
				}
				for _, p := range preds {
					m, ok := p.(map[string]any)
					if !ok {
						return mcp.NewToolResultError("each where predicate must be an object"), nil
					}
					col, _ := m["column"].(string)
					op, _ := m["op"].(string)
					qb.Where(col, qte.CompareOp(op), m["value"])
				}
			}

			if countOnly {
				cnt, err := qb.Count()
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				res, _ := json.Marshal(map[string]int{"count": cnt})
				return mcp.NewToolResultText(string(res)), nil
			}

			rows, err := qb.Execute()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if rows == nil {
				rows = []qte.QueryRow{}
			}
			res, _ := json.Marshal(rows)
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 9. qte_incr_cell
	s.AddTool(
		mcp.NewTool("qte_incr_cell",
			mcp.WithDescription("Atomically increment an integer or float cell value."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
			mcp.WithString("column", mcp.Required(), mcp.Description("Column name")),
			mcp.WithNumber("delta", mcp.Required(), mcp.Description("Numeric delta to add")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rowKey, _ := req.GetArguments()["rowKey"].(string)
			col, _ := req.GetArguments()["column"].(string)
			delta, _ := req.GetArguments()["delta"].(float64)

			newVal, err := db.Incr(table, rowKey, col, delta)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			res, _ := json.Marshal(map[string]any{"value": newVal})
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 10. qte_append_cell
	s.AddTool(
		mcp.NewTool("qte_append_cell",
			mcp.WithDescription("Atomically append string to an existing cell value."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
			mcp.WithString("column", mcp.Required(), mcp.Description("Column name")),
			mcp.WithString("suffix", mcp.Required(), mcp.Description("String suffix to append")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rowKey, _ := req.GetArguments()["rowKey"].(string)
			col, _ := req.GetArguments()["column"].(string)
			suffix, _ := req.GetArguments()["suffix"].(string)

			newVal, err := db.AppendCell(table, rowKey, col, suffix)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			res, _ := json.Marshal(map[string]any{"value": newVal})
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 11. qte_batch_get
	s.AddTool(
		mcp.NewTool("qte_batch_get",
			mcp.WithDescription("Retrieve multiple rows by their keys."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithArray("rowKeys", mcp.Required(), mcp.WithStringItems(), mcp.Description("8-character hex row keys")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rawKeys, ok := req.GetArguments()["rowKeys"].([]any)
			if !ok {
				return mcp.NewToolResultError("rowKeys array required"), nil
			}
			keys := make([]string, 0, len(rawKeys))
			for _, k := range rawKeys {
				s, ok := k.(string)
				if !ok {
					return mcp.NewToolResultError("rowKeys must be strings"), nil
				}
				keys = append(keys, s)
			}

			rows, err := db.BatchGetRows(table, keys)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			res, _ := json.Marshal(rows)
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 12. qte_list_tables
	s.AddTool(
		mcp.NewTool("qte_list_tables",
			mcp.WithDescription("List the names of all tables in the database."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tables, err := db.ListTables()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if tables == nil {
				tables = []string{}
			}
			res, _ := json.Marshal(tables)
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 13. qte_drop_table
	s.AddTool(
		mcp.NewTool("qte_drop_table",
			mcp.WithDescription("Permanently delete a table and all of its rows. Irreversible."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			if err := db.DropTable(table); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(`{"status":"ok"}`), nil
		},
	)

	// 14. qte_list_rows
	s.AddTool(
		mcp.NewTool("qte_list_rows",
			mcp.WithDescription("List the keys of all rows in a table, sorted."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rows, err := db.ListRows(table)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if rows == nil {
				rows = []string{}
			}
			res, _ := json.Marshal(rows)
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 15. qte_prepend_cell
	s.AddTool(
		mcp.NewTool("qte_prepend_cell",
			mcp.WithDescription("Atomically prepend a string to a string cell. Creates the cell if it does not exist."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
			mcp.WithString("column", mcp.Required(), mcp.Description("Column name")),
			mcp.WithString("prefix", mcp.Required(), mcp.Description("String to put in front")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rowKey, _ := req.GetArguments()["rowKey"].(string)
			col, _ := req.GetArguments()["column"].(string)
			prefix, _ := req.GetArguments()["prefix"].(string)

			newVal, err := db.PrependCell(table, rowKey, col, prefix)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			res, _ := json.Marshal(map[string]any{"value": newVal})
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 16. qte_cas
	s.AddTool(
		mcp.NewTool("qte_cas",
			mcp.WithDescription("Atomically check a condition on one column of a row and, if it holds, write columns. "+
				"Without a condition the write is unconditional. Unknown keys are rejected."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
			mcp.WithObject("condition", mcp.Description(`{"column": name, "op": op, "value": v}; same operators as qte_query where`),
				mcp.Properties(predicateProps()), mcp.AdditionalProperties(false)),
			mcp.WithObject("setCols", mcp.Description("Column name to scalar value pairs to write")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := req.GetArguments()
			if k := unknownKey(args, "table", "rowKey", "condition", "setCols"); k != "" {
				return mcp.NewToolResultError(fmt.Sprintf("unknown argument %q", k)), nil
			}
			table, _ := args["table"].(string)
			rowKey, _ := args["rowKey"].(string)

			var cond *qte.Condition
			if raw, ok := args["condition"]; ok {
				m, ok := raw.(map[string]any)
				if !ok {
					return mcp.NewToolResultError("condition must be an object"), nil
				}
				if k := unknownKey(m, "column", "op", "value"); k != "" {
					return mcp.NewToolResultError(fmt.Sprintf("unknown condition key %q", k)), nil
				}
				col, _ := m["column"].(string)
				op, _ := m["op"].(string)
				cond = &qte.Condition{Column: col, Op: qte.CompareOp(op), Value: m["value"]}
			}

			var setCols qte.Row
			if raw, ok := args["setCols"]; ok {
				m, ok := raw.(map[string]any)
				if !ok {
					return mcp.NewToolResultError("setCols must be an object"), nil
				}
				setCols = m
			}

			if err := db.CheckAndSet(table, rowKey, cond, setCols); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(`{"status":"ok"}`), nil
		},
	)

	// 17. qte_batch_write
	s.AddTool(
		mcp.NewTool("qte_batch_write",
			mcp.WithDescription("Apply several row mutations atomically: all succeed or none do. "+
				"Each mutation deletes the whole row, or deletes delCols and then sets setCols. Unknown keys are rejected."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithArray("mutations", mcp.Required(), mcp.Description("Row mutations"),
				mcp.Items(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"rowKey":  map[string]any{"type": "string", "description": "8-character hex row key"},
						"setCols": map[string]any{"type": "object", "description": "Column name to scalar value pairs to set"},
						"delCols": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Columns to delete"},
						"delete":  map[string]any{"type": "boolean", "description": "Delete the entire row"},
					},
					"required":             []string{"rowKey"},
					"additionalProperties": false,
				})),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			raw, ok := req.GetArguments()["mutations"].([]any)
			if !ok {
				return mcp.NewToolResultError("mutations array required"), nil
			}

			muts := make([]qte.BatchMutation, 0, len(raw))
			for i, r := range raw {
				m, ok := r.(map[string]any)
				if !ok {
					return mcp.NewToolResultError(fmt.Sprintf("mutation %d must be an object", i)), nil
				}
				if k := unknownKey(m, "rowKey", "setCols", "delCols", "delete"); k != "" {
					return mcp.NewToolResultError(fmt.Sprintf("mutation %d: unknown key %q", i, k)), nil
				}
				mut, err := parseMutation(m)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("mutation %d: %v", i, err)), nil
				}
				muts = append(muts, mut)
			}

			if err := db.BatchWrite(table, muts); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(`{"status":"ok"}`), nil
		},
	)
}

// predicateProps is the JSON schema of a {column, op, value} predicate's fields.
func predicateProps() map[string]any {
	return map[string]any{
		"column": map[string]any{"type": "string"},
		"op": map[string]any{"type": "string", "enum": []string{
			"=", "!=", ">", ">=", "<", "<=", "CONTAINS", "PREFIX", "EXISTS", "MISSING",
		}},
		"value": map[string]any{},
	}
}

// unknownKey returns a key of m that is not in allowed, or "" if there is none.
// Misspelled keys must fail rather than be ignored, or a write could lose its condition.
func unknownKey(m map[string]any, allowed ...string) string {
	for k := range m {
		if !slices.Contains(allowed, k) {
			return k
		}
	}
	return ""
}

// parseMutation converts one decoded JSON mutation object into a BatchMutation.
func parseMutation(m map[string]any) (qte.BatchMutation, error) {
	var mut qte.BatchMutation
	var ok bool

	if mut.RowKey, ok = m["rowKey"].(string); !ok {
		return mut, errors.New("rowKey must be a string")
	}
	if raw, present := m["setCols"]; present {
		if mut.SetCols, ok = raw.(map[string]any); !ok {
			return mut, errors.New("setCols must be an object")
		}
	}
	if raw, present := m["delCols"]; present {
		cols, ok := raw.([]any)
		if !ok {
			return mut, errors.New("delCols must be an array of column names")
		}
		for _, c := range cols {
			s, ok := c.(string)
			if !ok {
				return mut, errors.New("delCols must be an array of column names")
			}
			mut.DelCols = append(mut.DelCols, s)
		}
	}
	if raw, present := m["delete"]; present {
		if mut.Delete, ok = raw.(bool); !ok {
			return mut, errors.New("delete must be a boolean")
		}
	}
	return mut, nil
}
