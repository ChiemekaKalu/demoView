package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// fakeS3 serves path-style GET, HEAD and PUT for one bucket. It does not
// check signatures.
type fakeS3 struct {
	bucket string

	mu       sync.Mutex
	objects  map[string]fakeObject
	requests []*http.Request
	fail     bool // answer every request with 500
}

type fakeObject struct {
	data        []byte
	contentType string
}

func newFakeS3(t *testing.T) (*fakeS3, *S3) {
	f := &fakeS3{bucket: "demoview-test", objects: map[string]fakeObject{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	s := NewR2(R2Config{
		AccessKeyID:     "test-key-id",
		SecretAccessKey: "test-secret",
		Bucket:          f.bucket,
		Endpoint:        srv.URL,
	}, func(o *s3.Options) { o.RetryMaxAttempts = 1 })
	return f, s
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r)
	if f.fail {
		s3Error(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if bucket != f.bucket {
		s3Error(w, r, http.StatusNotFound, "NoSuchBucket")
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		obj, ok := f.objects[key]
		if !ok {
			s3Error(w, r, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("Content-Type", obj.contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(obj.data)))
		if r.Method == http.MethodGet {
			w.Write(obj.data)
		}
	case http.MethodPut:
		data, err := io.ReadAll(r.Body)
		if err != nil {
			s3Error(w, r, http.StatusBadRequest, "IncompleteBody")
			return
		}
		f.objects[key] = fakeObject{data: data, contentType: r.Header.Get("Content-Type")}
		w.Header().Set("ETag", `"fake"`)
	default:
		s3Error(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// s3Error writes an S3 XML error. HEAD errors have no body, as on S3.
func s3Error(w http.ResponseWriter, r *http.Request, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, code)
	}
}

func TestS3(t *testing.T) {
	_, s := newFakeS3(t)
	testStore(t, s)
}

func TestS3Requests(t *testing.T) {
	f, s := newFakeS3(t)
	ctx := context.Background()
	if err := s.Put(ctx, "parsed/abc/match.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "demos/abc.dem", []byte("PBDEMS2\x00")); err != nil {
		t.Fatal(err)
	}

	if got := f.objects["parsed/abc/match.json"].contentType; got != "application/json" {
		t.Errorf("match.json Content-Type = %q, want application/json", got)
	}
	if got := f.objects["demos/abc.dem"].contentType; got != "application/octet-stream" {
		t.Errorf("demo Content-Type = %q, want application/octet-stream", got)
	}
	r := f.requests[0]
	if r.URL.Path != "/demoview-test/parsed/abc/match.json" {
		t.Errorf("path = %q, want path-style /demoview-test/parsed/abc/match.json", r.URL.Path)
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=test-key-id/") || !strings.Contains(auth, "/auto/s3/") {
		t.Errorf("Authorization = %q, want SigV4 for region auto with the access key id", auth)
	}
}

func TestS3MissingBucketIsNotNotFound(t *testing.T) {
	f, s := newFakeS3(t)
	f.bucket = "other"
	_, _, err := s.Open(context.Background(), "demos/abc.dem")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Open error = %v, want a non-ErrNotFound error", err)
	}
}

func TestS3ServerErrors(t *testing.T) {
	f, s := newFakeS3(t)
	f.fail = true
	ctx := context.Background()
	if _, _, err := s.Open(ctx, "demos/abc.dem"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Open error = %v, want a non-ErrNotFound error", err)
	}
	if err := s.Put(ctx, "parsed/abc/match.json", []byte("{}")); err == nil {
		t.Error("Put succeeded against a failing server")
	}
}

func TestR2ConfigFromEnv(t *testing.T) {
	env := map[string]string{
		"R2_ACCOUNT_ID":        "acct",
		"R2_ACCESS_KEY_ID":     "id",
		"R2_SECRET_ACCESS_KEY": "secret",
		"R2_BUCKET":            "bucket",
	}
	cfg, err := R2ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg != (R2Config{AccountID: "acct", AccessKeyID: "id", SecretAccessKey: "secret", Bucket: "bucket"}) {
		t.Errorf("cfg = %+v", cfg)
	}

	_, err = R2ConfigFromEnv(func(k string) string { return map[string]string{"R2_BUCKET": "b"}[k] })
	want := "store: missing R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY"
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}

	delete(env, "R2_ACCOUNT_ID")
	env["R2_ENDPOINT"] = "http://localhost:9000"
	if _, err := R2ConfigFromEnv(func(k string) string { return env[k] }); err != nil {
		t.Errorf("R2_ENDPOINT without R2_ACCOUNT_ID: %v", err)
	}
}
