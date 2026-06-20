// Package h3llo is a thin HTTP client for the h3llo.cloud (h3) public API.
//
// Auth is HMAC-SHA256 over a canonical request, using an API key-id + secret
// pair (the same scheme as the official terraform-provider-h3). It covers only
// the operations the COSI driver needs: create bucket, delete bucket. Create
// returns the project's shared S3 access key pair inline.
package h3llo

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client talks to the h3llo.cloud public API with HMAC auth.
type Client struct {
	baseURL    string // e.g. https://api.h3llo.cloud
	keyID      string
	secretKey  string
	projectID  string
	maxRetries int
	httpClient *http.Client
}

// Config configures a Client.
type Config struct {
	BaseURL    string
	KeyID      string
	SecretKey  string
	ProjectID  string
	Timeout    time.Duration
	MaxRetries int
}

// New builds a Client.
func New(c Config) *Client {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	switch {
	case c.MaxRetries == 0:
		c.MaxRetries = 3 // unset → sensible default
	case c.MaxRetries < 0:
		c.MaxRetries = 0 // explicit "no retries"
	}
	return &Client{
		baseURL:    strings.TrimRight(c.BaseURL, "/"),
		keyID:      c.KeyID,
		secretKey:  c.SecretKey,
		projectID:  c.ProjectID,
		maxRetries: c.MaxRetries,
		httpClient: &http.Client{Timeout: c.Timeout},
	}
}

// HTTPError is returned for non-2xx responses the caller may want to inspect.
type HTTPError struct {
	StatusCode int
	Method     string
	URL        string
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s: status %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

// Credentials is the S3 access key pair returned on bucket creation. h3llo
// shares one pair across all buckets in a project.
type Credentials struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

type createBucketRequest struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
}

type createBucketResponse struct {
	Message     string      `json:"message"`
	Credentials Credentials `json:"credentials"`
}

// CreateBucket creates a bucket by name and returns the project's shared S3
// credentials. The h3llo API is idempotent: creating an existing bucket
// succeeds (201) and returns the same credentials, so this doubles as a
// credential fetch for an already-created bucket.
func (c *Client) CreateBucket(ctx context.Context, name string) (Credentials, error) {
	body := createBucketRequest{ProjectID: c.projectID, Name: name}
	code, raw, err := c.do(ctx, http.MethodPost, "/api/s3/v1/buckets", "", body)
	if err != nil {
		return Credentials{}, fmt.Errorf("create bucket %q: %w", name, err)
	}
	if code < 200 || code >= 300 {
		return Credentials{}, fmt.Errorf("create bucket %q: status %d: %s", name, code, raw)
	}
	var cr createBucketResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return Credentials{}, fmt.Errorf("create bucket %q: decode: %w", name, err)
	}
	if cr.Credentials.AccessKeyID == "" {
		return Credentials{}, fmt.Errorf("create bucket %q: empty credentials in response", name)
	}
	return cr.Credentials, nil
}

// DeleteBucket deletes a bucket by name. A 404 is treated as success so the
// call is idempotent.
func (c *Client) DeleteBucket(ctx context.Context, name string) error {
	query := "project_id=" + c.projectID
	code, raw, err := c.do(ctx, http.MethodDelete, "/api/s3/v1/buckets/"+name, query, nil)
	if err != nil {
		return fmt.Errorf("delete bucket %q: %w", name, err)
	}
	switch {
	case code >= 200 && code < 300:
		return nil
	case code == http.StatusNotFound:
		return nil // already gone — idempotent
	default:
		return fmt.Errorf("delete bucket %q: status %d: %s", name, code, raw)
	}
}

// do issues an HMAC-signed request, retrying transport errors and 5xx with
// exponential backoff. It returns the status code and read body. A fresh
// request (and signature, since X-H3-Date changes) is built per attempt.
func (c *Client) do(ctx context.Context, method, path, query string, payload any) (int, []byte, error) {
	var bodyBytes []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyBytes = b
	}

	url := c.baseURL + path
	if query != "" {
		url += "?" + query
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return 0, nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(bodyBytes))
		if err != nil {
			return 0, nil, fmt.Errorf("build request: %w", err)
		}
		c.sign(req, bodyBytes)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%s %s: %w", method, path, err)
			continue // retry transport errors
		}
		raw, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response body: %w", err)
			continue
		}
		if resp.StatusCode >= 500 {
			lastErr = &HTTPError{StatusCode: resp.StatusCode, Method: method, URL: url, Body: string(raw)}
			continue // retry server errors
		}
		return resp.StatusCode, raw, nil
	}
	return 0, nil, lastErr
}

// sign adds the h3 HMAC-SHA256 signature headers. The canonical request is:
//
//	METHOD\n PATH\n RAW_QUERY\n SORTED_SIGNED_HEADERS\n SHA256_HEX(body)
//
// signed headers are x-h3-date and x-h3-key-id, sorted. Must stay byte-for-byte
// compatible with the h3 API gateway and terraform-provider-h3.
func (c *Client) sign(req *http.Request, body []byte) {
	date := nowUTC().Format(time.RFC3339)
	bodyHash := sha256.Sum256(body)

	signedHeaders := []string{
		"x-h3-date:" + date,
		"x-h3-key-id:" + c.keyID,
	}
	sort.Strings(signedHeaders)

	canonical := strings.Join([]string{
		req.Method,
		req.URL.Path,
		req.URL.RawQuery,
		strings.Join(signedHeaders, "\n"),
		hex.EncodeToString(bodyHash[:]),
	}, "\n")

	mac := hmac.New(sha256.New, []byte(c.secretKey))
	mac.Write([]byte(canonical))
	signature := hex.EncodeToString(mac.Sum(nil))

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-H3-Key-Id", c.keyID)
	req.Header.Set("X-H3-Date", date)
	req.Header.Set("X-H3-Signature", signature)
}

// nowUTC is a variable so tests can pin the clock if needed.
var nowUTC = func() time.Time { return time.Now().UTC() }
