package s3api

import "net/http"

// Middleware — функция, которая оборачивает http.Handler.
type Middleware func(http.Handler) http.Handler

// Chain собирает middleware в цепочку в порядке их передачи.
// Например, Chain(h, mw1, mw2, mw3) выполнит mw1, затем mw2, затем mw3, затем h.
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
