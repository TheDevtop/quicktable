package server

import (
	"bytes"
	"encoding/gob"

	"github.com/TheDevtop/quicktable/pkg/shared/core"
)

func encodeList(list core.List) ([]byte, error) {
	var (
		buf bytes.Buffer
		enc = gob.NewEncoder(&buf)
		err error
	)
	if err = enc.Encode(list); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeList(buf []byte) (core.List, error) {
	var (
		ibuf = bytes.NewBuffer(buf)
		list core.List
		dec  = gob.NewDecoder(ibuf)
		err  error
	)
	if err = dec.Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}
