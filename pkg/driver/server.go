package driver

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	cosi "sigs.k8s.io/container-object-storage-interface/proto"
)

// Server wraps a gRPC server serving the COSI Identity and Provisioner APIs
// over a unix socket consumed by the COSI sidecar.
type Server struct {
	endpoint string
	grpc     *grpc.Server
}

// NewServer registers the identity and provisioner services on a new gRPC
// server. endpoint is a unix socket URL, e.g. unix:///var/lib/cosi/cosi.sock.
func NewServer(endpoint string, cfg Config) *Server {
	g := grpc.NewServer()
	cosi.RegisterIdentityServer(g, NewIdentityServer())
	cosi.RegisterProvisionerServer(g, NewProvisionerServer(cfg))
	return &Server{endpoint: endpoint, grpc: g}
}

// Run listens on the endpoint and serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	proto, addr, err := parseEndpoint(s.endpoint)
	if err != nil {
		return err
	}
	if proto == "unix" {
		// Remove a stale socket left by an unclean shutdown.
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale socket %q: %w", addr, err)
		}
	}

	lis, err := net.Listen(proto, addr)
	if err != nil {
		return fmt.Errorf("listen on %s://%s: %w", proto, addr, err)
	}

	go func() {
		<-ctx.Done()
		klog.Info("shutting down gRPC server")
		s.grpc.GracefulStop()
	}()

	klog.InfoS("COSI driver listening", "endpoint", s.endpoint, "driver", DriverName)
	return s.grpc.Serve(lis)
}

// parseEndpoint splits a unix:// or tcp:// endpoint into network and address.
func parseEndpoint(ep string) (string, string, error) {
	if strings.HasPrefix(ep, "unix://") || strings.HasPrefix(ep, "tcp://") {
		u, err := url.Parse(ep)
		if err != nil {
			return "", "", fmt.Errorf("parse endpoint %q: %w", ep, err)
		}
		addr := u.Path
		if u.Scheme == "tcp" {
			addr = u.Host
		}
		if addr == "" {
			return "", "", fmt.Errorf("endpoint %q has empty address", ep)
		}
		return u.Scheme, addr, nil
	}
	return "", "", fmt.Errorf("unsupported endpoint %q: want unix:// or tcp://", ep)
}
