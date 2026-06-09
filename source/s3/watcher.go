package s3

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Watch returns a channel that signals when the object identified by `key` changes.
// It polls the object's ETag and LastModified via HeadObject every 5 seconds and
// sends a signal on the channel when either value changes.
//
// The returned channel is closed when the context is cancelled. Callers should
// re-Load the script after receiving from the channel.
func (r *Reader) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	ch := make(chan struct{})

	objKey := r.resolveKey(key)

	// Get initial version info.
	head, err := r.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(objKey),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 source: head object %q: %w", objKey, err)
	}

	lastVer := version{
		etag:     aws.ToString(head.ETag),
		modified: aws.ToTime(head.LastModified),
	}

	go func() {
		defer close(ch)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				head, err := r.client.HeadObject(ctx, &s3.HeadObjectInput{
					Bucket: aws.String(r.bucket),
					Key:    aws.String(objKey),
				})
				if err != nil {
					// Object deleted or inaccessible; skip this tick.
					continue
				}

				currentVer := version{
					etag:     aws.ToString(head.ETag),
					modified: aws.ToTime(head.LastModified),
				}

				if currentVer.etag != lastVer.etag || !currentVer.modified.Equal(lastVer.modified) {
					lastVer = currentVer
					// Non-blocking send to avoid goroutine leak if receiver is gone.
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	return ch, nil
}
