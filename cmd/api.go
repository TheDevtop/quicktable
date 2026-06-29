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

}
