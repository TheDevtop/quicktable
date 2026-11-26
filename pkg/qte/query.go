package qte

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

// Common wide-column errors
var (
	ErrNonNumericValue = errors.New("cell value is not numeric")
	ErrNonStringValue  = errors.New("cell value is not a string")
	ErrConditionFailed = errors.New("check and put condition failed")
	ErrInvalidOp       = errors.New("unknown comparison operator")
)

// CompareOp represents comparison operators for column predicates and conditions.
type CompareOp string

const (
	OpEq       CompareOp = "="
	OpNeq      CompareOp = "!="
	OpGt       CompareOp = ">"
	OpGte      CompareOp = ">="
	OpLt       CompareOp = "<"
	OpLte      CompareOp = "<="
	OpContains CompareOp = "CONTAINS"
	OpPrefix   CompareOp = "PREFIX"
	OpExists   CompareOp = "EXISTS"
	OpMissing  CompareOp = "MISSING"
)

// ValidateOp ensures op is one of the known comparison operators.
func ValidateOp(op CompareOp) error {
	switch op {
	case OpEq, OpNeq, OpGt, OpGte, OpLt, OpLte, OpContains, OpPrefix, OpExists, OpMissing:
		return nil
	}
	return fmt.Errorf("%w: %q", ErrInvalidOp, op)
}

// Condition is a predicate on one column, used by query filters and CheckAndSet.
type Condition struct {
	Column string
	Op     CompareOp
	Value  any
}

// match reports whether the condition holds for a column value;
// exists is false when the row has no such column.
func (c Condition) match(val any, exists bool) bool {
	switch c.Op {
	case OpExists:
		return exists
	case OpMissing:
		return !exists
	}

	if !exists {
		return false
	}

	// Values of different kinds are unequal and unordered.
	cmp, ok := compareValues(val, c.Value)
	switch c.Op {
	case OpEq:
		return ok && cmp == 0
	case OpNeq:
		return !ok || cmp != 0
	case OpGt:
		return ok && cmp > 0
	case OpGte:
		return ok && cmp >= 0
	case OpLt:
		return ok && cmp < 0
	case OpLte:
		return ok && cmp <= 0
	case OpContains:
		sVal, ok1 := val.(string)
		sTarget, ok2 := c.Value.(string)
		return ok1 && ok2 && strings.Contains(sVal, sTarget)
	case OpPrefix:
		sVal, ok1 := val.(string)
		sTarget, ok2 := c.Value.(string)
		return ok1 && ok2 && strings.HasPrefix(sVal, sTarget)
	default:
		return false
	}
}

// compareValues compares two scalar values (numbers, strings, booleans).
// It reports false if the values are of kinds that cannot be compared.
func compareValues(a, b any) (int, bool) {
	if af, aOk := a.(float64); aOk {
		if bf, bOk := b.(float64); bOk {
			switch {
			case af < bf:
				return -1, true
			case af > bf:
				return 1, true
			default:
				return 0, true
			}
		}
	}

	if as, aOk := a.(string); aOk {
		if bs, bOk := b.(string); bOk {
			return strings.Compare(as, bs), true
		}
	}

	if ab, aOk := a.(bool); aOk {
		if bb, bOk := b.(bool); bOk {
			switch {
			case ab == bb:
				return 0, true
			case !ab:
				return -1, true
			default:
				return 1, true
			}
		}
	}

	return 0, false
}

// QueryRow represents a row returned by a query, containing its key and columns.
type QueryRow struct {
	Key     string `json:"rowKey"`
	Columns Row    `json:"columns"`
}

// QueryBuilder provides a fluent API for scanning and querying wide-column data.
type QueryBuilder struct {
	db         *DB
	table      string
	prefix     string
	startKey   string
	endKey     string
	groups     [][]Condition // a row matches if every condition of any group holds
	or         bool          // the next Where starts a new group
	selectCols []string
	desc       bool
	offset     int
	limit      int
}

// Query starts a new query builder on the specified table.
func (db *DB) Query(table string) *QueryBuilder {
	return &QueryBuilder{
		db:    db,
		table: table,
	}
}

