package main

/*
	Quicktable
	API endpoints
*/

import (
	"net/http"

	"github.com/TheDevtop/quicktable/pkg/engine"
	"github.com/TheDevtop/quicktable/pkg/shared"
)

func apiHealth(w http.ResponseWriter, r *http.Request) {
	shared.EncodeStream(w, shared.FormResult[string]{
		Status: shared.StatusOk,
		Data:   shared.Signature,
	})
}

func apiQuery(w http.ResponseWriter, r *http.Request) {
	var (
		form   shared.FormQuery
		result any
		err    error
	)

	if form, err = shared.DecodeStream[shared.FormQuery](r.Body); err != nil {
		logPtr.Error(err, "route", shared.RouteQuery)
		shared.EncodeStream(w, shared.FormResult[string]{
			Status: shared.StatusApiError,
			Data:   err.Error(),
		})
		return
	}

	if result, err = dispatch(form.Fn, form.Args); err != nil {
		logPtr.Error(err, "route", shared.RouteQuery, "fn", form.Fn)
		shared.EncodeStream(w, shared.FormResult[string]{
			Status: shared.StatusEngineError,
			Data:   err.Error(),
		})
		return
	}

	if err = shared.EncodeStream(w, shared.FormResult[any]{
		Status: shared.StatusOk,
		Data:   result,
	}); err != nil {
		logPtr.Warn(err, "route", shared.RouteQuery)
	}
}

func apiHash(w http.ResponseWriter, r *http.Request) {
	form, err := shared.DecodeStream[shared.FormQuery](r.Body)
	if err != nil {
		logPtr.Error(err, "route", shared.RouteHash)
		shared.EncodeStream(w, shared.FormResult[string]{
			Status: shared.StatusApiError,
			Data:   err.Error(),
		})
		return
	}
	if len(form.Args) < 1 {
		logPtr.Error(shared.ErrInvalidArgs, "route", shared.RouteHash)
		shared.EncodeStream(w, shared.FormResult[string]{
			Status: shared.StatusApiError,
			Data:   shared.ErrInvalidArgs.Error(),
		})
		return
	}

	arg, ok := form.Args[0].(string)
	if !ok {
		logPtr.Error(shared.ErrInvalidArgs, "route", shared.RouteHash)
		shared.EncodeStream(w, shared.FormResult[string]{
			Status: shared.StatusApiError,
			Data:   shared.ErrInvalidArgs.Error(),
		})
		return
	}

	if key, err := engine.GenerateHash(arg); err != nil {
		logPtr.Error(err, "route", shared.RouteHash)
		shared.EncodeStream(w, shared.FormResult[string]{
			Status: shared.StatusEngineError,
			Data:   err.Error(),
		})
		return
	} else {
		shared.EncodeStream(w, shared.FormResult[string]{
			Status: shared.StatusOk,
			Data:   key,
		})
	}
}
