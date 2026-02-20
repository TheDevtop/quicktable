package client

import (
	"bytes"
	"io"
	"net/http"

	"github.com/TheDevtop/quicktable/pkg/shared"
)

const mimeJson = "application/json"

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

func (c *Conn) postQuery(body io.Reader) (*http.Response, error) {
	return http.Post(c.baseAddr+shared.RouteQuery, mimeJson, body)
}

func Open(addr string) (*Conn, error) {
	var (
		resp *http.Response
		err  error
		buf  []byte
		dbc  = new(Conn)
	)
	if resp, err = http.Get(addr + shared.RouteHealth); err != nil {
		return nil, err
	}
	if buf, err = io.ReadAll(resp.Body); err != nil {
		return nil, err
	}
	if string(buf) != shared.Signature {
		return nil, shared.ErrNoQuicktable
	}
	dbc.baseAddr = addr
	return dbc, nil
}
