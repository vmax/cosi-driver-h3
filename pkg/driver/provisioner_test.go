package driver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	cosi "sigs.k8s.io/container-object-storage-interface/proto"
)

func testServer(t *testing.T, h http.HandlerFunc) *ProvisionerServer {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := Config{
		S3Endpoint: "https://storage.h3llo.cloud",
		Region:     "ru-1",
		APIBaseURL: srv.URL,
		KeyID:      "kid",
		SecretKey:  "sek",
		ProjectID:  "proj-1",
	}
	return NewProvisionerServer(cfg)
}

func credsHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"credentials":{"access_key_id":"AK","secret_access_key":"SK"}}`))
}

func TestCreateBucketReturnsIDAndProtocol(t *testing.T) {
	s := testServer(t, credsHandler)
	resp, err := s.DriverCreateBucket(context.Background(),
		&cosi.DriverCreateBucketRequest{Name: "b1"})
	if err != nil {
		t.Fatalf("DriverCreateBucket: %v", err)
	}
	if resp.GetBucketId() != "b1" {
		t.Errorf("bucketId = %q, want b1", resp.GetBucketId())
	}
	s3 := resp.GetProtocols().GetS3()
	if s3 == nil || s3.GetRegion() != "ru-1" || s3.GetBucketId() != "b1" {
		t.Errorf("protocols S3 = %+v", s3)
	}
	if s3.GetEndpoint() != "https://storage.h3llo.cloud" {
		t.Errorf("endpoint = %q", s3.GetEndpoint())
	}
	if s3.GetAddressingStyle().GetStyle() != cosi.S3AddressingStyle_PATH {
		t.Errorf("addressing style = %v, want PATH", s3.GetAddressingStyle().GetStyle())
	}
}

func TestIsPublicParam(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{{"true", true}, {"True", true}, {"read", true}, {"public-read", true},
		{"false", false}, {"", false}, {"yes", false}} {
		if got := isPublicParam(map[string]string{"public": tc.val}); got != tc.want {
			t.Errorf("public=%q → %v, want %v", tc.val, got, tc.want)
		}
	}
	if isPublicParam(nil) {
		t.Error("nil params should not be public")
	}
}

func TestCreateBucketPublicAppliesPolicy(t *testing.T) {
	// Mock S3 endpoint to capture the PutBucketPolicy.
	var policyApplied bool
	s3srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["policy"]; ok && r.Method == http.MethodPut {
			policyApplied = true
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer s3srv.Close()

	// Mock h3llo mgmt API for CreateBucket (returns creds).
	apisrv := httptest.NewServer(http.HandlerFunc(credsHandler))
	defer apisrv.Close()

	s := NewProvisionerServer(Config{
		S3Endpoint: s3srv.URL, Region: "ru-1",
		APIBaseURL: apisrv.URL, KeyID: "k", SecretKey: "s", ProjectID: "p",
	})

	_, err := s.DriverCreateBucket(context.Background(), &cosi.DriverCreateBucketRequest{
		Name: "pub", Parameters: map[string]string{"public": "true"},
	})
	if err != nil {
		t.Fatalf("DriverCreateBucket(public): %v", err)
	}
	if !policyApplied {
		t.Error("expected PutBucketPolicy on public bucket")
	}
}

func TestCreateBucketPrivateSkipsPolicy(t *testing.T) {
	s3srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("private bucket must not call S3 PutBucketPolicy")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer s3srv.Close()
	apisrv := httptest.NewServer(http.HandlerFunc(credsHandler))
	defer apisrv.Close()
	s := NewProvisionerServer(Config{
		S3Endpoint: s3srv.URL, Region: "ru-1",
		APIBaseURL: apisrv.URL, KeyID: "k", SecretKey: "s", ProjectID: "p",
	})
	if _, err := s.DriverCreateBucket(context.Background(),
		&cosi.DriverCreateBucketRequest{Name: "priv"}); err != nil {
		t.Fatalf("DriverCreateBucket: %v", err)
	}
}

func TestBackendBucketName(t *testing.T) {
	const uuid = "bc-5c7009ba-b4ed-42f5-b03e-dfe611586f59" // 39 chars, compliant
	cases := []struct {
		name, prefix, in string
		want             string
		wantErr          bool
	}{
		{"no prefix", "", uuid, uuid, false},
		{"valid prefix", "team-a-", uuid, "team-a-" + uuid, false},
		{"prefix too long", strings.Repeat("a", 25), uuid, "", true}, // 25+39=64 > 63
		{"prefix starts non-letter", "1team-", uuid, "", true},
		{"prefix uppercase", "Team-", uuid, "", true},
		{"prefix illegal char", "team_", uuid, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := backendBucketName(c.in, map[string]string{"bucketNamePrefix": c.prefix})
			if c.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestCreateBucketWithPrefix(t *testing.T) {
	var gotBody string
	s := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"credentials":{"access_key_id":"AK","secret_access_key":"SK"}}`))
	})
	resp, err := s.DriverCreateBucket(context.Background(), &cosi.DriverCreateBucketRequest{
		Name: "bc-abc", Parameters: map[string]string{"bucketNamePrefix": "team-"},
	})
	if err != nil {
		t.Fatalf("DriverCreateBucket: %v", err)
	}
	if resp.GetBucketId() != "team-bc-abc" {
		t.Errorf("bucketId = %q, want team-bc-abc", resp.GetBucketId())
	}
	if !strings.Contains(gotBody, `"name":"team-bc-abc"`) {
		t.Errorf("create body = %s, want name team-bc-abc", gotBody)
	}
	if resp.GetProtocols().GetS3().GetBucketId() != "team-bc-abc" {
		t.Errorf("s3 bucketId = %q", resp.GetProtocols().GetS3().GetBucketId())
	}
}

