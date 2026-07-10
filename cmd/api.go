package main

/*
	Quicktable
	API endpoint functions
*/

import (
	"net/http"

	"github.com/TheDevtop/quicktable/internal/engine"
	"github.com/TheDevtop/quicktable/pkg/api"
)

func apiHealth(w http.ResponseWriter, r *http.Request) {
	if err := api.EncodeStream(w, api.Signature); err != nil {
		logPtr.Error("apiHealth()", "err", err)
	}
}

func apiIndexExact(w http.ResponseWriter, r *http.Request) {
	var (
		err  error
		form api.FormExact
	)

	if form, err = api.DecodeStream[api.FormExact](r.Body); err != nil {
		logPtr.Error("apiIndexExact", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteIndexExact,
			Status: api.StatusApiError,
			Data:   err.Error(),
		})
		return
	}

	if _, err = engine.IndexExact(form.Key); err != nil {
		logPtr.Error("apiIndexExact", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteIndexExact,
			Status: api.StatusEngineError,
			Data:   err.Error(),
		})
		return
	}

	api.EncodeStream(w, api.FormResponse[string]{
		Route:  api.RouteIndexExact,
		Status: api.StatusOk,
		Data:   form.Key,
	})
}

func apiIndexPrefix(w http.ResponseWriter, r *http.Request) {
	var (
		err    error
		form   api.FormPrefix
		keyMap map[string]struct{}
	)

	if form, err = api.DecodeStream[api.FormPrefix](r.Body); err != nil {
		logPtr.Error("apiIndexPrefix", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteIndexPrefix,
			Status: api.StatusApiError,
			Data:   err.Error(),
		})
		return
	}

	keyMap = engine.IndexPrefix(form.Key)
	api.EncodeStream(w, api.FormResponse[map[string]struct{}]{
		Route:  api.RouteIndexPrefix,
		Status: api.StatusOk,
		Data:   keyMap,
	})
}

func apiQueryExact(w http.ResponseWriter, r *http.Request) {
	var (
		err  error
		form api.FormExact
	)

	if form, err = api.DecodeStream[api.FormExact](r.Body); err != nil {
		logPtr.Error("apiQueryExact", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteQueryExact,
			Status: api.StatusApiError,
			Data:   err.Error(),
		})
		return
	}

	if form.Value, err = engine.QueryExact(form.Key); err != nil {
		logPtr.Error("apiQueryExact", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteQueryExact,
			Status: api.StatusEngineError,
			Data:   err.Error(),
		})
		return
	}

	api.EncodeStream(w, api.FormResponse[string]{
		Route:  api.RouteQueryExact,
		Status: api.StatusOk,
		Data:   form.Value,
	})
}

func apiQueryPrefix(w http.ResponseWriter, r *http.Request) {
	var (
		err  error
		form api.FormPrefix
	)

	if form, err = api.DecodeStream[api.FormPrefix](r.Body); err != nil {
		logPtr.Error("apiQueryPrefix", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteQueryPrefix,
			Status: api.StatusApiError,
			Data:   err.Error(),
		})
		return
	}

	if form.Value, err = engine.QueryPrefix(form.Key); err != nil {
		logPtr.Error("apiQueryPrefix", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteQueryPrefix,
			Status: api.StatusEngineError,
			Data:   err.Error(),
		})
		return
	}

	api.EncodeStream(w, api.FormResponse[map[string]string]{
		Route:  api.RouteQueryPrefix,
		Status: api.StatusOk,
		Data:   form.Value,
	})
}

func apiInsertExact(w http.ResponseWriter, r *http.Request) {
	var (
		err  error
		form api.FormExact
	)

	if form, err = api.DecodeStream[api.FormExact](r.Body); err != nil {
		logPtr.Error("apiInsertExact", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteInsertExact,
			Status: api.StatusApiError,
			Data:   err.Error(),
		})
		return
	}

	if form.Key, err = engine.InsertExact(form.Key, form.Value); err != nil {
		logPtr.Error("apiInsertExact", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteInsertExact,
			Status: api.StatusEngineError,
			Data:   err.Error(),
		})
		return
	}

	api.EncodeStream(w, api.FormResponse[string]{
		Route:  api.RouteInsertExact,
		Status: api.StatusOk,
		Data:   form.Key,
	})
}

func apiInsertPrefix(w http.ResponseWriter, r *http.Request) {
	var (
		err  error
		form api.FormPrefix
	)

	if form, err = api.DecodeStream[api.FormPrefix](r.Body); err != nil {
		logPtr.Error("apiInsertPrefix", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteInsertPrefix,
			Status: api.StatusApiError,
			Data:   err.Error(),
		})
		return
	}

	if err = engine.InsertPrefix(form.Key, form.Value); err != nil {
		logPtr.Error("apiInsertPrefix", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteInsertPrefix,
			Status: api.StatusEngineError,
			Data:   err.Error(),
		})
		return
	}

	api.EncodeStream(w, api.FormResponse[string]{
		Route:  api.RouteInsertPrefix,
		Status: api.StatusOk,
		Data:   form.Key,
	})
}

func apiDeleteExact(w http.ResponseWriter, r *http.Request) {
	var (
		err  error
		form api.FormExact
	)

	if form, err = api.DecodeStream[api.FormExact](r.Body); err != nil {
		logPtr.Error("apiDeleteExact", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteDeleteExact,
			Status: api.StatusApiError,
			Data:   err.Error(),
		})
		return
	}

	if form.Key, err = engine.DeleteExact(form.Key); err != nil {
		logPtr.Error("apiDeleteExact", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteDeleteExact,
			Status: api.StatusEngineError,
			Data:   err.Error(),
		})
		return
	}

	api.EncodeStream(w, api.FormResponse[string]{
		Route:  api.RouteDeleteExact,
		Status: api.StatusOk,
		Data:   form.Key,
	})
}

func apiDeletePrefix(w http.ResponseWriter, r *http.Request) {
	var (
		err  error
		form api.FormPrefix
	)

	if form, err = api.DecodeStream[api.FormPrefix](r.Body); err != nil {
		logPtr.Error("apiDeletePrefix", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteDeletePrefix,
			Status: api.StatusApiError,
			Data:   err.Error(),
		})
		return
	}

	if err = engine.DeletePrefix(form.Key); err != nil {
		logPtr.Error("apiDeletePrefix", "err", err)
		api.EncodeStream(w, api.FormResponse[string]{
			Route:  api.RouteDeletePrefix,
			Status: api.StatusEngineError,
			Data:   err.Error(),
		})
		return
	}

	api.EncodeStream(w, api.FormResponse[string]{
		Route:  api.RouteDeletePrefix,
		Status: api.StatusOk,
		Data:   form.Key,
	})
}
