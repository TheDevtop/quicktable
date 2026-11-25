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
	ErrUnsupportedVal = errors.New("value must be a scalar (float64, bool, string)")
	ErrRowNotFound    = errors.New("row not found")
	ErrColumnNotFound = errors.New("column not found")
	ErrCorruptData    = errors.New("corrupted binary data encountered in storage")
	ErrKeysExhausted  = errors.New("could not generate an unused row key")
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

// Close cleanly shuts down the underlying Badger database.
func (db *DB) Close() error {
	return db.bdb.Close()
}

// ============================================================================
// Validation Helpers
// ============================================================================

// hasWhitespaceOrNull checks if a string contains any Unicode whitespace or null bytes.
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

// cellKey validates a cell address and returns its storage key.
func cellKey(table, rowKey, column string) ([]byte, error) {
	if err := ValidateTable(table); err != nil {
		return nil, err
	}
	if err := ValidateRowKey(rowKey); err != nil {
		return nil, err
	}
	if err := ValidateColumn(column); err != nil {
		return nil, err
	}
	return encodeKey(table, strings.ToLower(rowKey), column), nil
}

// rowPrefix validates a row address and returns the prefix of its keys.
func rowPrefix(table, rowKey string) ([]byte, error) {
	if err := ValidateTable(table); err != nil {
		return nil, err
	}
	if err := ValidateRowKey(rowKey); err != nil {
		return nil, err
	}
	return encodeRowPrefix(table, strings.ToLower(rowKey)), nil
}

// ============================================================================
// Scalar Value Serialization
// ============================================================================

// encodeValue serializes a scalar into a self-describing byte slice.
// Only the scalar types JSON can carry are accepted.
func encodeValue(val any) ([]byte, error) {
	switch v := val.(type) {
	case float64:
		buf := make([]byte, 9)
		buf[0] = tagFloat64
		binary.BigEndian.PutUint64(buf[1:], math.Float64bits(v))
		return buf, nil
	case bool:
		buf := []byte{tagBool, 0}
		if v {
			buf[1] = 1
		}
		return buf, nil
	case string:
		return append([]byte{tagString}, v...), nil
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
	// Integer tags are only read: older versions stored them, all numbers are now float64.
	case tagInt64, tagUint64, tagFloat64:
		if len(payload) != 8 {
			return nil, fmt.Errorf("%w: invalid number length", ErrCorruptData)
		}
		n := binary.BigEndian.Uint64(payload)
		switch tag {
		case tagInt64:
			return float64(int64(n)), nil
		case tagUint64:
			return float64(n), nil
		}
		return math.Float64frombits(n), nil

	case tagBool:
		if len(payload) != 1 {
			return nil, fmt.Errorf("%w: invalid bool length", ErrCorruptData)
		}
		return payload[0] != 0, nil

	// Byte cells are only read: older versions stored them, all text is now string.
	case tagString, tagBytes:
		return string(payload), nil

	default:
		return nil, fmt.Errorf("%w: unknown type tag 0x%x", ErrCorruptData, tag)
	}
}

// readValue decodes the value of a stored cell.
func readValue(item *badger.Item) (any, error) {
	var val any
	err := item.Value(func(raw []byte) error {
		var err error
		val, err = decodeValue(raw)
		return err
	})
	return val, err
}

// readRow decodes every column stored under prefix, returning nil if there are none.
func readRow(txn *badger.Txn, prefix []byte) (Row, error) {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = prefix
	it := txn.NewIterator(opts)
	defer it.Close()

	var row Row
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		item := it.Item()
		_, _, col, err := decodeKey(item.Key())
		if err != nil {
			return nil, err
		}
		val, err := readValue(item)
		if err != nil {
			return nil, err
		}
		if row == nil {
			row = make(Row)
		}
		row[col] = val
	}
	return row, nil
}

// deleteRow deletes every column stored under prefix.
func deleteRow(txn *badger.Txn, prefix []byte) error {
	opts := badger.DefaultIteratorOptions
	opts.PrefetchValues = false
	opts.Prefix = prefix
	it := txn.NewIterator(opts)

	// Collect first: a read-write transaction allows no writes while its iterator is open.
	var keys [][]byte
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		keys = append(keys, it.Item().KeyCopy(nil))
	}
	it.Close()

	for _, k := range keys {
		if err := txn.Delete(k); err != nil {
			return err
		}
	}
	return nil
}

