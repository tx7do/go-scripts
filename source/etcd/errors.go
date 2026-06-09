package etcd

import "errors"

// ErrNotFound is returned (wrapped) by Load when the requested key does not
// exist in etcd. Detect with errors.Is(err, ErrNotFound) or the convenience
// helper [IsNotFound].
var ErrNotFound = errors.New("etcd source: key not found")
