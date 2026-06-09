package git

import "errors"

// ErrNotFound is returned (wrapped) by Load when the requested key does not
// exist in the git repository. Detect with errors.Is(err, ErrNotFound) or the
// convenience helper [IsNotFound].
var ErrNotFound = errors.New("git source: key not found")
