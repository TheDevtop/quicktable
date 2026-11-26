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

// MCP server version
const mcpVersion = "0.2.0"

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
	s := server.NewMCPServer("quicktable-mcp", mcpVersion)
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
		mcp.NewTool("qte_query", slices.Concat(
			[]mcp.ToolOption{
				mcp.WithDescription("Query, scan, filter and project rows in a table, or aggregate a numeric column over them."),
				mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			},
			filterOpts(),
			[]mcp.ToolOption{
				mcp.WithBoolean("countOnly", mcp.Description("Return only count of matching rows")),
				mcp.WithString("aggregate", mcp.Description("Return count, sum, min, max and avg of this numeric column over matching rows, "+
					"ignoring offset and limit; non-numeric cells are skipped")),
				mcp.WithArray("select", mcp.WithStringItems(), mcp.Description("Columns to return; all columns if omitted")),
			},
		)...),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := req.GetArguments()
			countOnly, _ := args["countOnly"].(bool)
			aggregate, _ := args["aggregate"].(string)
			if countOnly && aggregate != "" {
				return mcp.NewToolResultError("countOnly and aggregate cannot be combined"), nil
			}

			qb, err := buildQuery(db, args)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if raw, ok := args["select"]; ok {
				cols, err := stringList(raw, "select")
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				qb.Select(cols...)
			}

			if aggregate != "" {
				stats, err := qb.Aggregate(aggregate)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				res, _ := json.Marshal(stats)
				return mcp.NewToolResultText(string(res)), nil
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
			mcp.WithDescription("Atomically increment a numeric cell value."),
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

			cond, err := parseCondition(args)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
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

	// 18. qte_batch_insert
	s.AddTool(
		mcp.NewTool("qte_batch_insert",
			mcp.WithDescription("Insert several rows atomically, each under a new auto-generated 8-digit hex key. "+
				"Returns the keys in the order of the rows."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithArray("rows", mcp.Required(), mcp.Description("Rows to insert, each an object of column name to scalar value pairs"),
				mcp.Items(map[string]any{"type": "object"})),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			raw, ok := req.GetArguments()["rows"].([]any)
			if !ok {
				return mcp.NewToolResultError("rows array required"), nil
			}

			rows := make([]qte.Row, 0, len(raw))
			for i, r := range raw {
				m, ok := r.(map[string]any)
				if !ok {
					return mcp.NewToolResultError(fmt.Sprintf("row %d must be an object", i)), nil
				}
				rows = append(rows, m)
			}

			keys, err := db.BatchInsert(table, rows)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			res, _ := json.Marshal(map[string]any{"rowKeys": keys})
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 19. qte_get_cells
	s.AddTool(
		mcp.NewTool("qte_get_cells",
			mcp.WithDescription("Retrieve selected columns of a row. Missing columns are omitted from the result."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
			mcp.WithArray("columns", mcp.Required(), mcp.WithStringItems(), mcp.Description("Column names")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rowKey, _ := req.GetArguments()["rowKey"].(string)
			cols, err := stringList(req.GetArguments()["columns"], "columns")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			row, err := db.GetCells(table, rowKey, cols)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			res, _ := json.Marshal(row)
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 20. qte_row_exists
	s.AddTool(
		mcp.NewTool("qte_row_exists",
			mcp.WithDescription("Check whether a row holds any cell, without reading its values."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rowKey, _ := req.GetArguments()["rowKey"].(string)

			ok, err := db.RowExists(table, rowKey)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			res, _ := json.Marshal(map[string]bool{"exists": ok})
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 21. qte_list_columns
	s.AddTool(
		mcp.NewTool("qte_list_columns",
			mcp.WithDescription("List the distinct column names used in a table, sorted. Scans every key of the table."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			cols, err := db.ListColumns(table)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if cols == nil {
				cols = []string{}
			}
			res, _ := json.Marshal(cols)
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 22. qte_cad
	s.AddTool(
		mcp.NewTool("qte_cad",
			mcp.WithDescription("Atomically check a condition on one column of a row and, if it holds, delete columns, "+
				"or the whole row if delCols is omitted. Without a condition the delete is unconditional. Unknown keys are rejected."),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			mcp.WithString("rowKey", mcp.Required(), mcp.Description("8-character hex row key")),
			mcp.WithObject("condition", mcp.Description(`{"column": name, "op": op, "value": v}; same operators as qte_query where`),
				mcp.Properties(predicateProps()), mcp.AdditionalProperties(false)),
			mcp.WithArray("delCols", mcp.WithStringItems(), mcp.Description("Columns to delete; the whole row if omitted")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := req.GetArguments()
			if k := unknownKey(args, "table", "rowKey", "condition", "delCols"); k != "" {
				return mcp.NewToolResultError(fmt.Sprintf("unknown argument %q", k)), nil
			}
			table, _ := args["table"].(string)
			rowKey, _ := args["rowKey"].(string)

			cond, err := parseCondition(args)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			var delCols []string
			if raw, ok := args["delCols"]; ok {
				if delCols, err = stringList(raw, "delCols"); err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
			}

			if err := db.CheckAndDelete(table, rowKey, cond, delCols); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(`{"status":"ok"}`), nil
		},
	)

	// 23. qte_delete_query
	s.AddTool(
		mcp.NewTool("qte_delete_query", slices.Concat(
			[]mcp.ToolOption{
				mcp.WithDescription("Atomically delete every row matching a filter; offset and limit apply as in qte_query. " +
					"Without any filter every row of the table is deleted. Irreversible. Unknown arguments are rejected."),
				mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table")),
			},
			filterOpts(),
		)...),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := req.GetArguments()
			if k := unknownKey(args, "table", "prefix", "startKey", "endKey", "orderDesc", "offset", "limit", "where", "or"); k != "" {
				return mcp.NewToolResultError(fmt.Sprintf("unknown argument %q", k)), nil
			}

			qb, err := buildQuery(db, args)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			n, err := qb.Delete()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			res, _ := json.Marshal(map[string]int{"deleted": n})
			return mcp.NewToolResultText(string(res)), nil
		},
	)
}

// filterOpts are the row selection parameters shared by qte_query and qte_delete_query.
func filterOpts() []mcp.ToolOption {
	pred := map[string]any{
		"type":       "object",
		"properties": predicateProps(),
		"required":   []string{"column", "op"},
	}
	return []mcp.ToolOption{
		mcp.WithString("prefix", mcp.Description("Filter by row key prefix")),
		mcp.WithString("startKey", mcp.Description("Start range key")),
		mcp.WithString("endKey", mcp.Description("End range key")),
		mcp.WithBoolean("orderDesc", mcp.Description("Sort row keys in descending order")),
		mcp.WithNumber("offset", mcp.Description("Skip first n rows")),
		mcp.WithNumber("limit", mcp.Description("Cap maximum rows")),
		mcp.WithArray("where", mcp.Description(`Predicates that must all hold, each {"column": name, "op": op, "value": v}. `+
			`op is one of =, !=, >, >=, <, <=, CONTAINS, PREFIX, EXISTS, MISSING; EXISTS and MISSING take no value`),
			mcp.Items(pred)),
		mcp.WithArray("or", mcp.Description("Alternative predicate groups instead of where: a row matches if all predicates "+
			"of any group hold. Cannot be combined with where."),
			mcp.Items(map[string]any{"type": "array", "items": pred})),
	}
}

// buildQuery applies the filter arguments of qte_query and qte_delete_query to a new query.
func buildQuery(db *qte.DB, args map[string]any) (*qte.QueryBuilder, error) {
	table, _ := args["table"].(string)
	prefix, _ := args["prefix"].(string)
	startKey, _ := args["startKey"].(string)
	endKey, _ := args["endKey"].(string)
	orderDesc, _ := args["orderDesc"].(bool)
	offset, _ := args["offset"].(float64)
	limit, _ := args["limit"].(float64)

	qb := db.Query(table).
		WithPrefix(prefix).
		WithKeyRange(startKey, endKey).
		Offset(int(offset)).
		Limit(int(limit))
	if orderDesc {
		qb.OrderDesc()
	}

	where, hasWhere := args["where"]
	or, hasOr := args["or"]
	if hasWhere && hasOr {
		return nil, errors.New("where and or cannot be combined")
	}
	if hasWhere {
		conds, err := parsePredicates(where)
		if err != nil {
			return nil, err
		}
		for _, c := range conds {
			qb.Where(c.Column, c.Op, c.Value)
		}
	}
	if hasOr {
		groups, ok := or.([]any)
		if !ok {
			return nil, errors.New("or must be an array of predicate arrays")
		}
		for i, g := range groups {
			conds, err := parsePredicates(g)
			if err != nil {
				return nil, fmt.Errorf("or group %d: %w", i, err)
			}
			if len(conds) == 0 {
				return nil, fmt.Errorf("or group %d is empty", i)
			}
			qb.Or()
			for _, c := range conds {
				qb.Where(c.Column, c.Op, c.Value)
			}
		}
	}
	return qb, nil
}

// parsePredicates converts a decoded JSON array of predicate objects into conditions.
func parsePredicates(raw any) ([]qte.Condition, error) {
	preds, ok := raw.([]any)
	if !ok {
		return nil, errors.New("where must be an array of predicates")
	}
	conds := make([]qte.Condition, 0, len(preds))
	for _, p := range preds {
		m, ok := p.(map[string]any)
		if !ok {
			return nil, errors.New("each where predicate must be an object")
		}
		col, _ := m["column"].(string)
		op, _ := m["op"].(string)
		conds = append(conds, qte.Condition{Column: col, Op: qte.CompareOp(op), Value: m["value"]})
	}
	return conds, nil
}

// parseCondition returns the optional condition argument of qte_cas and qte_cad, rejecting unknown keys.
func parseCondition(args map[string]any) (*qte.Condition, error) {
	raw, ok := args["condition"]
	if !ok {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("condition must be an object")
	}
	if k := unknownKey(m, "column", "op", "value"); k != "" {
		return nil, fmt.Errorf("unknown condition key %q", k)
	}
	col, _ := m["column"].(string)
	op, _ := m["op"].(string)
	return &qte.Condition{Column: col, Op: qte.CompareOp(op), Value: m["value"]}, nil
}

// stringList converts a decoded JSON array of strings; name labels the error.
func stringList(raw any, name string) ([]string, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", name)
	}
	list := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := it.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be an array of strings", name)
		}
		list = append(list, s)
	}
	return list, nil
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