// WithPrefix restricts the query to row keys starting with prefix.
func (q *QueryBuilder) WithPrefix(prefix string) *QueryBuilder {
	q.prefix = strings.ToLower(prefix)
	return q
}

// WithKeyRange restricts the query to row keys within [startKey, endKey] (inclusive).
func (q *QueryBuilder) WithKeyRange(startKey, endKey string) *QueryBuilder {
	if startKey != "" {
		q.startKey = strings.ToLower(startKey)
	}
	if endKey != "" {
		q.endKey = strings.ToLower(endKey)
	}
	return q
}

// Where adds a column comparison predicate (e.g. Where("age", OpGte, 18.0)).
// Predicates are ANDed within a group; see Or. EXISTS and MISSING ignore value.
func (q *QueryBuilder) Where(column string, op CompareOp, value any) *QueryBuilder {
	if len(q.groups) == 0 || q.or {
		q.groups = append(q.groups, nil)
		q.or = false
	}
	last := len(q.groups) - 1
	q.groups[last] = append(q.groups[last], Condition{Column: column, Op: op, Value: value})
	return q
}

// Or starts a new group of predicates. A row matches if all predicates of any group hold,
// so Where(a).Where(b).Or().Where(c) means (a AND b) OR c.
func (q *QueryBuilder) Or() *QueryBuilder {
	q.or = true
	return q
}

// Select projects only the specified columns in the returned rows.
// If not called or empty, all columns are returned.
func (q *QueryBuilder) Select(columns ...string) *QueryBuilder {
	q.selectCols = append(q.selectCols, columns...)
	return q
}

// OrderDesc returns results sorted in descending row key order.
func (q *QueryBuilder) OrderDesc() *QueryBuilder {
	q.desc = true
	return q
}

// Offset skips the first n matching rows.
func (q *QueryBuilder) Offset(n int) *QueryBuilder {
	if n > 0 {
		q.offset = n
	}
	return q
}

// Limit caps the maximum number of returned rows.
func (q *QueryBuilder) Limit(n int) *QueryBuilder {
	if n > 0 {
		q.limit = n
	}
	return q
}

