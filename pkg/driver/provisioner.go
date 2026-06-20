package driver

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	cosi "sigs.k8s.io/container-object-storage-interface/proto"

	"github.com/vmax/cosi-driver-h3/pkg/h3llo"
)

// ProvisionerServer implements cosi.ProvisionerServer against h3llo.cloud.
type ProvisionerServer struct {
	cosi.UnimplementedProvisionerServer
	cfg    Config
	client *h3llo.Client
}

// NewProvisionerServer builds a ProvisionerServer.
func NewProvisionerServer(cfg Config) *ProvisionerServer {
	return &ProvisionerServer{cfg: cfg, client: cfg.newClient()}
}

// DriverCreateBucket creates a bucket in h3llo. The bucket is identified by
// its name; we use the name as the COSI bucket_id. Idempotent.
func (s *ProvisionerServer) DriverCreateBucket(
	ctx context.Context, req *cosi.DriverCreateBucketRequest,
) (*cosi.DriverCreateBucketResponse, error) {
	name := req.GetName()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "bucket name is required")
	}
	klog.InfoS("DriverCreateBucket", "name", name)

	if _, err := s.client.CreateBucket(ctx, name); err != nil {
		return nil, status.Errorf(codes.Internal, "create bucket: %v", err)
	}

	return &cosi.DriverCreateBucketResponse{
		BucketId: name,
		BucketInfo: &cosi.Protocol{
			Type: &cosi.Protocol_S3{
				S3: &cosi.S3{
					Region:           s.cfg.Region,
					SignatureVersion: cosi.S3SignatureVersion_S3V4,
				},
			},
		},
	}, nil
}

// DriverDeleteBucket deletes a bucket by name (bucket_id == name). Idempotent.
func (s *ProvisionerServer) DriverDeleteBucket(
	ctx context.Context, req *cosi.DriverDeleteBucketRequest,
) (*cosi.DriverDeleteBucketResponse, error) {
	name := req.GetBucketId()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "bucket_id is required")
	}
	klog.InfoS("DriverDeleteBucket", "bucketId", name)

	if err := s.client.DeleteBucket(ctx, name); err != nil {
		return nil, status.Errorf(codes.Internal, "delete bucket: %v", err)
	}
	return &cosi.DriverDeleteBucketResponse{}, nil
}

// DriverGrantBucketAccess returns credentials for accessing the bucket.
//
// h3llo has no per-bucket access API: a single shared key pair grants access
// to every bucket in the project, and the public API returns that pair from
// the (idempotent) create-bucket call. We re-issue create — which by then has
// already succeeded — to obtain the shared credentials. account_id is the
// access key ID.
func (s *ProvisionerServer) DriverGrantBucketAccess(
	ctx context.Context, req *cosi.DriverGrantBucketAccessRequest,
) (*cosi.DriverGrantBucketAccessResponse, error) {
	bucket := req.GetBucketId()
	if bucket == "" {
		return nil, status.Error(codes.InvalidArgument, "bucket_id is required")
	}
	if req.GetAuthenticationType() != cosi.AuthenticationType_Key {
		return nil, status.Errorf(codes.InvalidArgument,
			"unsupported authentication type %v: only Key is supported", req.GetAuthenticationType())
	}
	klog.InfoS("DriverGrantBucketAccess", "bucketId", bucket, "name", req.GetName())

	creds, err := s.client.CreateBucket(ctx, bucket)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetch access credentials: %v", err)
	}

	// Secret keys follow the COSI S3 convention so consumers can mount and
	// use them directly. endpoint/bucketName live here because the S3
	// Protocol message carries neither.
	secrets := map[string]string{
		"accessKeyID":     creds.AccessKeyID,
		"accessSecretKey": creds.SecretAccessKey,
		"endpoint":        s.cfg.S3Endpoint,
		"region":          s.cfg.Region,
		"bucketName":      bucket,
	}

	return &cosi.DriverGrantBucketAccessResponse{
		AccountId: creds.AccessKeyID,
		Credentials: map[string]*cosi.CredentialDetails{
			"s3": {Secrets: secrets},
		},
	}, nil
}

// DriverRevokeBucketAccess is a no-op.
//
// The project shares one key pair across all buckets, so revoking access for
// a single bucket would break every other bucket. Rotating the project key is
// an out-of-band operation. We return success so COSI can finalize the
// BucketAccess object.
func (s *ProvisionerServer) DriverRevokeBucketAccess(
	_ context.Context, req *cosi.DriverRevokeBucketAccessRequest,
) (*cosi.DriverRevokeBucketAccessResponse, error) {
	klog.InfoS("DriverRevokeBucketAccess: no-op (shared project key)",
		"bucketId", req.GetBucketId(), "accountId", req.GetAccountId())
	return &cosi.DriverRevokeBucketAccessResponse{}, nil
}
