package driver

import (
	"context"

	cosi "sigs.k8s.io/container-object-storage-interface/proto"
)

// IdentityServer implements cosi.IdentityServer.
type IdentityServer struct {
	cosi.UnimplementedIdentityServer
}

// NewIdentityServer builds an IdentityServer.
func NewIdentityServer() *IdentityServer { return &IdentityServer{} }

// DriverGetInfo returns the driver's provisioner name. The COSI sidecar uses
// this to stamp Bucket/BucketAccess objects so it only acts on its own.
func (s *IdentityServer) DriverGetInfo(
	_ context.Context, _ *cosi.DriverGetInfoRequest,
) (*cosi.DriverGetInfoResponse, error) {
	return &cosi.DriverGetInfoResponse{Name: DriverName}, nil
}
