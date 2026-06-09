package s3

import "errors"

// ErrNotFound is returned (wrapped) by Load / ReloadCheck when the requested
// object does not exist in the bucket. Detect with errors.Is(err, ErrNotFound)
// or the convenience helper [IsNotFound].
var ErrNotFound = errors.New("s3 source: object not found")
