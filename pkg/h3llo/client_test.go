package h3llo

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	c := New(Config{BaseURL: srv.URL, KeyID: "kid", SecretKey: "sek", ProjectID: "proj-1", MaxRetries: -1})
	return c, srv
}

func TestCreateBucket(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/s3/v1/buckets" {
			t.Errorf("path = %q", r.URL.Path)
		}
		// HMAC headers present.
		for _, h := range []string{"X-H3-Key-Id", "X-H3-Date", "X-H3-Signature"} {
			if r.Header.Get(h) == "" {
				t.Errorf("missing header %s", h)
			}
		}
		if got := r.Header.Get("X-H3-Key-Id"); got != "kid" {
			t.Errorf("key id = %q", got)
		}
		var body createBucketRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Name != "my-bucket" || body.ProjectID != "proj-1" {
			t.Errorf("body = %+v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"credentials":{"access_key_id":"AK","secret_access_key":"SK"}}`)
	})
	defer srv.Close()

	creds, err := c.CreateBucket(context.Background(), "my-bucket")
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	if creds.AccessKeyID != "AK" || creds.SecretAccessKey != "SK" {
		t.Errorf("creds = %+v", creds)
	}
}

func TestCreateBucketEmptyCredsIsError(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"credentials":{}}`)
	})
	defer srv.Close()
	if _, err := c.CreateBucket(context.Background(), "x"); err == nil {
		t.Fatal("expected error on empty credentials")
	}
}

func TestCreateBucketServerError(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	})
	defer srv.Close()
	if _, err := c.CreateBucket(context.Background(), "x"); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestDeleteBucketNotFoundIsOK(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer srv.Close()
	if err := c.DeleteBucket(context.Background(), "ghost"); err != nil {
		t.Fatalf("404 should be idempotent success, got: %v", err)
	}
}

func TestDeleteBucketError(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	})
	defer srv.Close()
	if err := c.DeleteBucket(context.Background(), "x"); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestDeleteBucketPathAndQuery(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/api/s3/v1/buckets/my-bucket" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("project_id"); got != "proj-1" {
			t.Errorf("project_id = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()
	if err := c.DeleteBucket(context.Background(), "my-bucket"); err != nil {
		t.Fatalf("DeleteBucket: %v", err)
	}
}

func TestRetryRecoversAfter500(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadGateway) // transient 5xx
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"credentials":{"access_key_id":"AK","secret_access_key":"SK"}}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, KeyID: "k", SecretKey: "s", ProjectID: "p", MaxRetries: 2})

	creds, err := c.CreateBucket(context.Background(), "b")
	if err != nil {
		t.Fatalf("CreateBucket after retry: %v", err)
	}
	if creds.AccessKeyID != "AK" {
		t.Errorf("creds = %+v", creds)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (1 fail + 1 success)", calls)
	}
}

// TestSign verifies the signature is a stable, body-sensitive HMAC and that
// all required headers are set. It recomputes the expected value with an
// independent HMAC to guard the canonical-string layout against regressions.
func TestSign(t *testing.T) {
	c := New(Config{BaseURL: "https://x", KeyID: "kid", SecretKey: "sek", ProjectID: "p"})
	old := nowUTC
	fixed := time.Unix(1700000000, 0).UTC()
	nowUTC = func() time.Time { return fixed }
	defer func() { nowUTC = old }()

	body := []byte(`{"name":"b"}`)
	req, _ := http.NewRequest(http.MethodPost, "https://x/api/s3/v1/buckets?project_id=p", bytes.NewReader(body))
	c.sign(req, body)

	for _, h := range []string{"X-H3-Key-Id", "X-H3-Date", "X-H3-Signature"} {
		if req.Header.Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}

	// Independent recomputation of the canonical string.
	date := fixed.Format(time.RFC3339)
	bh := sha256.Sum256(body)
	hdrs := []string{"x-h3-date:" + date, "x-h3-key-id:kid"}
	sort.Strings(hdrs)
	canon := strings.Join([]string{"POST", "/api/s3/v1/buckets", "project_id=p",
		strings.Join(hdrs, "\n"), hex.EncodeToString(bh[:])}, "\n")
	mac := hmac.New(sha256.New, []byte("sek"))
	mac.Write([]byte(canon))
	want := hex.EncodeToString(mac.Sum(nil))

	if got := req.Header.Get("X-H3-Signature"); got != want {
		t.Errorf("signature = %q, want %q", got, want)
	}

	// Body change must change the signature.
	req2, _ := http.NewRequest(http.MethodPost, "https://x/api/s3/v1/buckets?project_id=p", nil)
	c.sign(req2, []byte(`{"name":"different"}`))
	if req2.Header.Get("X-H3-Signature") == want {
		t.Error("signature did not change with body")
	}
}
