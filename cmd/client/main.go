package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"git.hkr.lan/thijs/quicktable/pkg/client"
)

var addr = flag.String("addr", "http://localhost:16000", "Quicktable server address")

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
	}

	c := client.New(*addr)
	var err error

	switch args[0] {
	case "list-tables":
		err = cmdListTables(c, args[1:])
	case "drop-table":
		err = cmdDropTable(c, args[1:])
	case "get-cell":
		err = cmdGetCell(c, args[1:])
	case "set-cell":
		err = cmdSetCell(c, args[1:])
	case "del-cell":
		err = cmdDelCell(c, args[1:])
	case "list-rows":
		err = cmdListRows(c, args[1:])
	case "get-row":
		err = cmdGetRow(c, args[1:])
	case "set-row":
		err = cmdSetRow(c, args[1:])
	case "insert-row":
		err = cmdInsertRow(c, args[1:])
	case "del-row":
		err = cmdDelRow(c, args[1:])
	case "query":
		err = cmdQuery(c, args[1:])
	case "batch-get":
		err = cmdBatchGet(c, args[1:])
	case "batch-delete":
		err = cmdBatchDelete(c, args[1:])
	case "incr":
		err = cmdIncr(c, args[1:])
	case "append":
		err = cmdAppend(c, args[1:])
	case "prepend":
		err = cmdPrepend(c, args[1:])
	case "cas":
		err = cmdCAS(c, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		usage()
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: qt [-addr url] <command> [args...]

commands:
  list-tables
  drop-table  <table>
  get-cell   <table> <rowKey> <column>
  set-cell    <table> <rowKey> <column> <json-value>
  del-cell    <table> <rowKey> <column>
  list-rows   <table>
  get-row     <table> <rowKey>
  set-row     <table> <rowKey> <json-columns>
  insert-row  <table> [json-columns]
  del-row     <table> <rowKey>
  query       <table> [json-query]
  batch-get   <table> <key> [key...]
  batch-delete <table> <key> [key...]
  incr        <table> <rowKey> <column> <delta>
  append      <table> <rowKey> <column> <value>
  prepend     <table> <rowKey> <column> <value>
  cas         <table> <rowKey> <json-body>
`)
	os.Exit(2)
}

func needArgs(args []string, n int, cmd string) {
	if len(args) < n {
		fmt.Fprintf(os.Stderr, "%s: need at least %d arguments\n", cmd, n)
		os.Exit(2)
	}
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// parseJSONValue unmarshals a JSON literal from a CLI argument.
func parseJSONValue(s string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("invalid json value %q: %w", s, err)
	}
	return v, nil
}

func parseRow(s string) (client.Row, error) {
	var r client.Row
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil, fmt.Errorf("invalid json row: %w", err)
	}
	return r, nil
}

// --- command handlers ---

func cmdListTables(c *client.Client, args []string) error {
	tables, err := c.ListTables()
	if err != nil {
		return err
	}
	for _, t := range tables {
		fmt.Println(t)
	}
	return nil
}

func cmdDropTable(c *client.Client, args []string) error {
	needArgs(args, 1, "drop-table")
	return c.DropTable(args[0])
}

func cmdGetCell(c *client.Client, args []string) error {
	needArgs(args, 3, "get-cell")
	val, err := c.GetCell(args[0], args[1], args[2])
	if err != nil {
		return err
	}
	printJSON(val)
	return nil
}

func cmdSetCell(c *client.Client, args []string) error {
	needArgs(args, 4, "set-cell")
	val, err := parseJSONValue(args[3])
	if err != nil {
		return err
	}
	return c.SetCell(args[0], args[1], args[2], val)
}

func cmdDelCell(c *client.Client, args []string) error {
	needArgs(args, 3, "del-cell")
	return c.DeleteCell(args[0], args[1], args[2])
}

func cmdListRows(c *client.Client, args []string) error {
	needArgs(args, 1, "list-rows")
	rows, err := c.ListRows(args[0])
	if err != nil {
		return err
	}
	for _, r := range rows {
		fmt.Println(r)
	}
	return nil
}

func cmdGetRow(c *client.Client, args []string) error {
	needArgs(args, 2, "get-row")
	row, err := c.GetRow(args[0], args[1])
	if err != nil {
		return err
	}
	printJSON(row)
	return nil
}

func cmdSetRow(c *client.Client, args []string) error {
	needArgs(args, 3, "set-row")
	row, err := parseRow(args[2])
	if err != nil {
		return err
	}
	return c.SetRow(args[0], args[1], row)
}

func cmdInsertRow(c *client.Client, args []string) error {
	needArgs(args, 1, "insert-row")
	var cols client.Row
	if len(args) > 1 {
		var err error
		cols, err = parseRow(args[1])
		if err != nil {
			return err
		}
	} else {
		cols = client.Row{}
	}
	key, err := c.InsertRow(args[0], cols)
	if err != nil {
		return err
	}
	fmt.Println(key)
	return nil
}

func cmdDelRow(c *client.Client, args []string) error {
	needArgs(args, 2, "del-row")
	return c.DeleteRow(args[0], args[1])
}

func cmdQuery(c *client.Client, args []string) error {
	needArgs(args, 1, "query")
	var q *client.QueryRequest
	if len(args) > 1 {
		q = &client.QueryRequest{}
		if err := json.Unmarshal([]byte(args[1]), q); err != nil {
			return fmt.Errorf("invalid json query: %w", err)
		}
	}
	res, err := c.Query(args[0], q)
	if err != nil {
		return err
	}
	printJSON(res)
	return nil
}

func cmdBatchGet(c *client.Client, args []string) error {
	needArgs(args, 2, "batch-get")
	rows, err := c.BatchGet(args[0], args[1:])
	if err != nil {
		return err
	}
	printJSON(rows)
	return nil
}

func cmdBatchDelete(c *client.Client, args []string) error {
	needArgs(args, 2, "batch-delete")
	return c.BatchDelete(args[0], args[1:])
}

func cmdIncr(c *client.Client, args []string) error {
	needArgs(args, 4, "incr")
	delta, err := parseJSONValue(args[3])
	if err != nil {
		return err
	}
	val, err := c.Incr(args[0], args[1], args[2], delta)
	if err != nil {
		return err
	}
	printJSON(val)
	return nil
}

func cmdAppend(c *client.Client, args []string) error {
	needArgs(args, 4, "append")
	val, err := c.Append(args[0], args[1], args[2], args[3])
	if err != nil {
		return err
	}
	printJSON(val)
	return nil
}

func cmdPrepend(c *client.Client, args []string) error {
	needArgs(args, 4, "prepend")
	val, err := c.Prepend(args[0], args[1], args[2], args[3])
	if err != nil {
		return err
	}
	printJSON(val)
	return nil
}

func cmdCAS(c *client.Client, args []string) error {
	needArgs(args, 3, "cas")
	table := args[0]
	rowKey := args[1]

	var body struct {
		Condition *client.CASCondition `json:"condition,omitempty"`
		SetCols   client.Row           `json:"setCols"`
	}
	if err := json.Unmarshal([]byte(args[2]), &body); err != nil {
		return fmt.Errorf("invalid json cas body: %w", err)
	}

	// CAS condition fields arrive camelCase; normalise to PascalCase for the wire.
	// Accept both styles transparently via a raw re-parse.
	if body.Condition == nil && body.SetCols == nil {
		// Try alternate casing via raw map
		var raw map[string]json.RawMessage
		_ = json.Unmarshal([]byte(args[2]), &raw)
		if v, ok := raw["Condition"]; ok {
			body.Condition = &client.CASCondition{}
			_ = json.Unmarshal(v, body.Condition)
		}
		if v, ok := raw["SetCols"]; ok {
			_ = json.Unmarshal(v, &body.SetCols)
		}
	}

	return c.CAS(table, rowKey, body.Condition, body.SetCols)
}

func init() {
	// Accept -addr from QUICKTABLE_ADDR env as a default.
	if v := os.Getenv("QUICKTABLE_ADDR"); v != "" {
		_ = strings.TrimSpace(v)
		flag.Set("addr", v)
	}
}