func TestCreateBucketInvalidPrefixRejected(t *testing.T) {
	s := testServer(t, func(http.ResponseWriter, *http.Request) {
		t.Error("must not call API on invalid name")
	})
	_, err := s.DriverCreateBucket(context.Background(), &cosi.DriverCreateBucketRequest{
		Name: "bc-abc", Parameters: map[string]string{"bucketNamePrefix": "BAD_"},
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for bad prefix")
	}
}

func TestCreateBucketEmptyName(t *testing.T) {
	s := testServer(t, func(http.ResponseWriter, *http.Request) {})
	if _, err := s.DriverCreateBucket(context.Background(),
		&cosi.DriverCreateBucketRequest{}); err == nil {
		t.Fatal("expected error on empty name")
	}
}

func keyAuth() *cosi.AuthenticationType {
	return &cosi.AuthenticationType{Type: cosi.AuthenticationType_KEY}
}

func TestGrantBucketAccessReturnsSharedKey(t *testing.T) {
	s := testServer(t, credsHandler)
	resp, err := s.DriverGrantBucketAccess(context.Background(),
		&cosi.DriverGrantBucketAccessRequest{
			AccountName:        "acc",
			AuthenticationType: keyAuth(),
			Buckets: []*cosi.DriverGrantBucketAccessRequest_AccessedBucket{
				{BucketId: "b1"},
			},
		})
	if err != nil {
		t.Fatalf("DriverGrantBucketAccess: %v", err)
	}
	if resp.GetAccountId() != "AK" {
		t.Errorf("accountId = %q, want AK", resp.GetAccountId())
	}
	s3 := resp.GetCredentials().GetS3()
	if s3.GetAccessKeyId() != "AK" || s3.GetAccessSecretKey() != "SK" {
		t.Errorf("credentials = %+v", s3)
	}
	infos := resp.GetBuckets()
	if len(infos) != 1 || infos[0].GetBucketId() != "b1" {
		t.Fatalf("bucket infos = %+v", infos)
	}
	if infos[0].GetBucketInfo().GetS3().GetEndpoint() != "https://storage.h3llo.cloud" {
		t.Errorf("bucket info endpoint = %q", infos[0].GetBucketInfo().GetS3().GetEndpoint())
	}
}

func TestGrantBucketAccessMultipleBuckets(t *testing.T) {
	s := testServer(t, credsHandler)
	resp, err := s.DriverGrantBucketAccess(context.Background(),
		&cosi.DriverGrantBucketAccessRequest{
			AuthenticationType: keyAuth(),
			Buckets: []*cosi.DriverGrantBucketAccessRequest_AccessedBucket{
				{BucketId: "b1"}, {BucketId: "b2"},
			},
		})
	if err != nil {
		t.Fatalf("DriverGrantBucketAccess: %v", err)
	}
	if len(resp.GetBuckets()) != 2 {
		t.Errorf("want 2 bucket infos, got %d", len(resp.GetBuckets()))
	}
}

func TestGrantBucketAccessRejectsNonKey(t *testing.T) {
	s := testServer(t, func(http.ResponseWriter, *http.Request) {})
	_, err := s.DriverGrantBucketAccess(context.Background(),
		&cosi.DriverGrantBucketAccessRequest{
			AuthenticationType: &cosi.AuthenticationType{Type: cosi.AuthenticationType_UNKNOWN},
			Buckets:            []*cosi.DriverGrantBucketAccessRequest_AccessedBucket{{BucketId: "b1"}},
		})
	if err == nil {
		t.Fatal("expected error for non-KEY auth type")
	}
}

func TestGrantBucketAccessNoBuckets(t *testing.T) {
	s := testServer(t, func(http.ResponseWriter, *http.Request) {})
	_, err := s.DriverGrantBucketAccess(context.Background(),
		&cosi.DriverGrantBucketAccessRequest{AuthenticationType: keyAuth()})
	if err == nil {
		t.Fatal("expected error with no buckets")
	}
}

func TestGetExistingBucket(t *testing.T) {
	s := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/s3/v1/buckets/adopt-me" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	resp, err := s.DriverGetExistingBucket(context.Background(),
		&cosi.DriverGetExistingBucketRequest{ExistingBucketId: "adopt-me"})
	if err != nil {
		t.Fatalf("DriverGetExistingBucket: %v", err)
	}
	if resp.GetBucketId() != "adopt-me" {
		t.Errorf("bucketId = %q", resp.GetBucketId())
	}
	if resp.GetProtocols().GetS3().GetBucketId() != "adopt-me" {
		t.Errorf("protocols s3 = %+v", resp.GetProtocols().GetS3())
	}
}

func TestGetExistingBucketNotFound(t *testing.T) {
	s := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := s.DriverGetExistingBucket(context.Background(),
		&cosi.DriverGetExistingBucketRequest{ExistingBucketId: "ghost"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestGetExistingBucketEmptyID(t *testing.T) {
	s := testServer(t, func(http.ResponseWriter, *http.Request) {})
	if _, err := s.DriverGetExistingBucket(context.Background(),
		&cosi.DriverGetExistingBucketRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("want InvalidArgument, got %v", err)
	}
}

func TestRevokeIsNoOp(t *testing.T) {
	s := testServer(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("revoke must not call the API")
	})
	if _, err := s.DriverRevokeBucketAccess(context.Background(),
		&cosi.DriverRevokeBucketAccessRequest{AccountId: "AK"}); err != nil {
		t.Fatalf("DriverRevokeBucketAccess: %v", err)
	}
}
