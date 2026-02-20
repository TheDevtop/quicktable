package client

import (
	"io"
	"net/http"

	"github.com/TheDevtop/quicktable/pkg/dkey"
	"github.com/TheDevtop/quicktable/pkg/shared"
)

type Conn struct {
	baseAddr string
}

func (c *Conn) List(str string) []string {
	return dkey.Expand(str)
}

func (c *Conn) Key(list ...string) string {
	return dkey.Merge(list...)
}

func (c *Conn) Index(key string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   shared.FnIndex,
		Args: []any{key},
	}); err != nil {
		return "", err
	}

	// Post the query
	if resp, err = c.postQuery(body); err != nil {
		return "", err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[string]](resp.Body); err != nil {
		return "", err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return "", shared.ErrFormNotOk
	}
	return report.Data, nil
}

func (c *Conn) IndexPrefixed(key string) ([]string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[[]string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   shared.FnIndexPrefixed,
		Args: []any{key},
	}); err != nil {
		return nil, err
	}

	// Post the query
	if resp, err = c.postQuery(body); err != nil {
		return nil, err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[[]string]](resp.Body); err != nil {
		return nil, err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return nil, shared.ErrFormNotOk
	}
	return report.Data, nil
}

func (c *Conn) Insert(key, value string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   shared.FnInsert,
		Args: []any{key, value},
	}); err != nil {
		return "", err
	}

	// Post the query
	if resp, err = c.postQuery(body); err != nil {
		return "", err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[string]](resp.Body); err != nil {
		return "", err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return "", shared.ErrFormNotOk
	}
	return report.Data, nil
}

func (c *Conn) InsertSelected(key string, pairs map[string]string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   shared.FnInsertSelected,
		Args: []any{key, pairs},
	}); err != nil {
		return "", err
	}

	// Post the query
	if resp, err = c.postQuery(body); err != nil {
		return "", err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[string]](resp.Body); err != nil {
		return "", err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return "", shared.ErrFormNotOk
	}
	return report.Data, nil
}

func (c *Conn) Copy(key, dest string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   shared.FnCopy,
		Args: []any{key, dest},
	}); err != nil {
		return "", err
	}

	// Post the query
	if resp, err = c.postQuery(body); err != nil {
		return "", err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[string]](resp.Body); err != nil {
		return "", err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return "", shared.ErrFormNotOk
	}
	return report.Data, nil
}

func (c *Conn) Move(key string, dest string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   shared.FnMove,
		Args: []any{key, dest},
	}); err != nil {
		return "", err
	}

	// Post the query
	if resp, err = c.postQuery(body); err != nil {
		return "", err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[string]](resp.Body); err != nil {
		return "", err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return "", shared.ErrFormNotOk
	}
	return report.Data, nil
}

func (c *Conn) Delete(key string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   shared.FnDelete,
		Args: []any{key},
	}); err != nil {
		return "", err
	}

	// Post the query
	if resp, err = c.postQuery(body); err != nil {
		return "", err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[string]](resp.Body); err != nil {
		return "", err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return "", shared.ErrFormNotOk
	}
	return report.Data, nil
}

func (c *Conn) DeleteSelected(key string, keys []string) error {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   shared.FnDeleteSelected,
		Args: []any{key, keys},
	}); err != nil {
		return err
	}

	// Post the query
	if resp, err = c.postQuery(body); err != nil {
		return err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[string]](resp.Body); err != nil {
		return err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return shared.ErrFormNotOk
	}
	return nil
}

func (c *Conn) DeletePrefixed(key string) error {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   shared.FnDeletePrefixed,
		Args: []any{key},
	}); err != nil {
		return err
	}

	// Post the query
	if resp, err = c.postQuery(body); err != nil {
		return err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[string]](resp.Body); err != nil {
		return err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return shared.ErrFormNotOk
	}
	return nil
}

func (c *Conn) Query(key string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   shared.FnQuery,
		Args: []any{key},
	}); err != nil {
		return "", err
	}

	// Post the query
	if resp, err = c.postQuery(body); err != nil {
		return "", err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[string]](resp.Body); err != nil {
		return "", err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return "", shared.ErrFormNotOk
	}
	return report.Data, nil
}

func (c *Conn) QuerySelected(key string, keys []string) (map[string]string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[map[string]string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   shared.FnQuerySelected,
		Args: []any{key, keys},
	}); err != nil {
		return nil, err
	}

	// Post the query
	if resp, err = c.postQuery(body); err != nil {
		return nil, err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[map[string]string]](resp.Body); err != nil {
		return nil, err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return nil, shared.ErrFormNotOk
	}
	return report.Data, nil
}

func (c *Conn) QueryPrefixed(key string) (map[string]string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[map[string]string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   shared.FnQueryPrefixed,
		Args: []any{key},
	}); err != nil {
		return nil, err
	}

	// Post the query
	if resp, err = c.postQuery(body); err != nil {
		return nil, err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[map[string]string]](resp.Body); err != nil {
		return nil, err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return nil, shared.ErrFormNotOk
	}
	return report.Data, nil
}

func (c *Conn) Hash(key string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.FormResult[string]
	)

	// Construct a body
	if body, err = toBody(shared.FormQuery{
		Fn:   "",
		Args: []any{key},
	}); err != nil {
		return "", err
	}

	// Post the query
	if resp, err = http.Post(c.baseAddr+shared.RouteHash, mimeJson, body); err != nil {
		return "", err
	}

	// Decode the response
	if report, err = shared.DecodeStream[shared.FormResult[string]](resp.Body); err != nil {
		return "", err
	}

	// Check status
	if report.Status != shared.StatusOk {
		return "", shared.ErrFormNotOk
	}
	return report.Data, nil
}
