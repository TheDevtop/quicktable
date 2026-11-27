package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/TheDevtop/quicktable/internal/mustang"
)

// errRequest marks a request the server cannot make sense of.
var errRequest = errors.New("bad request")

// cmps maps comparison names to Mustang ops; the names match the qt shell.
var cmps = map[string]mustang.Op{
	"=": mustang.Eq, "!=": mustang.Ne,
	"<": mustang.Lt, "<=": mustang.Le,
	">": mustang.Gt, ">=": mustang.Ge,
	"prefix": mustang.Prefix, "contains": mustang.Contains,
	"exists": mustang.Exists, "missing": mustang.Missing,
}

func cmp(name string) (mustang.Op, error) {
	o, ok := cmps[name]
	if !ok {
		return 0, fmt.Errorf("%w: unknown cmp %q", errRequest, name)
	}
	return o, nil
}

// Requests. Which of row and column are set picks the scope of get and delete.
type (
	scanReq struct {
		Table   string `json:"table,omitempty" jsonschema:"List the row keys of this table; all tables if omitted"`
		Columns bool   `json:"columns,omitempty" jsonschema:"List the column names of table instead of its row keys"`
	}
	addr struct {
		Table  string `json:"table" jsonschema:"Table name"`
		Row    string `json:"row,omitempty" jsonschema:"Row key"`
		Column string `json:"column,omitempty" jsonschema:"Column name"`
	}
	valuesReq struct {
		Cells []addr `json:"cells" jsonschema:"Cells to read; each needs table, row and column"`
	}
	pred struct {
		Column string `json:"column" jsonschema:"Column name"`
		Cmp    string `json:"cmp" jsonschema:"One of =, !=, <, <=, >, >=, prefix, contains, exists, missing"`
		Value  any    `json:"value,omitempty" jsonschema:"Operand: number, string or boolean; not used by exists and missing"`
	}
	queryReq struct {
		Table   string   `json:"table" jsonschema:"Table name"`
		Prefix  string   `json:"prefix,omitempty" jsonschema:"Only row keys with this prefix"`
		From    string   `json:"from,omitempty" jsonschema:"Only row keys >= from"`
		To      string   `json:"to,omitempty" jsonschema:"Only row keys < to"`
		Where   []pred   `json:"where,omitempty" jsonschema:"Predicates that must all hold"`
		Select  []string `json:"select,omitempty" jsonschema:"Columns to return; all if omitted"`
		Limit   int      `json:"limit,omitempty" jsonschema:"Stop after this many matching rows; 0 is no limit"`
		Reverse bool     `json:"reverse,omitempty" jsonschema:"Scan row keys in descending order, so limit keeps the last rows"`
		Agg     string   `json:"agg,omitempty" jsonschema:"Fold the matching rows instead: count, or sum, avg, min, max of column"`
		Column  string   `json:"column,omitempty" jsonschema:"Column for sum, avg, min and max"`
	}
	batchOp struct {
		Op     string  `json:"op" jsonschema:"set, delete, if or incr"`
		Table  string  `json:"table" jsonschema:"Table name"`
		Row    string  `json:"row,omitempty" jsonschema:"Row key; delete without row deletes a column or the whole table"`
		Column string  `json:"column,omitempty" jsonschema:"Column name; delete without column deletes a row or the whole table"`
		Cmp    string  `json:"cmp,omitempty" jsonschema:"Comparison for if: =, !=, <, <=, >, >=, prefix, contains, exists, missing"`
		Value  any     `json:"value,omitempty" jsonschema:"Value for set, operand for if: number, string or boolean"`
		Delta  float64 `json:"delta,omitempty" jsonschema:"Amount for incr"`
	}
	batchReq struct {
		Ops []batchOp `json:"ops" jsonschema:"Operations, applied in order"`
	}
	incrReq struct {
		Table  string  `json:"table" jsonschema:"Table name"`
		Row    string  `json:"row" jsonschema:"Row key"`
		Column string  `json:"column" jsonschema:"Column name"`
		Delta  float64 `json:"delta" jsonschema:"Amount to add"`
	}
)

