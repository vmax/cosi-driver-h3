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

// s3BucketInfo builds the S3 protocol bucket info handed to consumers.
// h3llo (Ceph RGW) uses path-style addressing.
func (s *ProvisionerServer) s3BucketInfo(bucketID string) *cosi.ObjectProtocolAndBucketInfo {
	return &cosi.ObjectProtocolAndBucketInfo{
		S3: &cosi.S3BucketInfo{
			BucketId:        bucketID,
			Endpoint:        s.cfg.S3Endpoint,
			Region:          s.cfg.Region,
			AddressingStyle: &cosi.S3AddressingStyle{Style: cosi.S3AddressingStyle_PATH},
		},
	}
}

// DriverCreateBucket creates a bucket in h3llo. The bucket is identified by its
// name; we use the name as the COSI bucket_id. Idempotent.
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
		BucketId:  name,
		Protocols: s.s3BucketInfo(name),
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

// DriverGrantBucketAccess returns credentials for accessing the requested
// buckets.
//
// h3llo has no per-bucket access API: a single shared key pair grants access to
// every bucket in the project, and the public API returns that pair from the
// (idempotent) create-bucket call. We fetch it once and return it for all
// requested buckets. account_id is the access key ID.
func (s *ProvisionerServer) DriverGrantBucketAccess(
	ctx context.Context, req *cosi.DriverGrantBucketAccessRequest,
) (*cosi.DriverGrantBucketAccessResponse, error) {
	buckets := req.GetBuckets()
	if len(buckets) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one bucket is required")
	}
	if req.GetAuthenticationType().GetType() != cosi.AuthenticationType_KEY {
		return nil, status.Errorf(codes.InvalidArgument,
			"unsupported authentication type %v: only KEY is supported",
			req.GetAuthenticationType().GetType())
	}
	klog.InfoS("DriverGrantBucketAccess", "account", req.GetAccountName(), "buckets", len(buckets))

	// The key is project-wide; one (idempotent) create returns it for any bucket.
	creds, err := s.client.CreateBucket(ctx, buckets[0].GetBucketId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetch access credentials: %v", err)
	}

	infos := make([]*cosi.DriverGrantBucketAccessResponse_BucketInfo, 0, len(buckets))
	for _, b := range buckets {
		id := b.GetBucketId()
		infos = append(infos, &cosi.DriverGrantBucketAccessResponse_BucketInfo{
			BucketId:   id,
			BucketInfo: s.s3BucketInfo(id),
		})
	}

	return &cosi.DriverGrantBucketAccessResponse{
		AccountId: creds.AccessKeyID,
		Buckets:   infos,
		Credentials: &cosi.CredentialInfo{
			S3: &cosi.S3CredentialInfo{
				AccessKeyId:     creds.AccessKeyID,
				AccessSecretKey: creds.SecretAccessKey,
			},
		},
	}, nil
}

// DriverRevokeBucketAccess is a no-op.
//
// The project shares one key pair across all buckets, so revoking access for a
// single bucket would break every other bucket. Rotating the project key is an
// out-of-band operation. We return success so COSI can finalize the
// BucketAccess object.
func (s *ProvisionerServer) DriverRevokeBucketAccess(
	_ context.Context, req *cosi.DriverRevokeBucketAccessRequest,
) (*cosi.DriverRevokeBucketAccessResponse, error) {
	klog.InfoS("DriverRevokeBucketAccess: no-op (shared project key)",
		"accountId", req.GetAccountId())
	return &cosi.DriverRevokeBucketAccessResponse{}, nil
}
