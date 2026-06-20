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

// DriverGetInfo returns the driver's provisioner name and the protocols it
// supports. The COSI sidecar uses the name to stamp objects so it only acts on
// its own.
func (s *IdentityServer) DriverGetInfo(
	_ context.Context, _ *cosi.DriverGetInfoRequest,
) (*cosi.DriverGetInfoResponse, error) {
	return &cosi.DriverGetInfoResponse{
		Name: DriverName,
		SupportedProtocols: []*cosi.ObjectProtocol{
			{Type: cosi.ObjectProtocol_S3},
		},
	}, nil
}
