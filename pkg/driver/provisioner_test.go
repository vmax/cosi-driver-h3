package driver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	cosi "sigs.k8s.io/container-object-storage-interface/proto"
)

func testServer(t *testing.T, h http.HandlerFunc) *ProvisionerServer {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := Config{
		S3Endpoint: "https://s3.h3llo.cloud",
		Region:     "ru-1",
		APIBaseURL: srv.URL,
		KeyID:      "kid",
		SecretKey:  "sek",
		ProjectID:  "proj-1",
	}
	return NewProvisionerServer(cfg)
}

func TestCreateBucketReturnsIDAndProtocol(t *testing.T) {
	s := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"credentials":{"access_key_id":"AK","secret_access_key":"SK"}}`))
	})
	resp, err := s.DriverCreateBucket(context.Background(),
		&cosi.DriverCreateBucketRequest{Name: "b1"})
	if err != nil {
		t.Fatalf("DriverCreateBucket: %v", err)
	}
	if resp.GetBucketId() != "b1" {
		t.Errorf("bucketId = %q, want b1", resp.GetBucketId())
	}
	s3 := resp.GetBucketInfo().GetS3()
	if s3 == nil || s3.GetRegion() != "ru-1" {
		t.Errorf("bucketInfo S3 = %+v", s3)
	}
}

func TestCreateBucketEmptyName(t *testing.T) {
	s := testServer(t, func(http.ResponseWriter, *http.Request) {})
	if _, err := s.DriverCreateBucket(context.Background(),
		&cosi.DriverCreateBucketRequest{}); err == nil {
		t.Fatal("expected error on empty name")
	}
}

func TestGrantBucketAccessReturnsSharedKey(t *testing.T) {
	s := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"credentials":{"access_key_id":"AK","secret_access_key":"SK"}}`))
	})
	resp, err := s.DriverGrantBucketAccess(context.Background(),
		&cosi.DriverGrantBucketAccessRequest{
			BucketId:           "b1",
			Name:               "acc",
			AuthenticationType: cosi.AuthenticationType_Key,
		})
	if err != nil {
		t.Fatalf("DriverGrantBucketAccess: %v", err)
	}
	if resp.GetAccountId() != "AK" {
		t.Errorf("accountId = %q, want AK", resp.GetAccountId())
	}
	creds := resp.GetCredentials()["s3"].GetSecrets()
	if creds["accessKeyID"] != "AK" || creds["accessSecretKey"] != "SK" {
		t.Errorf("creds = %+v", creds)
	}
	if creds["bucketName"] != "b1" || creds["endpoint"] != "https://s3.h3llo.cloud" {
		t.Errorf("creds endpoint/bucket = %+v", creds)
	}
}

func TestGrantBucketAccessRejectsIAM(t *testing.T) {
	s := testServer(t, func(http.ResponseWriter, *http.Request) {})
	_, err := s.DriverGrantBucketAccess(context.Background(),
		&cosi.DriverGrantBucketAccessRequest{
			BucketId:           "b1",
			AuthenticationType: cosi.AuthenticationType_IAM,
		})
	if err == nil {
		t.Fatal("expected error for IAM auth type")
	}
}

func TestRevokeIsNoOp(t *testing.T) {
	s := testServer(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("revoke must not call the API")
	})
	if _, err := s.DriverRevokeBucketAccess(context.Background(),
		&cosi.DriverRevokeBucketAccessRequest{BucketId: "b1", AccountId: "AK"}); err != nil {
		t.Fatalf("DriverRevokeBucketAccess: %v", err)
	}
}
