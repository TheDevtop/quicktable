package qte

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

// Common wide-column errors
var (
	ErrNonNumericValue = errors.New("cell value is not numeric")
	ErrNonBinaryValue  = errors.New("cell value is not a string or byte slice")
	ErrConditionFailed = errors.New("check and put condition failed")
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

// FilterFunc is a function signature for custom row filtering.
type FilterFunc func(rowKey string, row Row) bool

// queryPredicate represents a condition on a column value.
type queryPredicate struct {
	column string
	op     CompareOp
	value  any
}

// QueryRow represents a row returned by a query, containing its key and columns.
type QueryRow struct {
	Key     string
	Columns Row
}

// QueryBuilder provides a fluent API for scanning and querying wide-column data.
type QueryBuilder struct {
	db         *DB
	table      string
	prefix     string
	startKey   string
	endKey     string
	predicates []queryPredicate
	customFn   FilterFunc
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

// Where adds a column comparison predicate (e.g. Where("age", OpGte, 18)).
func (q *QueryBuilder) Where(column string, op CompareOp, value any) *QueryBuilder {
	q.predicates = append(q.predicates, queryPredicate{
		column: column,
		op:     op,
		value:  value,
	})
	return q
}

// WhereHasColumn filters for rows that contain the specified column.
func (q *QueryBuilder) WhereHasColumn(column string) *QueryBuilder {
	return q.Where(column, OpExists, nil)
}

// WhereMissingColumn filters for rows that do not contain the specified column.
func (q *QueryBuilder) WhereMissingColumn(column string) *QueryBuilder {
	return q.Where(column, OpMissing, nil)
}

// WhereFunc adds a custom predicate function to filter rows.
func (q *QueryBuilder) WhereFunc(fn FilterFunc) *QueryBuilder {
	q.customFn = fn
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
	if err := ValidateTable(q.table); err != nil {
		return nil, err
	}

	var results []QueryRow
	matched := 0

	err := q.iterateRows(func(rowKey string, row Row) bool {
		if !q.matchesAll(rowKey, row) {
			return true
		}

		if matched < q.offset {
			matched++
			return true
		}
		matched++

		results = append(results, QueryRow{
			Key:     rowKey,
			Columns: q.projectColumns(row),
		})

		if q.limit > 0 && len(results) >= q.limit {
			return false
		}
		return true
	})

	if err != nil {
		return nil, err
	}
	return results, nil
}

// First returns the first matching row or ErrRowNotFound if none match.
func (q *QueryBuilder) First() (QueryRow, error) {
	clone := *q
	clone.limit = 1
	rows, err := clone.Execute()
	if err != nil {
		return QueryRow{}, err
	}
	if len(rows) == 0 {
		return QueryRow{}, ErrRowNotFound
	}
	return rows[0], nil
}

// Count returns the count of matching rows (ignoring Limit and Offset).
func (q *QueryBuilder) Count() (int, error) {
	if err := ValidateTable(q.table); err != nil {
		return 0, err
	}

	count := 0
	err := q.iterateRows(func(rowKey string, row Row) bool {
		if q.matchesAll(rowKey, row) {
			count++
		}
		return true
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Delete deletes all rows matching the query predicates and returns the number of deleted rows.
func (q *QueryBuilder) Delete() (int, error) {
	if err := ValidateTable(q.table); err != nil {
		return 0, err
	}

	var toDelete []string
	matched := 0

	err := q.iterateRows(func(rowKey string, row Row) bool {
		if !q.matchesAll(rowKey, row) {
			return true
		}

		if matched < q.offset {
			matched++
			return true
		}
		matched++

		toDelete = append(toDelete, rowKey)
		if q.limit > 0 && len(toDelete) >= q.limit {
			return false
		}
		return true
	})
	if err != nil {
		return 0, err
	}

	if len(toDelete) == 0 {
		return 0, nil
	}

	err = q.db.BatchDeleteRows(q.table, toDelete)
	if err != nil {
		return 0, err
	}
	return len(toDelete), nil
}

// iterateRows scans table keys according to prefix, range, and ordering.
func (q *QueryBuilder) iterateRows(fn func(rowKey string, row Row) bool) error {
	tblPrefix := encodeTablePrefix(q.table)

	return q.db.bdb.View(func(txn *badger.Txn) error {
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

			// Boundary checks
			if !q.desc {
				if q.endKey != "" && rk > q.endKey {
					break
				}
				if q.prefix != "" && !strings.HasPrefix(rk, q.prefix) {
					if rk > q.prefix {
						break
					}
					continue
				}
				if q.startKey != "" && rk < q.startKey {
					continue
				}
			} else {
				if q.startKey != "" && rk < q.startKey {
					break
				}
				if q.prefix != "" && !strings.HasPrefix(rk, q.prefix) {
					if rk < q.prefix {
						break
					}
					continue
				}
				if q.endKey != "" && rk > q.endKey {
					continue
				}
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

			err = item.Value(func(val []byte) error {
				decoded, err := decodeValue(val)
				if err != nil {
					return err
				}
				currentRow[col] = decoded
				return nil
			})
			if err != nil {
				return err
			}
		}

		if currentRow != nil {
			fn(currentRowKey, currentRow)
		}
		return nil
	})
}

func (q *QueryBuilder) matchesAll(rowKey string, cols Row) bool {
	for _, p := range q.predicates {
		if !evalPredicate(cols, p) {
			return false
		}
	}
	if q.customFn != nil && !q.customFn(rowKey, cols) {
		return false
	}
	return true
}

func (q *QueryBuilder) projectColumns(cols Row) Row {
	if len(q.selectCols) == 0 {
		out := make(Row, len(cols))
		for k, v := range cols {
			out[k] = v
		}
		return out
	}

	out := make(Row, len(q.selectCols))
	for _, c := range q.selectCols {
		if v, ok := cols[c]; ok {
			out[c] = v
		}
	}
	return out
}

func evalPredicate(cols Row, p queryPredicate) bool {
	val, exists := cols[p.column]

	switch p.op {
	case OpExists:
		return exists
	case OpMissing:
		return !exists
	}

	if !exists {
		return false
	}

	switch p.op {
	case OpEq:
		return compareValues(val, p.value) == 0
	case OpNeq:
		return compareValues(val, p.value) != 0
	case OpGt:
		return compareValues(val, p.value) > 0
	case OpGte:
		return compareValues(val, p.value) >= 0
	case OpLt:
		return compareValues(val, p.value) < 0
	case OpLte:
		return compareValues(val, p.value) <= 0
	case OpContains:
		sVal, ok1 := val.(string)
		sTarget, ok2 := p.value.(string)
		return ok1 && ok2 && strings.Contains(sVal, sTarget)
	case OpPrefix:
		sVal, ok1 := val.(string)
		sTarget, ok2 := p.value.(string)
		return ok1 && ok2 && strings.HasPrefix(sVal, sTarget)
	default:
		return false
	}
}

// compareValues compares two scalar values (numbers, strings, booleans, byte slices).
func compareValues(a, b any) int {
	if af, aOk := toFloat64(a); aOk {
		if bf, bOk := toFloat64(b); bOk {
			switch {
			case af < bf:
				return -1
			case af > bf:
				return 1
			default:
				return 0
			}
		}
	}

	if as, aOk := a.(string); aOk {
		if bs, bOk := b.(string); bOk {
			return strings.Compare(as, bs)
		}
	}

	if ab, aOk := a.(bool); aOk {
		if bb, bOk := b.(bool); bOk {
			if ab == bb {
				return 0
			}
			if !ab && bb {
				return -1
			}
			return 1
		}
	}

	if ab, aOk := a.([]byte); aOk {
		if bb, bOk := b.([]byte); bOk {
			return bytes.Compare(ab, bb)
		}
	}

	if reflect.DeepEqual(a, b) {
		return 0
	}
	return -1
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		return int64(n), true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		return int64(n), true
	case float32:
		if float32(int64(n)) == n {
			return int64(n), true
		}
		return 0, false
	case float64:
		if float64(int64(n)) == n {
			return int64(n), true
		}
		return 0, false
	default:
		return 0, false
	}
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
				prefix := encodeRowPrefix(table, rk)
				it := txn.NewIterator(badger.IteratorOptions{Prefix: prefix})
				var delKeys [][]byte
				for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
					delKeys = append(delKeys, it.Item().KeyCopy(nil))
				}
				it.Close()
				for _, k := range delKeys {
					if err := txn.Delete(k); err != nil {
						return err
					}
				}
				continue
			}

			for _, col := range m.DelCols {
				if err := ValidateColumn(col); err != nil {
					return err
				}
				k := encodeKey(table, rk, col)
				if err := txn.Delete(k); err != nil {
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
				k := encodeKey(table, rk, col)
				if err := txn.Set(k, enc); err != nil {
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
	if len(rowKeys) == 0 {
		return make(map[string]Row), nil
	}

	result := make(map[string]Row)

	err := db.bdb.View(func(txn *badger.Txn) error {
		for _, rk := range rowKeys {
			if err := ValidateRowKey(rk); err != nil {
				return err
			}
			normKey := strings.ToLower(rk)
			prefix := encodeRowPrefix(table, normKey)

			opts := badger.DefaultIteratorOptions
			opts.Prefix = prefix
			it := txn.NewIterator(opts)

			var row Row
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				item := it.Item()
				_, _, col, err := decodeKey(item.Key())
				if err != nil {
					it.Close()
					return err
				}
				err = item.Value(func(val []byte) error {
					decoded, err := decodeValue(val)
					if err != nil {
						return err
					}
					if row == nil {
						row = make(Row)
					}
					row[col] = decoded
					return nil
				})
				if err != nil {
					it.Close()
					return err
				}
			}
			it.Close()

			if row != nil {
				result[normKey] = row
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
			if err := ValidateRowKey(rk); err != nil {
				return err
			}
			normKey := strings.ToLower(rk)
			prefix := encodeRowPrefix(table, normKey)

			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			opts.Prefix = prefix
			it := txn.NewIterator(opts)

			var keysToDelete [][]byte
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				keysToDelete = append(keysToDelete, it.Item().KeyCopy(nil))
			}
			it.Close()

			for _, k := range keysToDelete {
				if err := txn.Delete(k); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// IncrCell atomically adds delta to an integer cell value and returns the new value.
// If the cell does not exist, it is initialized to delta.
func (db *DB) IncrCell(table, rowKey, column string, delta int64) (int64, error) {
	if err := ValidateTable(table); err != nil {
		return 0, err
	}
	if err := ValidateRowKey(rowKey); err != nil {
		return 0, err
	}
	if err := ValidateColumn(column); err != nil {
		return 0, err
	}

	normKey := strings.ToLower(rowKey)
	k := encodeKey(table, normKey, column)
	var newVal int64

	err := db.bdb.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(k)
		if errors.Is(err, badger.ErrKeyNotFound) {
			newVal = delta
		} else if err != nil {
			return err
		} else {
			var curVal int64
			err = item.Value(func(raw []byte) error {
				dec, err := decodeValue(raw)
				if err != nil {
					return err
				}
				num, ok := toInt64(dec)
				if !ok {
					return ErrNonNumericValue
				}
				curVal = num
				return nil
			})
			if err != nil {
				return err
			}
			newVal = curVal + delta
		}

		enc, err := encodeValue(newVal)
		if err != nil {
			return err
		}
		return txn.Set(k, enc)
	})

	if err != nil {
		return 0, err
	}
	return newVal, nil
}

// IncrCellFloat atomically adds delta to a float64 cell value and returns the new value.
// If the cell does not exist, it is initialized to delta.
func (db *DB) IncrCellFloat(table, rowKey, column string, delta float64) (float64, error) {
	if err := ValidateTable(table); err != nil {
		return 0, err
	}
	if err := ValidateRowKey(rowKey); err != nil {
		return 0, err
	}
	if err := ValidateColumn(column); err != nil {
		return 0, err
	}

	normKey := strings.ToLower(rowKey)
	k := encodeKey(table, normKey, column)
	var newVal float64

	err := db.bdb.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(k)
		if errors.Is(err, badger.ErrKeyNotFound) {
			newVal = delta
		} else if err != nil {
			return err
		} else {
			var curVal float64
			err = item.Value(func(raw []byte) error {
				dec, err := decodeValue(raw)
				if err != nil {
					return err
				}
				num, ok := toFloat64(dec)
				if !ok {
					return ErrNonNumericValue
				}
				curVal = num
				return nil
			})
			if err != nil {
				return err
			}
			newVal = curVal + delta
		}

		enc, err := encodeValue(newVal)
		if err != nil {
			return err
		}
		return txn.Set(k, enc)
	})

	if err != nil {
		return 0, err
	}
	return newVal, nil
}

// AppendCell atomically appends a string or byte slice suffix to an existing cell value.
// If the cell does not exist, it is initialized with suffix.
// Returns the resulting concatenated value (string or []byte matching the type of suffix).
func (db *DB) AppendCell(table, rowKey, column string, suffix any) (any, error) {
	return db.modifyStringOrBytes(table, rowKey, column, suffix, false)
}

// PrependCell atomically prepends a string or byte slice prefix to an existing cell value.
// If the cell does not exist, it is initialized with prefixVal.
// Returns the resulting concatenated value (string or []byte matching the type of prefixVal).
func (db *DB) PrependCell(table, rowKey, column string, prefixVal any) (any, error) {
	return db.modifyStringOrBytes(table, rowKey, column, prefixVal, true)
}

func (db *DB) modifyStringOrBytes(table, rowKey, column string, val any, prepend bool) (any, error) {
	if err := ValidateTable(table); err != nil {
		return nil, err
	}
	if err := ValidateRowKey(rowKey); err != nil {
		return nil, err
	}
	if err := ValidateColumn(column); err != nil {
		return nil, err
	}

	normKey := strings.ToLower(rowKey)
	k := encodeKey(table, normKey, column)
	var finalVal any

	err := db.bdb.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(k)
		if errors.Is(err, badger.ErrKeyNotFound) {
			finalVal = val
		} else if err != nil {
			return err
		} else {
			var curVal any
			err = item.Value(func(raw []byte) error {
				dec, err := decodeValue(raw)
				if err != nil {
					return err
				}
				curVal = dec
				return nil
			})
			if err != nil {
				return err
			}

			switch v := val.(type) {
			case string:
				curStr, ok := curVal.(string)
				if !ok {
					return ErrNonBinaryValue
				}
				if prepend {
					finalVal = v + curStr
				} else {
					finalVal = curStr + v
				}
			case []byte:
				curBytes, ok := curVal.([]byte)
				if !ok {
					return ErrNonBinaryValue
				}
				buf := make([]byte, len(curBytes)+len(v))
				if prepend {
					copy(buf, v)
					copy(buf[len(v):], curBytes)
				} else {
					copy(buf, curBytes)
					copy(buf[len(curBytes):], v)
				}
				finalVal = buf
			default:
				return ErrNonBinaryValue
			}
		}

		enc, err := encodeValue(finalVal)
		if err != nil {
			return err
		}
		return txn.Set(k, enc)
	})

	if err != nil {
		return nil, err
	}
	return finalVal, nil
}

// Condition represents a predicate check on a row's column for CheckAndSet / CAS operations.
type Condition struct {
	Column string
	Op     CompareOp
	Value  any
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

	normKey := strings.ToLower(rowKey)

	// Pre-validate set columns
	type kv struct {
		key []byte
		val []byte
	}
	pairs := make([]kv, 0, len(setCols))
	for col, val := range setCols {
		if err := ValidateColumn(col); err != nil {
			return err
		}
		enc, err := encodeValue(val)
		if err != nil {
			return fmt.Errorf("column %q: %w", col, err)
		}
		pairs = append(pairs, kv{
			key: encodeKey(table, normKey, col),
			val: enc,
		})
	}

	return db.bdb.Update(func(txn *badger.Txn) error {
		if cond != nil {
			if err := ValidateColumn(cond.Column); err != nil {
				return err
			}
			ck := encodeKey(table, normKey, cond.Column)
			item, err := txn.Get(ck)

			var exists bool
			var curVal any

			if err == nil {
				exists = true
				err = item.Value(func(raw []byte) error {
					dec, err := decodeValue(raw)
					if err != nil {
						return err
					}
					curVal = dec
					return nil
				})
				if err != nil {
					return err
				}
			} else if !errors.Is(err, badger.ErrKeyNotFound) {
				return err
			}

			// Evaluate condition
			mockRow := Row{}
			if exists {
				mockRow[cond.Column] = curVal
			}
			pred := queryPredicate{
				column: cond.Column,
				op:     cond.Op,
				value:  cond.Value,
			}
			if !evalPredicate(mockRow, pred) {
				return ErrConditionFailed
			}
		}

		for _, p := range pairs {
			if err := txn.Set(p.key, p.val); err != nil {
				return err
			}
		}
		return nil
	})
}
