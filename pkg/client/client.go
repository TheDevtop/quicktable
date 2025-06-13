package client

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"github.com/TheDevtop/quicktable/pkg/shared"
)

const mimeJson = "application/json"

func formatPath(base string, route string) string {
	return base + route
}

func toBody(object any) (io.Reader, error) {
	var (
		body = bytes.NewBuffer(make([]byte, 0, 64))
		err  error
	)
	if err = shared.EncodeStream(body, object); err != nil {
		return nil, err
	}
	return body, nil
}

func Open(addr string) (*Conn, error) {
	var (
		resp *http.Response
		err  error
		buf  []byte
		dbc  = new(Conn)
	)
	if resp, err = http.Get(formatPath(addr, shared.RoutePing)); err != nil {
		return nil, err
	}
	if buf, err = io.ReadAll(resp.Body); err != nil {
		return nil, err
	}
	if string(buf) != shared.Signature {
		return nil, errors.New("Could not connect to Quicktable")
	}
	dbc.baseAddr = addr
	return dbc, nil
}