// ============================================================================
// Core Wide-Column Operations (CRUD & Scans)
// ============================================================================

// SetCell writes a single scalar value at (Table -> RowKey -> Column).
func (db *DB) SetCell(table, rowKey, column string, value any) error {
	key, err := cellKey(table, rowKey, column)
	if err != nil {
		return err
	}
	enc, err := encodeValue(value)
	if err != nil {
		return err
	}
	return db.bdb.Update(func(txn *badger.Txn) error {
		return txn.Set(key, enc)
	})
}

// insertTries bounds the search for an unused random row key.
const insertTries = 16

// InsertRow generates a new random 8-digit hex RowKey not yet used in Table and
// atomically inserts the row, returning the newly generated RowKey.
// With no columns nothing is stored, as a row exists only while it holds a cell.
func (db *DB) InsertRow(table string, columns Row) (string, error) {
	if err := ValidateTable(table); err != nil {
		return "", err
	}
	if len(columns) == 0 {
		return GenerateRowKey()
	}

	for i := 0; i < insertTries; i++ {
		rowKey, err := GenerateRowKey()
		if err != nil {
			return "", err
		}
		entries, err := encodeRow(table, rowKey, columns)
		if err != nil {
			return "", err
		}

		used := false
		err = db.bdb.Update(func(txn *badger.Txn) error {
			prefix := encodeRowPrefix(table, rowKey)
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			opts.Prefix = prefix
			it := txn.NewIterator(opts)
			it.Seek(prefix)
			used = it.ValidForPrefix(prefix)
			it.Close()
			if used {
				return nil
			}
			return writeEntries(txn, entries)
		})
		if err != nil {
			return "", err
		}
		if !used {
			return rowKey, nil
		}
	}
	return "", ErrKeysExhausted
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

	entries, err := encodeRow(table, strings.ToLower(rowKey), columns)
	if err != nil {
		return err
	}
	return db.bdb.Update(func(txn *badger.Txn) error {
		return writeEntries(txn, entries)
	})
}

// kvPair is an encoded cell ready to be written.
type kvPair struct {
	key []byte
	val []byte
}

// encodeRow validates and encodes every column of a row before any write starts.
func encodeRow(table, rowKey string, columns Row) ([]kvPair, error) {
	entries := make([]kvPair, 0, len(columns))
	for col, val := range columns {
		if err := ValidateColumn(col); err != nil {
			return nil, err
		}
		encodedVal, err := encodeValue(val)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", col, err)
		}
		entries = append(entries, kvPair{
			key: encodeKey(table, rowKey, col),
			val: encodedVal,
		})
	}
	return entries, nil
}

func writeEntries(txn *badger.Txn, entries []kvPair) error {
	for _, entry := range entries {
		if err := txn.Set(entry.key, entry.val); err != nil {
			return err
		}
	}
	return nil
}

// GetCell retrieves a single scalar value at (Table -> RowKey -> Column).
func (db *DB) GetCell(table, rowKey, column string) (any, error) {
	key, err := cellKey(table, rowKey, column)
	if err != nil {
		return nil, err
	}

	var val any
	err = db.bdb.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return ErrColumnNotFound
		}
		if err != nil {
			return err
		}
		val, err = readValue(item)
		return err
	})
	return val, err
}

// GetRow retrieves all columns and their scalar values for a specific (Table -> RowKey).
func (db *DB) GetRow(table, rowKey string) (Row, error) {
	prefix, err := rowPrefix(table, rowKey)
	if err != nil {
		return nil, err
	}

	var row Row
	err = db.bdb.View(func(txn *badger.Txn) error {
		var err error
		row, err = readRow(txn, prefix)
		return err
	})
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrRowNotFound
	}
	return row, nil
}

// DeleteCell removes a single column from a row.
func (db *DB) DeleteCell(table, rowKey, column string) error {
	key, err := cellKey(table, rowKey, column)
	if err != nil {
		return err
	}
	return db.bdb.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

// DeleteRow atomically removes all columns associated with (Table -> RowKey).
func (db *DB) DeleteRow(table, rowKey string) error {
	return db.BatchDeleteRows(table, []string{rowKey})
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
