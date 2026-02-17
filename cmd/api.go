package main

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
