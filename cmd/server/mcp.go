package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/TheDevtop/quicktable/internal/mustang"
)

const mcpVersion = "0.4.0"

// RegisterMCPRoutes serves every endpoint as tool qt_<name> over Streamable HTTP at /mcp.
// The server only offers tools, so it keeps no sessions and opens no streams:
// each call is a plain request and response, bounded by the server's timeouts.
func RegisterMCPRoutes(mux *http.ServeMux, s *mustang.Store) {
	srv := server.NewMCPServer("quicktable-mcp", mcpVersion)
	for _, e := range endpoints {
		tool := mcp.NewTool("qt_"+e.name,
			mcp.WithDescription(e.desc),
			e.schema,
			mcp.WithReadOnlyHintAnnotation(e.ro),
			mcp.WithDestructiveHintAnnotation(!e.ro),
			mcp.WithOpenWorldHintAnnotation(false),
		)
		srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args, err := json.Marshal(req.GetArguments())
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			v, err := e.call(s, bytes.NewReader(args))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			out, err := json.Marshal(v)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(out)), nil
		})
	}
	mux.Handle("/mcp", server.NewStreamableHTTPServer(srv,
		server.WithStateLess(true),
		server.WithDisableStreaming(true),
	))
}
