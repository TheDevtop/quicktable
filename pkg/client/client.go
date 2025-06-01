package client

import (
	"errors"
	"io"
	"net/http"

	"github.com/TheDevtop/quicktable/pkg/shared"
)

func formatPath(base string, route string) string {
	return base + route
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
	if string(buf) != "\"pong!\"" {
		return nil, errors.New("Could not connect to Quicktable")
	}
	dbc.baseAddr = addr
	return dbc, nil
}
