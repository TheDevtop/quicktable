package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"git.hkr.lan/thijs/quicktable/pkg/qte"
)

// MCPHandler integrates the MCP server into a standard http.ServeMux using SSE transport.
type MCPHandler struct {
	mcpServer *server.MCPServer
	sessions  sync.Map
}

type sseClient struct {
	w       http.ResponseWriter
	flusher http.Flusher
	done    chan struct{}
}

// RegisterMCPRoutes creates and registers the MCP server endpoints on mux.
// Endpoints:
//
//	GET  /mcp/sse     - SSE stream endpoint for clients
//	POST /mcp/message - JSON-RPC message endpoint
func RegisterMCPRoutes(mux *http.ServeMux, db *qte.DB) *MCPHandler {
	s := server.NewMCPServer("quicktable-mcp", "1.0.0")
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
	defer h.sessions.Delete(sessionID)

	// Send endpoint notification so client knows where to POST JSON-RPC messages
	msgEndpoint := fmt.Sprintf("/mcp/message?sessionId=%s", sessionID)
	fmt.Fprintf(w, "event: endpoint\ndata: %s\r\n\r\n", msgEndpoint)
	flusher.Flush()

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

	ctx := r.Context()
	resp := h.mcpServer.HandleMessage(ctx, raw)
	if resp != nil {
		data, err := json.Marshal(resp)
		if err == nil {
			fmt.Fprintf(client.w, "event: message\ndata: %s\r\n\r\n", data)
			client.flusher.Flush()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *MCPHandler) Close() {
	h.sessions.Range(func(key, value any) bool {
		if c, ok := value.(*sseClient); ok {
			close(c.done)
		}
		h.sessions.Delete(key)
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

			if float64(int64(delta)) == delta {
				newVal, err := db.IncrCell(table, rowKey, col, int64(delta))
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				res, _ := json.Marshal(map[string]int64{"value": newVal})
				return mcp.NewToolResultText(string(res)), nil
			}

			newVal, err := db.IncrCellFloat(table, rowKey, col, delta)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			res, _ := json.Marshal(map[string]float64{"value": newVal})
			return mcp.NewToolResultText(string(res)), nil
		},
	)

	// 10. qte_append_cell / qte_prepend_cell
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
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			table, _ := req.GetArguments()["table"].(string)
			rawKeys, ok := req.GetArguments()["rowKeys"].([]any)
			if !ok {
				return mcp.NewToolResultError("rowKeys array required"), nil
			}
			keys := make([]string, 0, len(rawKeys))
			for _, k := range rawKeys {
				if s, ok := k.(string); ok {
					keys = append(keys, s)
				}
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
}
