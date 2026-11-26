package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client talks to a Quicktable REST API server.
type Client struct {
	base string // e.g. "http://localhost:16000"
	http *http.Client
}

// New creates a client pointing at the given base URL.
func New(base string) *Client {
	return &Client{
		base: base,
		http: http.DefaultClient,
	}
}

// Row is a column→value map, mirroring qte.Row.
type Row map[string]any

// QueryRow is a single row returned by Query.
type QueryRow struct {
	RowKey  string `json:"rowKey"`
	Columns Row    `json:"columns"`
}

// Predicate is a column filter for queries.
type Predicate struct {
	Column string `json:"column"`
	Op     string `json:"op"`
	Value  any    `json:"value,omitempty"`
}

// QueryRequest holds all optional query parameters.
type QueryRequest struct {
	Prefix    string      `json:"prefix,omitempty"`
	StartKey  string      `json:"startKey,omitempty"`
	EndKey    string      `json:"endKey,omitempty"`
	Select    []string    `json:"select,omitempty"`
	OrderDesc bool        `json:"orderDesc,omitempty"`
	Offset    int         `json:"offset,omitempty"`
	Limit     int         `json:"limit,omitempty"`
	Where     []Predicate `json:"where,omitempty"`
	CountOnly bool        `json:"countOnly,omitempty"`
}

// QueryResult holds the response from a query.
type QueryResult struct {
	Table string     `json:"table"`
	Rows  []QueryRow `json:"rows"`
	Count int        `json:"count"`
}

// Mutation describes a single row mutation in a batch write.
type Mutation struct {
	RowKey  string   `json:"rowKey"`
	SetCols Row      `json:"setCols,omitempty"`
	DelCols []string `json:"delCols,omitempty"`
	Delete  bool     `json:"delete,omitempty"`
}

// CASCondition is the check predicate for a check-and-set.
type CASCondition struct {
	Column string `json:"column"`
	Op     string `json:"op"`
	Value  any    `json:"value,omitempty"`
}

// Health reports whether the server is up and answering requests.
func (c *Client) Health() error {
	return c.get("/health", nil)
}

// --- Table operations ---

// ListTables returns the names of all tables, sorted.
func (c *Client) ListTables() ([]string, error) {
	var resp struct {
		Tables []string `json:"tables"`
	}
	err := c.get("/api/tables", &resp)
	return resp.Tables, err
}

// DropTable permanently deletes a table and all of its rows.
// Dropping a table that does not exist is not an error.
func (c *Client) DropTable(table string) error {
	return c.del(apiPath(table), nil)
}

// ListRows returns the keys of all rows in a table, sorted.
func (c *Client) ListRows(table string) ([]string, error) {
	var resp struct {
		Rows []string `json:"rows"`
	}
	err := c.get(apiPath(table, "rows"), &resp)
	return resp.Rows, err
}

// --- Cell operations ---

// GetCell retrieves a single cell value.
func (c *Client) GetCell(table, rowKey, column string) (any, error) {
	var resp struct {
		Value any `json:"value"`
	}
	err := c.get(apiPath(table, "rows", rowKey, "columns", column), &resp)
	return resp.Value, err
}

// SetCell writes a single cell value.
func (c *Client) SetCell(table, rowKey, column string, value any) error {
	body := map[string]any{"value": value}
	return c.putJSON(apiPath(table, "rows", rowKey, "columns", column), body, nil)
}

// DeleteCell removes a single cell.
func (c *Client) DeleteCell(table, rowKey, column string) error {
	return c.del(apiPath(table, "rows", rowKey, "columns", column), nil)
}

// --- Row operations ---

// GetRow retrieves all columns of a row.
func (c *Client) GetRow(table, rowKey string) (Row, error) {
	var resp struct {
		Columns Row `json:"columns"`
	}
	err := c.get(apiPath(table, "rows", rowKey), &resp)
	return resp.Columns, err
}

// SetRow merges columns into a row.
func (c *Client) SetRow(table, rowKey string, cols Row) error {
	return c.putJSON(apiPath(table, "rows", rowKey), cols, nil)
}

// InsertRow creates a new row with an auto-generated key.
func (c *Client) InsertRow(table string, cols Row) (string, error) {
	var resp struct {
		RowKey string `json:"rowKey"`
	}
	err := c.postJSON(apiPath(table, "rows"), cols, &resp)
	return resp.RowKey, err
}

// DeleteRow removes an entire row.
func (c *Client) DeleteRow(table, rowKey string) error {
	return c.del(apiPath(table, "rows", rowKey), nil)
}

// --- Query ---

