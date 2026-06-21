package s3

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicReadPolicy(t *testing.T) {
	var p struct {
		Statement []struct {
			Effect    string
			Principal any
			Action    []string
			Resource  []string
		}
	}
	if err := json.Unmarshal([]byte(publicReadPolicy("my-bucket")), &p); err != nil {
		t.Fatalf("policy not valid JSON: %v", err)
	}
	if len(p.Statement) != 1 {
		t.Fatalf("want 1 statement, got %d", len(p.Statement))
	}
	s := p.Statement[0]
	if s.Effect != "Allow" || s.Principal != "*" {
		t.Errorf("effect/principal = %q/%v", s.Effect, s.Principal)
	}
	if s.Action[0] != "s3:GetObject" {
		t.Errorf("action = %v", s.Action)
	}
	if s.Resource[0] != "arn:aws:s3:::my-bucket/*" {
		t.Errorf("resource = %v", s.Resource)
	}
}

func TestMakeBucketPublicRead(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	var sawPolicyQuery bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_, sawPolicyQuery = r.URL.Query()["policy"]
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := MakeBucketPublicRead(context.Background(), Config{
		Endpoint: srv.URL, Region: "ru-1", AccessKeyID: "AK", SecretAccessKey: "SK",
	}, "my-bucket")
	if err != nil {
		t.Fatalf("MakeBucketPublicRead: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", gotMethod)
	}
	if !sawPolicyQuery {
		t.Error("missing ?policy query")
	}
	if !strings.Contains(gotPath, "my-bucket") {
		t.Errorf("path = %q, want bucket name (path-style)", gotPath)
	}
	if !strings.Contains(gotBody, `"Principal":"*"`) {
		t.Errorf("body missing public principal: %s", gotBody)
	}
}

func TestMakeBucketPublicReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code></Error>`)
	}))
	defer srv.Close()
	err := MakeBucketPublicRead(context.Background(), Config{
		Endpoint: srv.URL, Region: "ru-1", AccessKeyID: "AK", SecretAccessKey: "SK",
	}, "b")
	if err == nil {
		t.Fatal("expected error on 403")
	}
}
