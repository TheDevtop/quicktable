package main

/*
	Quicktable
	API endpoint functions
*/

import (
	"net/http"

	"github.com/TheDevtop/quicktable/pkg/api"
)

func apiHealth(w http.ResponseWriter, r *http.Request) {
	if err := api.EncodeStream(w, api.Signature); err != nil {
		logPtr.Error("apiHealth()", "err", err)
	}
}

func apiIndexExact(w http.ResponseWriter, r *http.Request) {

}

func apiIndexPrefix(w http.ResponseWriter, r *http.Request) {

}

func apiQueryExact(w http.ResponseWriter, r *http.Request) {

}

func apiQueryPrefix(w http.ResponseWriter, r *http.Request) {

}

func apiInsertExact(w http.ResponseWriter, r *http.Request) {

}

func apiInsertPrefix(w http.ResponseWriter, r *http.Request) {

}

func apiDeleteExact(w http.ResponseWriter, r *http.Request) {

}

func apiDeletePrefix(w http.ResponseWriter, r *http.Request) {

}
