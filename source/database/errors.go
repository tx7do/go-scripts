package database

import "errors"

// ErrNotFound is returned (wrapped) by Load when the requested key does not
// exist in the database. Detect with errors.Is(err, ErrNotFound) or the
// convenience helper [IsNotFound].
var ErrNotFound = errors.New("database source: key not found")
