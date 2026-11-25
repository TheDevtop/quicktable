package qte

/*
	Quicktable Wide-column engine
	Concept Hierarchy: Table -> RowKey -> Column -> Value
*/

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/dgraph-io/badger/v4"
)

var (
	ErrInvalidTable   = errors.New("table name must be non-empty and contain no whitespace")
	ErrInvalidRowKey  = errors.New("row key must be an 8-digit hexadecimal string")
	ErrInvalidColumn  = errors.New("column name must be non-empty and contain no whitespace")
	ErrUnsupportedVal = errors.New("value must be a scalar (int, int64, uint64, float64, bool, string, []byte)")
	ErrRowNotFound    = errors.New("row not found")
	ErrColumnNotFound = errors.New("column not found")
	ErrCorruptData    = errors.New("corrupted binary data encountered in storage")
)

// Key separator byte (0x00) used to form composite keys safely because
// Table, RowKey, and Column are guaranteed not to contain null bytes or whitespace.
const keySeparator byte = 0x00

// Scalar Type Tags for binary serialization
const (
	tagInt64   byte = 0x01
	tagUint64  byte = 0x02
	tagFloat64 byte = 0x03
	tagBool    byte = 0x04
	tagString  byte = 0x05
	tagBytes   byte = 0x06
)

// Row represents a mapping of Column names to their scalar Values for a single RowKey.
type Row map[string]any

// DB wraps a BadgerDB instance to provide wide-column database semantics.
type DB struct {
	bdb *badger.DB
}

// Options configures the wide-column database.
type Options struct {
	Dir      string        // Directory path for persistent storage
	InMemory bool          // Run completely in-memory (useful for testing)
	Logger   badger.Logger // Optional Badger-compatible logger (defaults to silent if nil)
}

// Open initializes and opens a new wide-column DB backed by Badger.
func Open(opts Options) (*DB, error) {
	bOpts := badger.DefaultOptions(opts.Dir)
	if opts.InMemory {
		bOpts = badger.DefaultOptions("").WithInMemory(true)
	}
	// Use provided logger, or silence Badger's verbose default logger if nil
	bOpts = bOpts.WithLogger(opts.Logger)

	bdb, err := badger.Open(bOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger db: %w", err)
	}

	return &DB{bdb: bdb}, nil
}

// SetLogger updates the active Badger logger on the underlying database options.
// Note: Badger reads the logger at Open time, so pass Options.Logger to Open() for full runtime logging.
func (db *DB) SetLogger(logger badger.Logger) {
	if db.bdb != nil {
		db.bdb.Opts().WithLogger(logger)
	}
}

// Close cleanly shuts down the underlying Badger database.
func (db *DB) Close() error {
	return db.bdb.Close()
}

// ============================================================================
// Validation Helpers
// ============================================================================

// hasWhitespace checks if a string contains any Unicode whitespace or null bytes.
func hasWhitespaceOrNull(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || r == 0 {
			return true
		}
	}
	return false
}

// ValidateTable ensures Table is non-empty and has no whitespace.
func ValidateTable(table string) error {
	if table == "" || hasWhitespaceOrNull(table) {
		return fmt.Errorf("%w: %q", ErrInvalidTable, table)
	}
	return nil
}

// ValidateRowKey ensures RowKey is strictly an 8-character hexadecimal string.
func ValidateRowKey(rowKey string) error {
	if len(rowKey) != 8 {
		return fmt.Errorf("%w: %q (length %d)", ErrInvalidRowKey, rowKey, len(rowKey))
	}
	for i := 0; i < len(rowKey); i++ {
		c := rowKey[i]
		isDigit := c >= '0' && c <= '9'
		isLowerHex := c >= 'a' && c <= 'f'
		isUpperHex := c >= 'A' && c <= 'F'
		if !isDigit && !isLowerHex && !isUpperHex {
			return fmt.Errorf("%w: %q (invalid char %q)", ErrInvalidRowKey, rowKey, c)
		}
	}
	return nil
}

// ValidateColumn ensures Column is non-empty and has no whitespace.
func ValidateColumn(column string) error {
	if column == "" || hasWhitespaceOrNull(column) {
		return fmt.Errorf("%w: %q", ErrInvalidColumn, column)
	}
	return nil
}

