package main

import "net/http"

func registerSignInWebRoutes(mux *http.ServeMux, handler http.Handler) {
	mux.Handle("/sign-in/", handler)
	mux.Handle("GET /api/sign-in/{session}", handler)
	mux.Handle("DELETE /api/sign-in/{session}", handler)
}
