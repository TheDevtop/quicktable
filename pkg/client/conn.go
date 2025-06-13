package client

import (
	"io"
	"net/http"

	"github.com/TheDevtop/quicktable/pkg/shared"
	"github.com/TheDevtop/quicktable/pkg/shared/core"
)

type Conn struct {
	baseAddr string
}

func (c *Conn) List(str string) []string {
	return core.Expand(str)
}

func (c *Conn) Key(list ...string) string {
	return core.Merge(list...)
}

func (c *Conn) Index(keys []string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.Key]
	)
	if body, err = toBody(keys); err != nil {
		return "", err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteIndex), mimeJson, body); err != nil {
		return "", err
	}
	if report, err = shared.DecodeStream[shared.Report[core.Key]](resp.Body); err != nil {
		return "", err
	}
	return report.Data, nil
}

func (c *Conn) IndexRanged(keys []string) ([]string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.List]
	)
	if body, err = toBody(keys); err != nil {
		return nil, err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteIndexRanged), mimeJson, body); err != nil {
		return nil, err
	}
	if report, err = shared.DecodeStream[shared.Report[core.List]](resp.Body); err != nil {
		return nil, err
	}
	return report.Data, nil
}

func (c *Conn) Insert(keys []string, values []string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.Key]
	)
	if body, err = toBody(keys); err != nil {
		return "", err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteInsert), mimeJson, body); err != nil {
		return "", err
	}
	if report, err = shared.DecodeStream[shared.Report[core.Key]](resp.Body); err != nil {
		return "", err
	}
	return report.Data, nil
}

func (c *Conn) InsertRanged(keys []string, values []string) ([]string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.List]
	)
	if body, err = toBody(keys); err != nil {
		return nil, err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteInsertRanged), mimeJson, body); err != nil {
		return nil, err
	}
	if report, err = shared.DecodeStream[shared.Report[core.List]](resp.Body); err != nil {
		return nil, err
	}
	return report.Data, nil
}

func (c *Conn) Append(keys []string, values []string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.Key]
	)
	if body, err = toBody(keys); err != nil {
		return "", err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteAppend), mimeJson, body); err != nil {
		return "", err
	}
	if report, err = shared.DecodeStream[shared.Report[core.Key]](resp.Body); err != nil {
		return "", err
	}
	return report.Data, nil
}

func (c *Conn) Copy(keys []string, values []string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.Key]
	)
	if body, err = toBody(keys); err != nil {
		return "", err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteCopy), mimeJson, body); err != nil {
		return "", err
	}
	if report, err = shared.DecodeStream[shared.Report[core.Key]](resp.Body); err != nil {
		return "", err
	}
	return report.Data, nil
}

func (c *Conn) Move(keys []string, values []string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.Key]
	)
	if body, err = toBody(keys); err != nil {
		return "", err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteMove), mimeJson, body); err != nil {
		return "", err
	}
	if report, err = shared.DecodeStream[shared.Report[core.Key]](resp.Body); err != nil {
		return "", err
	}
	return report.Data, nil
}

func (c *Conn) Delete(keys []string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.Key]
	)
	if body, err = toBody(keys); err != nil {
		return "", err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteDelete), mimeJson, body); err != nil {
		return "", err
	}
	if report, err = shared.DecodeStream[shared.Report[core.Key]](resp.Body); err != nil {
		return "", err
	}
	return report.Data, nil
}

func (c *Conn) DeleteRanged(keys []string) ([]string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.List]
	)
	if body, err = toBody(keys); err != nil {
		return nil, err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteDeleteRanged), mimeJson, body); err != nil {
		return nil, err
	}
	if report, err = shared.DecodeStream[shared.Report[core.List]](resp.Body); err != nil {
		return nil, err
	}
	return report.Data, nil
}

func (c *Conn) Query(keys []string) ([]string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.List]
	)
	if body, err = toBody(keys); err != nil {
		return nil, err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteQuery), mimeJson, body); err != nil {
		return nil, err
	}
	if report, err = shared.DecodeStream[shared.Report[core.List]](resp.Body); err != nil {
		return nil, err
	}
	return report.Data, nil
}

func (c *Conn) QueryRanged(keys []string) (map[string][]string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.Pair]
	)
	if body, err = toBody(keys); err != nil {
		return nil, err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteQueryRanged), mimeJson, body); err != nil {
		return nil, err
	}
	if report, err = shared.DecodeStream[shared.Report[core.Pair]](resp.Body); err != nil {
		return nil, err
	}
	return report.Data, nil
}

func (c *Conn) NewId(keys []string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.Key]
	)
	if body, err = toBody(keys); err != nil {
		return "", err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteGenerateId), mimeJson, body); err != nil {
		return "", err
	}
	if report, err = shared.DecodeStream[shared.Report[core.Key]](resp.Body); err != nil {
		return "", err
	}
	return report.Data, nil
}

func (c *Conn) NewHash(keys []string) (string, error) {
	var (
		err    error
		resp   *http.Response
		body   io.Reader
		report shared.Report[core.Key]
	)
	if body, err = toBody(keys); err != nil {
		return "", err
	}
	if resp, err = http.Post(formatPath(c.baseAddr, shared.RouteGenerateHash), mimeJson, body); err != nil {
		return "", err
	}
	if report, err = shared.DecodeStream[shared.Report[core.Key]](resp.Body); err != nil {
		return "", err
	}
	return report.Data, nil
}
