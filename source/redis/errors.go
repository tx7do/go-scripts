package redis

import "errors"

// ErrNotFound is returned (wrapped) by Load when the requested key does not
// exist in Redis. Detect with errors.Is(err, ErrNotFound) or the convenience
// helper [IsNotFound].
var ErrNotFound = errors.New("redis source: key not found")
