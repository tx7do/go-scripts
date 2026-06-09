package consul

import "errors"

// ErrNotFound is returned (wrapped) by Load when the requested key does not
// exist in Consul's KV store. Detect with errors.Is(err, ErrNotFound) or the
// convenience helper [IsNotFound].
var ErrNotFound = errors.New("consul source: key not found")
