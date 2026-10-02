package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// R2Config holds the R2 bucket and its S3 API credentials.
type R2Config struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	// Endpoint overrides https://<AccountID>.r2.cloudflarestorage.com, for
	// tests or another S3-compatible service.
	Endpoint string
}

// R2ConfigFromEnv reads R2_ACCOUNT_ID, R2_ACCESS_KEY_ID,
// R2_SECRET_ACCESS_KEY, R2_BUCKET and the optional R2_ENDPOINT. The account
// id is not needed when R2_ENDPOINT is set.
func R2ConfigFromEnv(getenv func(string) string) (R2Config, error) {
	cfg := R2Config{
		AccountID:       getenv("R2_ACCOUNT_ID"),
		AccessKeyID:     getenv("R2_ACCESS_KEY_ID"),
		SecretAccessKey: getenv("R2_SECRET_ACCESS_KEY"),
		Bucket:          getenv("R2_BUCKET"),
		Endpoint:        getenv("R2_ENDPOINT"),
	}
	var missing []string
	for _, v := range []struct{ name, value string }{
		{"R2_ACCOUNT_ID", cfg.AccountID + cfg.Endpoint},
		{"R2_ACCESS_KEY_ID", cfg.AccessKeyID},
		{"R2_SECRET_ACCESS_KEY", cfg.SecretAccessKey},
		{"R2_BUCKET", cfg.Bucket},
	} {
		if v.value == "" {
			missing = append(missing, v.name)
		}
	}
	if len(missing) > 0 {
		return R2Config{}, fmt.Errorf("store: missing %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

// S3 is a Store backed by one bucket on an S3-compatible service.
type S3 struct {
	client *s3.Client
	bucket string
}

// NewR2 returns a Store for the R2 bucket in cfg. optFns adjust the S3
// client options, for example to change retries in tests.
func NewR2(cfg R2Config, optFns ...func(*s3.Options)) *S3 {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)
	}
	creds := aws.Credentials{AccessKeyID: cfg.AccessKeyID, SecretAccessKey: cfg.SecretAccessKey}
	opts := s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: true,
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return creds, nil
		}),
		// Only send and check the checksums S3 requires. The newer default
		// of always adding them is an AWS extension that other services
		// may not support.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}
	return &S3{client: s3.New(opts, optFns...), bucket: cfg.Bucket}
}

// Open implements Store. The returned reader is the HTTP response body, so
// the object streams as it is read.
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if err := checkKey(key); err != nil {
		return nil, 0, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return nil, 0, s3Err("get", key, err)
	}
	return out.Body, aws.ToInt64(out.ContentLength), nil
}

// Stat implements Store.
func (s *S3) Stat(ctx context.Context, key string) (int64, error) {
	if err := checkKey(key); err != nil {
		return 0, err
	}
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return 0, s3Err("head", key, err)
	}
	return aws.ToInt64(out.ContentLength), nil
}

// Put implements Store.
func (s *S3) Put(ctx context.Context, key string, data []byte) error {
	if err := checkKey(key); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &s.bucket,
		Key:           &key,
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String(contentType(key)),
	})
	if err != nil {
		return s3Err("put", key, err)
	}
	return nil
}

// s3Err maps a missing key to ErrNotFound. On GET a missing bucket is not
// ErrNotFound, because it is a configuration error and not a missing demo.
// HEAD responses have no body, so Stat cannot tell the two apart.
func s3Err(op, key string, err error) error {
	var noKey *types.NoSuchKey
	var notFound *types.NotFound
	if errors.As(err, &noKey) || errors.As(err, &notFound) {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return fmt.Errorf("store: %s %s: %w", op, key, err)
}
