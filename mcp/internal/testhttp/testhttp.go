// Package testhttp serves HTTP requests from an in-process handler, so tests
// can fake remote APIs without opening a network socket.
package testhttp

import (
	"net/http"
	"net/http/httptest"
)

// Client returns an *http.Client whose requests are answered by h.
func Client(h http.Handler) *http.Client {
	return &http.Client{Transport: roundTripper{h}}
}

type roundTripper struct{ h http.Handler }

func (rt roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rt.h.ServeHTTP(rec, req)
	resp := rec.Result()
	resp.Request = req
	return resp, nil
}
