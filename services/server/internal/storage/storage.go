// Package storage wraps the S3-compatible object store (MinIO locally, R2 in production —
// docs/ARCHITECTURE.md §2). minio-go was chosen over the AWS SDK for the open
// "HTTP router and database access" — well, storage-client — decision: it's purpose-built
// for S3-compatible endpoints including MinIO and R2, and pulls in far less than
// aws-sdk-go-v2 for the two operations this needs (put, get).
package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Store struct {
	client *minio.Client
	bucket string
}

func New(endpoint, accessKey, secretKey, bucket string) (*Store, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("storage: parse endpoint: %w", err)
	}
	secure := u.Scheme == "https"
	host := u.Host
	if host == "" {
		host = u.Path // endpoint given without a scheme
	}

	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: new client: %w", err)
	}
	return &Store{client: client, bucket: bucket}, nil
}

// EnsureBucket creates the bucket if it doesn't exist yet — dev/MinIO convenience; R2
// buckets are provisioned out of band in production.
func (s *Store) EnsureBucket(ctx context.Context) error {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("storage: bucket exists check: %w", err)
	}
	if ok {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("storage: make bucket: %w", err)
	}
	return nil
}

// Put streams r to key, not buffering the payload in memory — size must be known (the
// multipart upload's Content-Length for that part) since PutObject needs it up front for a
// non-chunked PUT; -1 falls back to a slower multipart-streaming upload.
func (s *Store) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("storage: put %s: %w", key, err)
	}
	return nil
}

// Remove deletes a single object by its exact key — the single-object counterpart to
// RemoveByPrefix below, for a caller (avatar removal) that already knows the one key to
// delete rather than a prefix to list and bulk-delete.
func (s *Store) Remove(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: remove %s: %w", key, err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: get %s: %w", key, err)
	}
	return obj, nil
}

// RemoveByPrefix deletes every object under prefix — internal/worker's demo-account purge
// sweep is the only caller today, cleaning up a demo user's raw uploads and fog/heatmap tile
// pyramids (all namespaced raw/{userID}/, fog/{userID}/, heatmap/{userID}/ — see server.go
// and internal/fog/render.go's object key builders) in one call rather than tracking every
// individual key written for that user. Best-effort: logged and swallowed by the caller,
// not surfaced as a reason to skip deleting the DB rows those objects belong to.
func (s *Store) RemoveByPrefix(ctx context.Context, prefix string) error {
	objectsCh := make(chan minio.ObjectInfo)
	go func() {
		defer close(objectsCh)
		for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			if obj.Err != nil {
				continue
			}
			objectsCh <- obj
		}
	}()

	var firstErr error
	for result := range s.client.RemoveObjects(ctx, s.bucket, objectsCh, minio.RemoveObjectsOptions{}) {
		if result.Err != nil && firstErr == nil {
			firstErr = fmt.Errorf("storage: remove %s: %w", result.ObjectName, result.Err)
		}
	}
	return firstErr
}
