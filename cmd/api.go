package main

import (
	"log"
	"net/http"

	"github.com/TheDevtop/quicktable/pkg/server"
	"github.com/TheDevtop/quicktable/pkg/shared"
	"github.com/TheDevtop/quicktable/pkg/shared/core"
)

const mesgKeyConstrained = "Key is not constrained"

var apiTable = map[string]http.HandlerFunc{
	shared.RoutePing: func(w http.ResponseWriter, r *http.Request) {
		shared.EncodeStream(w, "pong!")
	},
	shared.RouteIndex: func(w http.ResponseWriter, r *http.Request) {
		form, err := shared.DecodeStream[shared.Form](r.Body)
		if err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		if key, err := server.Index(dbPtr, core.Merge(form.Keys)); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		} else {
			shared.EncodeStream(w, shared.Report[core.Key]{Failed: false, Data: key})
		}
	},
	shared.RouteIndexRanged: func(w http.ResponseWriter, r *http.Request) {
		form, err := shared.DecodeStream[shared.Form](r.Body)
		if err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		keys := server.IndexRanged(dbPtr, core.Merge(form.Keys))
		shared.EncodeStream(w, shared.Report[[]core.Key]{Failed: false, Data: keys})
	},
	shared.RouteInsert: func(w http.ResponseWriter, r *http.Request) {
		var (
			form shared.Form
			err  error
			key  core.Key
		)
		if form, err = shared.DecodeStream[shared.Form](r.Body); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		if key, err = server.Insert(dbPtr, core.Merge(form.Keys), form.Values); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		shared.EncodeStream(w, shared.Report[core.Key]{Failed: false, Data: key})
	},
	shared.RouteInsertRanged: func(w http.ResponseWriter, r *http.Request) {
		var (
			form    shared.Form
			err     error
			keyList []core.Key
		)
		if form, err = shared.DecodeStream[shared.Form](r.Body); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		if keyList, err = server.InsertRanged(dbPtr, core.Merge(form.Keys), form.Values); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		shared.EncodeStream(w, shared.Report[[]core.Key]{Failed: false, Data: keyList})
	},
	shared.RouteAppend: func(w http.ResponseWriter, r *http.Request) {
		var (
			form shared.Form
			err  error
			key  core.Key
		)
		if form, err = shared.DecodeStream[shared.Form](r.Body); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		if key, err = server.Append(dbPtr, core.Merge(form.Keys), form.Values); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		shared.EncodeStream(w, shared.Report[core.Key]{Failed: false, Data: key})
	},
	shared.RouteDelete: func(w http.ResponseWriter, r *http.Request) {
		var (
			form shared.Form
			err  error
			key  core.Key
		)
		if form, err = shared.DecodeStream[shared.Form](r.Body); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		if key, err = server.Delete(dbPtr, core.Merge(form.Keys)); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		shared.EncodeStream(w, shared.Report[core.Key]{Failed: false, Data: key})
	},
	shared.RouteDeleteRanged: func(w http.ResponseWriter, r *http.Request) {
		var (
			form shared.Form
			err  error
		)
		if form, err = shared.DecodeStream[shared.Form](r.Body); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		if err = server.DeleteRanged(dbPtr, core.Merge(form.Keys)); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		shared.EncodeStream(w, shared.Report[any]{Failed: false, Data: nil})
	},
	shared.RouteQuery: func(w http.ResponseWriter, r *http.Request) {
		var (
			form shared.Form
			err  error
			list core.List
		)
		if form, err = shared.DecodeStream[shared.Form](r.Body); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		if list, err = server.Query(dbPtr, core.Merge(form.Keys)); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		shared.EncodeStream(w, shared.Report[core.List]{Failed: false, Data: list})
	},
	shared.RouteQueryRanged: func(w http.ResponseWriter, r *http.Request) {
		var (
			form shared.Form
			err  error
			pair core.Pair
		)
		if form, err = shared.DecodeStream[shared.Form](r.Body); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		if !core.Constrained(form.Keys) {
			log.Println(mesgKeyConstrained)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: mesgKeyConstrained, Data: nil})
			return
		}
		if pair, err = server.QueryRanged(dbPtr, core.Merge(form.Keys)); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		shared.EncodeStream(w, shared.Report[core.Pair]{Failed: false, Data: pair})
	},
	shared.RouteGenerateId: func(w http.ResponseWriter, r *http.Request) {
		form, err := shared.DecodeStream[shared.Form](r.Body)
		if err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		if !core.Constrained(form.Keys) {
			log.Println(mesgKeyConstrained)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: mesgKeyConstrained, Data: nil})
			return
		}
		if key, err := server.GenerateId(dbPtr, core.Merge(form.Keys)); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		} else {
			shared.EncodeStream(w, shared.Report[core.Key]{Failed: false, Data: key})
		}
	},
	shared.RouteGenerateHash: func(w http.ResponseWriter, r *http.Request) {
		form, err := shared.DecodeStream[shared.Form](r.Body)
		if err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		}
		if key, err := server.GenerateHash(core.Merge(form.Keys)); err != nil {
			log.Println(err)
			shared.EncodeStream(w, shared.Report[any]{Failed: true, Mesg: err.Error(), Data: nil})
			return
		} else {
			shared.EncodeStream(w, shared.Report[core.Key]{Failed: false, Data: key})
		}
	},
}