// GenerateRowKey generates a cryptographically random 8-digit lowercase hex string.
func GenerateRowKey() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("failed to generate random row key: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// ============================================================================
// Key Construction & Parsing
// ============================================================================

// encodeKey constructs a composite Badger key: <Table>\x00<RowKey>\x00<Column>
func encodeKey(table, rowKey, column string) []byte {
	buf := make([]byte, 0, len(table)+len(rowKey)+len(column)+2)
	buf = append(buf, table...)
	buf = append(buf, keySeparator)
	buf = append(buf, rowKey...)
	buf = append(buf, keySeparator)
	buf = append(buf, column...)
	return buf
}

// encodeRowPrefix constructs a prefix for scanning all columns of a row: <Table>\x00<RowKey>\x00
func encodeRowPrefix(table, rowKey string) []byte {
	buf := make([]byte, 0, len(table)+len(rowKey)+2)
	buf = append(buf, table...)
	buf = append(buf, keySeparator)
	buf = append(buf, rowKey...)
	buf = append(buf, keySeparator)
	return buf
}

// encodeTablePrefix constructs a prefix for scanning all rows in a table: <Table>\x00
func encodeTablePrefix(table string) []byte {
	buf := make([]byte, 0, len(table)+1)
	buf = append(buf, table...)
	buf = append(buf, keySeparator)
	return buf
}

// decodeKey extracts (table, rowKey, column) from a composite Badger key.
func decodeKey(raw []byte) (string, string, string, error) {
	parts := bytes.Split(raw, []byte{keySeparator})
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("%w: invalid key structure", ErrCorruptData)
	}
	return string(parts[0]), string(parts[1]), string(parts[2]), nil
}

// ============================================================================
// Scalar Value Serialization
// ============================================================================

// encodeValue serializes supported Go scalar types into a self-describing byte slice.
func encodeValue(val any) ([]byte, error) {
	if val == nil {
		return nil, fmt.Errorf("%w: nil is not allowed", ErrUnsupportedVal)
	}

	switch v := val.(type) {
	case int:
		buf := make([]byte, 9)
		buf[0] = tagInt64
		binary.BigEndian.PutUint64(buf[1:], uint64(v))
		return buf, nil
	case int8:
		buf := make([]byte, 9)
		buf[0] = tagInt64
		binary.BigEndian.PutUint64(buf[1:], uint64(v))
		return buf, nil
	case int16:
		buf := make([]byte, 9)
		buf[0] = tagInt64
		binary.BigEndian.PutUint64(buf[1:], uint64(v))
		return buf, nil
	case int32:
		buf := make([]byte, 9)
		buf[0] = tagInt64
		binary.BigEndian.PutUint64(buf[1:], uint64(v))
		return buf, nil
	case int64:
		buf := make([]byte, 9)
		buf[0] = tagInt64
		binary.BigEndian.PutUint64(buf[1:], uint64(v))
		return buf, nil

	case uint:
		buf := make([]byte, 9)
		buf[0] = tagUint64
		binary.BigEndian.PutUint64(buf[1:], uint64(v))
		return buf, nil
	case uint8:
		buf := make([]byte, 9)
		buf[0] = tagUint64
		binary.BigEndian.PutUint64(buf[1:], uint64(v))
		return buf, nil
	case uint16:
		buf := make([]byte, 9)
		buf[0] = tagUint64
		binary.BigEndian.PutUint64(buf[1:], uint64(v))
		return buf, nil
	case uint32:
		buf := make([]byte, 9)
		buf[0] = tagUint64
		binary.BigEndian.PutUint64(buf[1:], uint64(v))
		return buf, nil
	case uint64:
		buf := make([]byte, 9)
		buf[0] = tagUint64
		binary.BigEndian.PutUint64(buf[1:], v)
		return buf, nil

	case float32:
		buf := make([]byte, 9)
		buf[0] = tagFloat64
		binary.BigEndian.PutUint64(buf[1:], math.Float64bits(float64(v)))
		return buf, nil
	case float64:
		buf := make([]byte, 9)
		buf[0] = tagFloat64
		binary.BigEndian.PutUint64(buf[1:], math.Float64bits(v))
		return buf, nil

	case bool:
		buf := make([]byte, 2)
		buf[0] = tagBool
		if v {
			buf[1] = 1
		} else {
			buf[1] = 0
		}
		return buf, nil

	case string:
		buf := make([]byte, 1+len(v))
		buf[0] = tagString
		copy(buf[1:], v)
		return buf, nil

	case []byte:
		buf := make([]byte, 1+len(v))
		buf[0] = tagBytes
		copy(buf[1:], v)
		return buf, nil

	default:
		return nil, fmt.Errorf("%w: got %T", ErrUnsupportedVal, val)
	}
}