// Execute runs the query and returns matching rows.
func (q *QueryBuilder) Execute() ([]QueryRow, error) {
	if err := q.validate(); err != nil {
		return nil, err
	}

	var results []QueryRow
	err := q.db.bdb.View(func(txn *badger.Txn) error {
		return q.scan(txn, func(rowKey string, row Row) {
			results = append(results, QueryRow{
				Key:     rowKey,
				Columns: q.projectColumns(row),
			})
		})
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// Delete atomically removes every matching row and returns how many were removed.
// Offset and Limit apply as in Execute.
func (q *QueryBuilder) Delete() (int, error) {
	if err := q.validate(); err != nil {
		return 0, err
	}

	n := 0
	err := q.db.bdb.Update(func(txn *badger.Txn) error {
		// Collect first: a read-write transaction allows no writes while its iterator is open.
		// A scanned row holds all its columns, so its cell keys need no second iteration.
		var keys [][]byte
		err := q.scan(txn, func(rowKey string, row Row) {
			for col := range row {
				keys = append(keys, encodeKey(q.table, rowKey, col))
			}
			n++
		})
		if err != nil {
			return err
		}
		for _, k := range keys {
			if err := txn.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Stats summarizes the numeric values of a column over matching rows.
type Stats struct {
	Count int     `json:"count"`
	Sum   float64 `json:"sum"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Avg   float64 `json:"avg"`
}

// Aggregate computes Stats over the numeric values of column in matching rows
// (ignoring Limit and Offset). Rows where column is missing or not a number are skipped.
func (q *QueryBuilder) Aggregate(column string) (Stats, error) {
	var s Stats
	if err := q.validate(); err != nil {
		return s, err
	}
	if err := ValidateColumn(column); err != nil {
		return s, err
	}

	err := q.db.bdb.View(func(txn *badger.Txn) error {
		return q.iterateRows(txn, func(rowKey string, row Row) bool {
			n, ok := row[column].(float64)
			if !ok || !q.matches(row) {
				return true
			}
			if s.Count == 0 || n < s.Min {
				s.Min = n
			}
			if s.Count == 0 || n > s.Max {
				s.Max = n
			}
			s.Count++
			s.Sum += n
			return true
		})
	})
	if err != nil {
		return Stats{}, err
	}
	if s.Count > 0 {
		s.Avg = s.Sum / float64(s.Count)
	}
	return s, nil
}

// Count returns the count of matching rows (ignoring Limit and Offset).
func (q *QueryBuilder) Count() (int, error) {
	if err := q.validate(); err != nil {
		return 0, err
	}

	count := 0
	err := q.db.bdb.View(func(txn *badger.Txn) error {
		return q.iterateRows(txn, func(rowKey string, row Row) bool {
			if q.matches(row) {
				count++
			}
			return true
		})
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// validate checks the table name and every predicate operator.
func (q *QueryBuilder) validate() error {
	if err := ValidateTable(q.table); err != nil {
		return err
	}
	for _, g := range q.groups {
		for _, c := range g {
			if err := validateCondition(&c); err != nil {
				return err
			}
		}
	}
	return nil
}

// scan calls fn for each matching row, applying Offset and Limit.
func (q *QueryBuilder) scan(txn *badger.Txn, fn func(rowKey string, row Row)) error {
	matched, taken := 0, 0
	return q.iterateRows(txn, func(rowKey string, row Row) bool {
		if !q.matches(row) {
			return true
		}
		matched++
		if matched <= q.offset {
			return true
		}
		fn(rowKey, row)
		taken++
		return q.limit == 0 || taken < q.limit
	})
}

// iterateRows scans table keys according to prefix, range, and ordering.
func (q *QueryBuilder) iterateRows(txn *badger.Txn, fn func(rowKey string, row Row) bool) error {
	tblPrefix := encodeTablePrefix(q.table)

	opts := badger.DefaultIteratorOptions
	opts.Prefix = tblPrefix
	opts.Reverse = q.desc
	it := txn.NewIterator(opts)
	defer it.Close()

	var seekKey []byte
	if q.desc {
		if q.endKey != "" {
			seekKey = encodeKey(q.table, q.endKey, "\xff")
		} else if q.prefix != "" {
			seekKey = encodeKey(q.table, q.prefix+"\xff", "\xff")
		} else {
			seekKey = encodeKey(q.table, "\xff\xff\xff\xff\xff\xff\xff\xff", "\xff")
		}
	} else {
		if q.startKey != "" {
			seekKey = encodeRowPrefix(q.table, q.startKey)
		} else if q.prefix != "" {
			seekKey = encodeRowPrefix(q.table, q.prefix)
		} else {
			seekKey = tblPrefix
		}
	}

	var currentRowKey string
	var currentRow Row

	for it.Seek(seekKey); it.ValidForPrefix(tblPrefix); it.Next() {
		item := it.Item()
		_, rk, col, err := decodeKey(item.Key())
		if err != nil {
			return err
		}

		// A key outside the range ends the scan once it lies in the scan direction.
		outPrefix := q.prefix != "" && !strings.HasPrefix(rk, q.prefix)
		before := (q.startKey != "" && rk < q.startKey) || (outPrefix && rk < q.prefix)
		after := (q.endKey != "" && rk > q.endKey) || (outPrefix && rk > q.prefix)
		if before || after {
			if (q.desc && before) || (!q.desc && after) {
				break
			}
			continue
		}

		if currentRowKey != "" && rk != currentRowKey {
			if !fn(currentRowKey, currentRow) {
				return nil
			}
			currentRow = nil
		}

		if currentRow == nil {
			currentRowKey = rk
			currentRow = make(Row)
		}

		currentRow[col], err = readValue(item)
		if err != nil {
			return err
		}
	}

	if currentRow != nil {
		fn(currentRowKey, currentRow)
	}
	return nil
}

// matches reports whether a row satisfies any predicate group; with no predicates every row matches.
func (q *QueryBuilder) matches(row Row) bool {
	if len(q.groups) == 0 {
		return true
	}
	for _, g := range q.groups {
		if matchesAll(g, row) {
			return true
		}
	}
	return false
}

func matchesAll(conds []Condition, row Row) bool {
	for _, c := range conds {
		val, exists := row[c.Column]
		if !c.match(val, exists) {
			return false
		}
	}
	return true
}

// projectColumns returns the selected columns of a row.
// The row itself is returned when nothing is selected, as each scanned row is a fresh map.
func (q *QueryBuilder) projectColumns(cols Row) Row {
	if len(q.selectCols) == 0 {
		return cols
	}

	out := make(Row, len(q.selectCols))
	for _, c := range q.selectCols {
		if v, ok := cols[c]; ok {
			out[c] = v
		}
	}
	return out
}

// ============================================================================
// Wide-Column Operations (Batch, Incr, CAS)
// ============================================================================

// BatchMutation represents a row mutation in a BatchWrite operation.
type BatchMutation struct {
	RowKey  string
	SetCols Row      // Columns to set (can be nil/empty if Deleting)
	DelCols []string // Specific columns to delete
	Delete  bool     // If true, deletes the entire row
}

// BatchWrite applies multiple row mutations atomically in a single transaction.
func (db *DB) BatchWrite(table string, mutations []BatchMutation) error {
	if err := ValidateTable(table); err != nil {
		return err
	}
	if len(mutations) == 0 {
		return nil
	}

	return db.bdb.Update(func(txn *badger.Txn) error {
		for _, m := range mutations {
			if err := ValidateRowKey(m.RowKey); err != nil {
				return err
			}
			rk := strings.ToLower(m.RowKey)

			if m.Delete {
				if err := deleteRow(txn, encodeRowPrefix(table, rk)); err != nil {
					return err
				}
				continue
			}

			for _, col := range m.DelCols {
				if err := ValidateColumn(col); err != nil {
					return err
				}
				if err := txn.Delete(encodeKey(table, rk, col)); err != nil {
					return err
				}
			}

			for col, val := range m.SetCols {
				if err := ValidateColumn(col); err != nil {
					return err
				}
				enc, err := encodeValue(val)
				if err != nil {
					return fmt.Errorf("row %s column %s: %w", m.RowKey, col, err)
				}
				if err := txn.Set(encodeKey(table, rk, col), enc); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// BatchGetRows retrieves multiple rows by their row keys. Missing rows are omitted from the map.
func (db *DB) BatchGetRows(table string, rowKeys []string) (map[string]Row, error) {
	if err := ValidateTable(table); err != nil {
		return nil, err
	}

	result := make(map[string]Row)
	err := db.bdb.View(func(txn *badger.Txn) error {
		for _, rk := range rowKeys {
			prefix, err := rowPrefix(table, rk)
			if err != nil {
				return err
			}
			row, err := readRow(txn, prefix)
			if err != nil {
				return err
			}
			if row != nil {
				result[strings.ToLower(rk)] = row
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// BatchDeleteRows deletes multiple rows by their keys atomically.
func (db *DB) BatchDeleteRows(table string, rowKeys []string) error {
	if err := ValidateTable(table); err != nil {
		return err
	}
	if len(rowKeys) == 0 {
		return nil
	}

	return db.bdb.Update(func(txn *badger.Txn) error {
		for _, rk := range rowKeys {
			prefix, err := rowPrefix(table, rk)
			if err != nil {
				return err
			}
			if err := deleteRow(txn, prefix); err != nil {
				return err
			}
		}
		return nil
	})
}

// updateCell atomically replaces a cell with fn(cur) and returns the new value.
// cur is nil if the cell does not exist.
func (db *DB) updateCell(table, rowKey, column string, fn func(cur any) (any, error)) (any, error) {
	k, err := cellKey(table, rowKey, column)
	if err != nil {
		return nil, err
	}

	var val any
	err = db.bdb.Update(func(txn *badger.Txn) error {
		var cur any
		item, err := txn.Get(k)
		if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		if err == nil {
			if cur, err = readValue(item); err != nil {
				return err
			}
		}

		if val, err = fn(cur); err != nil {
			return err
		}
		enc, err := encodeValue(val)
		if err != nil {
			return err
		}
		return txn.Set(k, enc)
	})
	if err != nil {
		return nil, err
	}
	return val, nil
}

// Incr atomically adds delta to a numeric cell and returns the new value.
// If the cell does not exist, it is initialized to delta.
func (db *DB) Incr(table, rowKey, column string, delta float64) (float64, error) {
	val, err := db.updateCell(table, rowKey, column, func(cur any) (any, error) {
		if cur == nil {
			return delta, nil
		}
		n, ok := cur.(float64)
		if !ok {
			return nil, ErrNonNumericValue
		}
		return n + delta, nil
	})
	if err != nil {
		return 0, err
	}
	return val.(float64), nil
}

// AppendCell atomically appends suffix to a string cell and returns the result.
// If the cell does not exist, it is initialized with suffix.
func (db *DB) AppendCell(table, rowKey, column, suffix string) (string, error) {
	return db.modifyString(table, rowKey, column, suffix, false)
}

// PrependCell atomically prepends prefix to a string cell and returns the result.
// If the cell does not exist, it is initialized with prefix.
func (db *DB) PrependCell(table, rowKey, column, prefix string) (string, error) {
	return db.modifyString(table, rowKey, column, prefix, true)
}

func (db *DB) modifyString(table, rowKey, column, s string, prepend bool) (string, error) {
	val, err := db.updateCell(table, rowKey, column, func(cur any) (any, error) {
		if cur == nil {
			return s, nil
		}
		str, ok := cur.(string)
		if !ok {
			return nil, ErrNonStringValue
		}
		if prepend {
			return s + str, nil
		}
		return str + s, nil
	})
	if err != nil {
		return "", err
	}
	return val.(string), nil
}

// CheckAndSet atomically verifies that the condition holds on the row before applying mutations.
// If the condition fails, it returns ErrConditionFailed.
// If cond is nil, updates proceed unconditionally.
func (db *DB) CheckAndSet(table, rowKey string, cond *Condition, setCols Row) error {
	if err := ValidateTable(table); err != nil {
		return err
	}
	if err := ValidateRowKey(rowKey); err != nil {
		return err
	}
	if len(setCols) == 0 && cond == nil {
		return nil
	}
	if err := validateCondition(cond); err != nil {
		return err
	}

	normKey := strings.ToLower(rowKey)
	entries, err := encodeRow(table, normKey, setCols)
	if err != nil {
		return err
	}

	return db.bdb.Update(func(txn *badger.Txn) error {
		if err := checkCondition(txn, table, normKey, cond); err != nil {
			return err
		}
		return writeEntries(txn, entries)
	})
}

// CheckAndDelete atomically verifies that the condition holds on the row before deleting
// delCols, or the whole row if delCols is empty.
// If the condition fails, it returns ErrConditionFailed.
// If cond is nil, the delete proceeds unconditionally.
func (db *DB) CheckAndDelete(table, rowKey string, cond *Condition, delCols []string) error {
	prefix, err := rowPrefix(table, rowKey)
	if err != nil {
		return err
	}
	for _, col := range delCols {
		if err := ValidateColumn(col); err != nil {
			return err
		}
	}
	if err := validateCondition(cond); err != nil {
		return err
	}
	normKey := strings.ToLower(rowKey)

	return db.bdb.Update(func(txn *badger.Txn) error {
		if err := checkCondition(txn, table, normKey, cond); err != nil {
			return err
		}
		if len(delCols) == 0 {
			return deleteRow(txn, prefix)
		}
		for _, col := range delCols {
			if err := txn.Delete(encodeKey(table, normKey, col)); err != nil {
				return err
			}
		}
		return nil
	})
}

// validateCondition checks the column and operator of a condition; nil is valid.
func validateCondition(cond *Condition) error {
	if cond == nil {
		return nil
	}
	if err := ValidateColumn(cond.Column); err != nil {
		return err
	}
	return ValidateOp(cond.Op)
}

// checkCondition returns ErrConditionFailed unless cond holds on the row; nil always holds.
func checkCondition(txn *badger.Txn, table, rowKey string, cond *Condition) error {
	if cond == nil {
		return nil
	}

	var cur any
	item, err := txn.Get(encodeKey(table, rowKey, cond.Column))
	if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	exists := err == nil
	if exists {
		if cur, err = readValue(item); err != nil {
			return err
		}
	}
	if !cond.match(cur, exists) {
		return ErrConditionFailed
	}
	return nil
}