// result is one query row; a slice of them keeps the rowKey order on the wire.
type result struct {
	Key string      `json:"key"`
	Row mustang.Row `json:"row"`
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func scan(s *mustang.Store, req scanReq) (any, error) {
	var names []string
	var err error
	switch {
	case req.Table == "" && req.Columns:
		return nil, fmt.Errorf("%w: columns needs a table", errRequest)
	case req.Table == "":
		names, err = s.ScanTables()
	case req.Columns:
		names, err = s.ScanColumns(req.Table)
	default:
		names, err = s.ScanRows(req.Table)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"names": orEmpty(names)}, nil
}

func get(s *mustang.Store, req addr) (any, error) {
	var k string
	var v any
	var err error
	switch {
	case req.Row == "" && req.Column == "":
		k = "table"
		v, err = s.GetTable(req.Table)
	case req.Column == "":
		k = "row"
		v, err = s.GetRow(req.Table, req.Row)
	case req.Row == "":
		k = "column"
		v, err = s.GetColumn(req.Table, req.Column)
	default:
		k = "value"
		v, err = s.GetValue(req.Table, req.Row, req.Column)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{k: v}, nil
}

func values(s *mustang.Store, req valuesReq) (any, error) {
	cells := make([]mustang.Cell, 0, len(req.Cells))
	for _, c := range req.Cells {
		cells = append(cells, mustang.Cell{Table: c.Table, Row: c.Row, Col: c.Column})
	}
	vs, err := s.GetValues(cells)
	if err != nil {
		return nil, err
	}
	return map[string]any{"values": vs}, nil
}

func query(s *mustang.Store, req queryReq) (any, error) {
	q := s.Query(req.Table).Prefix(req.Prefix).From(req.From).To(req.To).Limit(req.Limit)
	for _, p := range req.Where {
		o, err := cmp(p.Cmp)
		if err != nil {
			return nil, err
		}
		q.Where(p.Column, o, p.Value)
	}
	if len(req.Select) > 0 {
		q.Select(req.Select...)
	}
	if req.Reverse {
		q.Reverse()
	}

	var v any
	var err error
	switch req.Agg {
	case "":
		res, err := q.Run()
		if err != nil {
			return nil, err
		}
		rows := make([]result, 0, len(res))
		for _, r := range res {
			rows = append(rows, result{r.Key, r.Row})
		}
		return map[string]any{"rows": rows}, nil
	case "count":
		v, err = q.Count()
	case "sum":
		v, err = q.Sum(req.Column)
	case "avg":
		v, err = q.Avg(req.Column)
	case "min":
		v, err = q.Min(req.Column)
	case "max":
		v, err = q.Max(req.Column)
	default:
		return nil, fmt.Errorf("%w: unknown agg %q", errRequest, req.Agg)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"value": v}, nil
}

func batch(s *mustang.Store, req batchReq) (any, error) {
	b := s.Batch()
	for i, o := range req.Ops {
		switch o.Op {
		case "set":
			b.Set(o.Table, o.Row, o.Column, o.Value)
		case "delete":
			switch {
			case o.Row != "" && o.Column != "":
				b.Delete(o.Table, o.Row, o.Column)
			case o.Row != "":
				b.DeleteRow(o.Table, o.Row)
			case o.Column != "":
				b.DeleteColumn(o.Table, o.Column)
			default:
				b.DeleteTable(o.Table)
			}
		case "if":
			c, err := cmp(o.Cmp)
			if err != nil {
				return nil, fmt.Errorf("op %d: %w", i, err)
			}
			b.If(o.Table, o.Row, o.Column, c, o.Value)
		case "incr":
			b.Incr(o.Table, o.Row, o.Column, o.Delta)
		default:
			return nil, fmt.Errorf("op %d: %w: unknown op %q", i, errRequest, o.Op)
		}
	}
	if err := b.Commit(); err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok"}, nil
}

func incr(s *mustang.Store, req incrReq) (any, error) {
	n, err := s.Incr(req.Table, req.Row, req.Column, req.Delta)
	if err != nil {
		return nil, err
	}
	return map[string]any{"value": n}, nil
}

// endpoint is one operation, served as POST /api/<name> and as MCP tool qt_<name>.
type endpoint struct {
	name, desc string
	ro         bool           // reads only
	schema     mcp.ToolOption // MCP input schema, derived from the request type
	call       func(s *mustang.Store, body io.Reader) (any, error)
}

// op wraps fn so it takes a JSON body. Unknown fields are rejected:
// a misspelled "row" must not turn a row delete into a table drop.
func op[T any](name, desc string, ro bool, fn func(*mustang.Store, T) (any, error)) endpoint {
	return endpoint{name, desc, ro, mcp.WithInputSchema[T](), func(s *mustang.Store, body io.Reader) (any, error) {
		var req T
		dec := json.NewDecoder(body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: %v", errRequest, err)
		}
		return fn(s, req)
	}}
}

var endpoints = []endpoint{
	op("scan", "List table names, the row keys of a table, or the column names of a table.", true, scan),
	op("get", "Read a whole table, a row, a column (row key to value) or one value; "+
		"which of row and column are given picks the scope.", true, get),
	op("values", "Read many cells from one consistent snapshot; missing cells are null.", true, values),
	op("query", "Scan a table's rows in row key order, filtered by key range and predicates, "+
		"or fold them with count, sum, avg, min or max.", true, query),
	op("batch", "Apply operations atomically and in order: set a cell, delete a cell, row, column or table, "+
		"check a cell with if, or incr a number. A failing if aborts the whole batch.", false, batch),
	op("incr", "Atomically add delta to a numeric cell, a missing cell counting as 0, and return the new value.", false, incr),
}