// decodeValue deserializes bytes stored in Badger back into their original Go scalar type.
func decodeValue(raw []byte) (any, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: empty value payload", ErrCorruptData)
	}

	tag := raw[0]
	payload := raw[1:]

	switch tag {
	case tagInt64:
		if len(payload) != 8 {
			return nil, fmt.Errorf("%w: invalid int64 length", ErrCorruptData)
		}
		return int64(binary.BigEndian.Uint64(payload)), nil

	case tagUint64:
		if len(payload) != 8 {
			return nil, fmt.Errorf("%w: invalid uint64 length", ErrCorruptData)
		}
		return binary.BigEndian.Uint64(payload), nil

	case tagFloat64:
		if len(payload) != 8 {
			return nil, fmt.Errorf("%w: invalid float64 length", ErrCorruptData)
		}
		return math.Float64frombits(binary.BigEndian.Uint64(payload)), nil

	case tagBool:
		if len(payload) != 1 {
			return nil, fmt.Errorf("%w: invalid bool length", ErrCorruptData)
		}
		return payload[0] != 0, nil

	case tagString:
		return string(payload), nil

	case tagBytes:
		out := make([]byte, len(payload))
		copy(out, payload)
		return out, nil

	default:
		return nil, fmt.Errorf("%w: unknown type tag 0x%x", ErrCorruptData, tag)
	}
}

// ============================================================================
// Core Wide-Column Operations (CRUD & Scans)
// ============================================================================

// SetCell writes a single scalar value at (Table -> RowKey -> Column).
func (db *DB) SetCell(table, rowKey, column string, value any) error {
	if err := ValidateTable(table); err != nil {
		return err
	}
	if err := ValidateRowKey(rowKey); err != nil {
		return err
	}
	if err := ValidateColumn(column); err != nil {
		return err
	}

	encodedVal, err := encodeValue(value)
	if err != nil {
		return err
	}

	// Normalize RowKey to lowercase hex for consistent storage lookups
	normalizedRowKey := strings.ToLower(rowKey)
	key := encodeKey(table, normalizedRowKey, column)

	return db.bdb.Update(func(txn *badger.Txn) error {
		return txn.Set(key, encodedVal)
	})
}

// InsertRow generates a new random 8-digit hex RowKey and atomically inserts the row,
// returning the newly generated RowKey.
func (db *DB) InsertRow(table string, columns Row) (string, error) {
	rowKey, err := GenerateRowKey()
	if err != nil {
		return "", err
	}
	if err := db.SetRow(table, rowKey, columns); err != nil {
		return "", err
	}
	return rowKey, nil
}

// SetRow atomically inserts or updates multiple columns for a given (Table -> RowKey).
func (db *DB) SetRow(table, rowKey string, columns Row) error {
	if err := ValidateTable(table); err != nil {
		return err
	}
	if err := ValidateRowKey(rowKey); err != nil {
		return err
	}
	if len(columns) == 0 {
		return nil
	}

	normalizedRowKey := strings.ToLower(rowKey)

	// Pre-validate and encode all entries before starting the write transaction
	type kvPair struct {
		key []byte
		val []byte
	}
	entries := make([]kvPair, 0, len(columns))

	for col, val := range columns {
		if err := ValidateColumn(col); err != nil {
			return err
		}
		encodedVal, err := encodeValue(val)
		if err != nil {
			return fmt.Errorf("column %q: %w", col, err)
		}
		entries = append(entries, kvPair{
			key: encodeKey(table, normalizedRowKey, col),
			val: encodedVal,
		})
	}

	return db.bdb.Update(func(txn *badger.Txn) error {
		for _, entry := range entries {
			if err := txn.Set(entry.key, entry.val); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetCell retrieves a single scalar value at (Table -> RowKey -> Column).
func (db *DB) GetCell(table, rowKey, column string) (any, error) {
	if err := ValidateTable(table); err != nil {
		return nil, err
	}
	if err := ValidateRowKey(rowKey); err != nil {
		return nil, err
	}
	if err := ValidateColumn(column); err != nil {
		return nil, err
	}

	normalizedRowKey := strings.ToLower(rowKey)
	key := encodeKey(table, normalizedRowKey, column)

	var result any
	err := db.bdb.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return ErrColumnNotFound
		}
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			decoded, err := decodeValue(val)
			if err != nil {
				return err
			}
			result = decoded
			return nil
		})
	})

	return result, err
}

