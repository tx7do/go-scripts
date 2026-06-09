package http

import "errors"

// ErrNotFound is returned (wrapped) by Load when the HTTP endpoint responds
// with a 404 status code. Detect with errors.Is(err, ErrNotFound) or the
// convenience helper [IsNotFound].
var ErrNotFound = errors.New("http source: not found")
