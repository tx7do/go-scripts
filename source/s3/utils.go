package s3

import (
	"errors"
	"strings"

	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// IsNotFound reports whether err represents a "404 / NoSuchKey" response from
// S3. Equivalent to errors.Is(err, ErrNotFound).
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// isNotFound inspects an SDK-level error for a 404 / NoSuchKey response.
// We check both the structured smithy ResponseError path and a textual
// fallback for non-Smithy errors.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var re *smithyhttp.ResponseError
	if errors.As(err, &re) && re.Response != nil && re.Response.StatusCode == 404 {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "NoSuchKey") || strings.Contains(msg, "NotFound")
}