// GetRow retrieves all columns and their scalar values for a specific (Table -> RowKey).
func (db *DB) GetRow(table, rowKey string) (Row, error) {
	if err := ValidateTable(table); err != nil {
		return nil, err
	}
	if err := ValidateRowKey(rowKey); err != nil {
		return nil, err
	}

	normalizedRowKey := strings.ToLower(rowKey)
	prefix := encodeRowPrefix(table, normalizedRowKey)
	row := make(Row)

	err := db.bdb.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			_, _, col, err := decodeKey(item.Key())
			if err != nil {
				return err
			}

			err = item.Value(func(val []byte) error {
				decoded, err := decodeValue(val)
				if err != nil {
					return err
				}
				row[col] = decoded
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	if len(row) == 0 {
		return nil, ErrRowNotFound
	}
	return row, nil
}

// DeleteCell removes a single column from a row.
func (db *DB) DeleteCell(table, rowKey, column string) error {
	if err := ValidateTable(table); err != nil {
		return err
	}
	if err := ValidateRowKey(rowKey); err != nil {
		return err
	}
	if err := ValidateColumn(column); err != nil {
		return err
	}

	normalizedRowKey := strings.ToLower(rowKey)
	key := encodeKey(table, normalizedRowKey, column)

	return db.bdb.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

// DeleteRow atomically removes all columns associated with (Table -> RowKey).
func (db *DB) DeleteRow(table, rowKey string) error {
	if err := ValidateTable(table); err != nil {
		return err
	}
	if err := ValidateRowKey(rowKey); err != nil {
		return err
	}

	normalizedRowKey := strings.ToLower(rowKey)
	prefix := encodeRowPrefix(table, normalizedRowKey)

	return db.bdb.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // Key-only iteration for fast deletes
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		var keysToDelete [][]byte
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			keysToDelete = append(keysToDelete, it.Item().KeyCopy(nil))
		}

		for _, k := range keysToDelete {
			if err := txn.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListTables returns the names of all tables in the database, in sorted order.
// It iterates keys only and skips past each table's keyspace once found.
func (db *DB) ListTables() ([]string, error) {
	var tables []string

	err := db.bdb.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); {
			key := it.Item().Key()
			idx := bytes.IndexByte(key, keySeparator)
			if idx <= 0 {
				return fmt.Errorf("%w: invalid key structure", ErrCorruptData)
			}
			table := string(key[:idx])
			tables = append(tables, table)

			// Jump past every key of this table: <Table>\x01 sorts after all <Table>\x00...
			next := make([]byte, 0, len(table)+1)
			next = append(next, table...)
			next = append(next, keySeparator+1)
			it.Seek(next)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tables, nil
}

// ListRows returns the keys of all rows in a Table, in sorted order.
// It iterates keys only and skips past each row's columns once found.
func (db *DB) ListRows(table string) ([]string, error) {
	if err := ValidateTable(table); err != nil {
		return nil, err
	}

	prefix := encodeTablePrefix(table)
	var rows []string

	err := db.bdb.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); {
			_, rk, _, err := decodeKey(it.Item().Key())
			if err != nil {
				return err
			}
			rows = append(rows, rk)

			// Jump past every column of this row: <Table>\x00<RowKey>\x01 sorts after all <Table>\x00<RowKey>\x00...
			next := encodeRowPrefix(table, rk)
			next[len(next)-1] = keySeparator + 1
			it.Seek(next)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// DropTable removes every row and column belonging to a Table.
// Dropping a non-existent table is a no-op.
func (db *DB) DropTable(table string) error {
	if err := ValidateTable(table); err != nil {
		return err
	}
	return db.bdb.DropPrefix(encodeTablePrefix(table))
}

// ScanTable iterates through every row in a Table, invoking fn(rowKey, row).
// Returning false from fn stops the scan early.
func (db *DB) ScanTable(table string, fn func(rowKey string, row Row) bool) error {
	if err := ValidateTable(table); err != nil {
		return err
	}

	prefix := encodeTablePrefix(table)

	return db.bdb.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		var currentRowKey string
		var currentRow Row

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			_, rk, col, err := decodeKey(item.Key())
			if err != nil {
				return err
			}

			// When RowKey changes, emit the completed previous row
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

		// Emit final buffered row
		if currentRow != nil {
			fn(currentRowKey, currentRow)
		}

		return nil
	})
}
