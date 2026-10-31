// Package s3 implements blob.Store over the S3 API, which RustFS, AWS
// S3 and MinIO all speak: which one is a matter of the endpoint, the
// credentials and the bucket alone (ADR-0127 §2).
package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/garamsh/garam/cowork/internal/blob"
	"github.com/garamsh/garam/lib/errorx"
)

// Config names a bucket and how to reach it. Endpoint is empty for AWS
// S3 itself, and the store's URL otherwise; PathStyle addresses the
// bucket in the path, as RustFS and MinIO expect, rather than in the
// host name.
type Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	PathStyle bool
}

// Store implements blob.Store in one bucket.
type Store struct {
	client *awss3.Client
	bucket string
}

var _ blob.Store = (*Store)(nil)

// New answers a store over cfg's bucket. It reaches nothing until it
// is first used.
func New(cfg Config) (*Store, error) {
	if cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "blob store: a bucket, an access key and a secret key are required")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	client := awss3.New(awss3.Options{
		Region:       region,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		UsePathStyle: cfg.PathStyle,
		BaseEndpoint: endpoint(cfg.Endpoint),
	})
	return &Store{client: client, bucket: cfg.Bucket}, nil
}

func endpoint(e string) *string {
	if e == "" {
		return nil
	}
	return aws.String(e)
}

// EnsureBucket creates the bucket where it does not exist, for a
// store a deployment has not provisioned: a development stack, or a
// test.
func (s *Store) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &awss3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil
	}
	_, err = s.client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	var owned *types.BucketAlreadyOwnedByYou
	if err != nil && !errors.As(err, &owned) {
		return errorx.Wrap(err, "blob store: create bucket", errorx.KindUnavailable)
	}
	return nil
}

// Put implements blob.Store. The write is conditional on no object
// being under key, so the store, not a read before it, keeps a blob
// from being replaced.
func (s *Store) Put(ctx context.Context, key string, b blob.Blob) error {
	_, err := s.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(b.Body),
		ContentLength: aws.Int64(int64(len(b.Body))),
		ContentType:   aws.String(b.ContentType),
		IfNoneMatch:   aws.String("*"),
	})
	if status(err) == http.StatusPreconditionFailed || status(err) == http.StatusConflict {
		return blob.ErrExists
	}
	if err != nil {
		return errorx.Wrap(err, "blob store: put", errorx.KindUnavailable)
	}
	return nil
}

// Get implements blob.Store.
func (s *Store) Get(ctx context.Context, key string) (blob.Blob, error) {
	out, err := s.client.GetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	var missing *types.NoSuchKey
	if errors.As(err, &missing) || status(err) == http.StatusNotFound {
		return blob.Blob{}, blob.ErrNotFound
	}
	if err != nil {
		return blob.Blob{}, errorx.Wrap(err, "blob store: get", errorx.KindUnavailable)
	}
	defer func() { _ = out.Body.Close() }()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return blob.Blob{}, errorx.Wrap(err, "blob store: read", errorx.KindUnavailable)
	}
	return blob.Blob{ContentType: aws.ToString(out.ContentType), Body: body}, nil
}

// Delete implements blob.Store. S3 answers a key it does not hold as
// it answers one it removed.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return errorx.Wrap(err, "blob store: delete", errorx.KindUnavailable)
	}
	return nil
}

// status is the HTTP status an S3 call answered, 0 for none.
func status(err error) int {
	var re interface{ HTTPStatusCode() int }
	if errors.As(err, &re) {
		return re.HTTPStatusCode()
	}
	var api smithy.APIError
	if errors.As(err, &api) && api.ErrorCode() == "PreconditionFailed" {
		return http.StatusPreconditionFailed
	}
	return 0
}