// Query scans, filters, and projects rows.
func (c *Client) Query(table string, q *QueryRequest) (*QueryResult, error) {
	var body any
	if q != nil {
		body = q
	}
	var resp QueryResult
	err := c.postJSON(apiPath(table, "query"), body, &resp)
	return &resp, err
}

// --- Batch ---

// BatchGet retrieves multiple rows by key.
func (c *Client) BatchGet(table string, keys []string) (map[string]Row, error) {
	body := map[string]any{
		"action":  "get",
		"rowKeys": keys,
	}
	var resp struct {
		Rows map[string]Row `json:"rows"`
	}
	err := c.postJSON(apiPath(table, "batch"), body, &resp)
	return resp.Rows, err
}

// BatchWrite applies multiple row mutations atomically.
func (c *Client) BatchWrite(table string, mutations []Mutation) error {
	body := map[string]any{
		"action":    "write",
		"mutations": mutations,
	}
	return c.postJSON(apiPath(table, "batch"), body, nil)
}

// BatchInsert inserts multiple rows atomically under new auto-generated keys,
// returned in the order of rows.
func (c *Client) BatchInsert(table string, rows []Row) ([]string, error) {
	body := map[string]any{
		"action": "insert",
		"rows":   rows,
	}
	var resp struct {
		RowKeys []string `json:"rowKeys"`
	}
	err := c.postJSON(apiPath(table, "batch"), body, &resp)
	return resp.RowKeys, err
}

// BatchDelete deletes multiple rows by key.
func (c *Client) BatchDelete(table string, keys []string) error {
	body := map[string]any{
		"action":  "delete",
		"rowKeys": keys,
	}
	return c.postJSON(apiPath(table, "batch"), body, nil)
}

// --- Atomic operations ---

// Incr atomically increments a numeric cell and returns the new value.
func (c *Client) Incr(table, rowKey, column string, delta float64) (float64, error) {
	body := map[string]any{
		"column": column,
		"delta":  delta,
	}
	var resp struct {
		Value float64 `json:"value"`
	}
	err := c.postJSON(apiPath(table, "rows", rowKey, "incr"), body, &resp)
	return resp.Value, err
}

// Append atomically appends to a string cell.
func (c *Client) Append(table, rowKey, column, value string) (string, error) {
	body := map[string]any{
		"column": column,
		"value":  value,
	}
	var resp struct {
		Value string `json:"value"`
	}
	err := c.postJSON(apiPath(table, "rows", rowKey, "append"), body, &resp)
	return resp.Value, err
}

// Prepend atomically prepends to a string cell.
func (c *Client) Prepend(table, rowKey, column, value string) (string, error) {
	body := map[string]any{
		"column": column,
		"value":  value,
	}
	var resp struct {
		Value string `json:"value"`
	}
	err := c.postJSON(apiPath(table, "rows", rowKey, "prepend"), body, &resp)
	return resp.Value, err
}

// CAS performs an atomic check-and-set on a row.
func (c *Client) CAS(table, rowKey string, cond *CASCondition, setCols Row) error {
	body := map[string]any{"setCols": setCols}
	if cond != nil {
		body["condition"] = cond
	}
	return c.postJSON(apiPath(table, "rows", rowKey, "cas"), body, nil)
}

// --- HTTP helpers ---

// apiPath joins escaped segments under /api/tables, so names may contain / or ?.
func apiPath(segs ...string) string {
	esc := make([]string, len(segs))
	for i, s := range segs {
		esc[i] = url.PathEscape(s)
	}
	return "/api/tables/" + strings.Join(esc, "/")
}

// apiError is returned when the server responds with a non-success status.
type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("quicktable: %d %s", e.Status, e.Msg)
}

func (c *Client) get(path string, dst any) error {
	resp, err := c.http.Get(c.base + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResp(resp, dst)
}

func (c *Client) del(path string, dst any) error {
	req, err := http.NewRequest(http.MethodDelete, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResp(resp, dst)
}

func (c *Client) putJSON(path string, body any, dst any) error {
	return c.doJSON(http.MethodPut, path, body, dst)
}

func (c *Client) postJSON(path string, body any, dst any) error {
	return c.doJSON(http.MethodPost, path, body, dst)
}

func (c *Client) doJSON(method, path string, body any, dst any) error {
	var r io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		return err
	}
	if r != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResp(resp, dst)
}

func decodeResp(resp *http.Response, dst any) error {
	if resp.StatusCode >= 400 {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		msg := errBody.Error
		if msg == "" {
			msg = resp.Status
		}
		return &apiError{Status: resp.StatusCode, Msg: msg}
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
