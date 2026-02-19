package main

/*
	Quicktable
	Engine dispatch function
*/

import (
	"github.com/TheDevtop/quicktable/pkg/engine"
	"github.com/TheDevtop/quicktable/pkg/shared"
)

// Ecast extracts the function out of the args slice and casts it to its type
func ecast[T any](args []any, index int) (T, error) {
	if len(args) < index+1 {
		return *new(T), shared.ErrInvalidCast
	}

	if val, ok := args[index].(T); !ok {
		return *new(T), shared.ErrInvalidCast
	} else {
		return val, nil
	}
}

// Dispatch performs a function lookup and applies the function to its arguments
func dispatch(fn string, args []any) (any, error) {
	switch fn {
	case shared.FnIndex:
		if a0, err := ecast[string](args, 0); err != nil {
			return nil, err
		} else {
			return engine.Index(dbPtr, a0)
		}
	case shared.FnIndexPrefixed:
		if a0, err := ecast[string](args, 0); err != nil {
			return nil, err
		} else {
			return engine.IndexPrefixed(dbPtr, a0), nil
		}
	case shared.FnInsert:
		if a0, err := ecast[string](args, 0); err != nil {
			return nil, err
		} else if a1, err := ecast[string](args, 1); err != nil {
			return nil, err
		} else {
			return engine.Insert(dbPtr, a0, a1)
		}
	case shared.FnInsertSelected:
		if a0, err := ecast[string](args, 0); err != nil {
			return nil, err
		} else if a1, err := ecast[map[string]string](args, 1); err != nil {
			return nil, err
		} else {
			return nil, engine.InsertSelected(dbPtr, a0, a1)
		}
	case shared.FnCopy:
		if a0, err := ecast[string](args, 0); err != nil {
			return nil, err
		} else if a1, err := ecast[string](args, 1); err != nil {
			return nil, err
		} else {
			return engine.Copy(dbPtr, a0, a1)
		}
	case shared.FnMove:
		if a0, err := ecast[string](args, 0); err != nil {
		} else if a1, err := ecast[string](args, 1); err != nil {
			return nil, err
		} else {
			return engine.Move(dbPtr, a0, a1)
		}
	case shared.FnDelete:
		if a0, err := ecast[string](args, 0); err != nil {
			return nil, err
		} else {
			return engine.Delete(dbPtr, a0)
		}
	case shared.FnDeleteSelected:
		if a0, err := ecast[string](args, 0); err != nil {
			return nil, err
		} else if a1, err := ecast[[]string](args, 1); err != nil {
			return nil, err
		} else {
			return nil, engine.DeleteSelected(dbPtr, a0, a1)
		}
	case shared.FnDeletePrefixed:
		if a0, err := ecast[string](args, 0); err != nil {
			return nil, err
		} else {
			return nil, engine.DeletePrefixed(dbPtr, a0)
		}
	case shared.FnQuery:
		if a0, err := ecast[string](args, 0); err != nil {
			return nil, err
		} else {
			return engine.Query(dbPtr, a0)
		}
	case shared.FnQuerySelected:
		if a0, err := ecast[string](args, 0); err != nil {
		} else if a1, err := ecast[[]string](args, 1); err != nil {
			return nil, err
		} else {
			return engine.QuerySelected(dbPtr, a0, a1)
		}
	case shared.FnQueryPrefixed:
		if a0, err := ecast[string](args, 0); err != nil {
			return nil, err
		} else {
			return engine.QueryPrefixed(dbPtr, a0)
		}
	}
	return nil, shared.ErrInvalidFn
}
