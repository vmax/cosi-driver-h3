// Command h3llo-cosi-driver runs the COSI provisioner driver for h3llo.cloud
// object storage. It serves the COSI Identity and Provisioner gRPC APIs over a
// unix socket that the COSI sidecar connects to.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/klog/v2"

	"github.com/vmax/cosi-driver-h3/pkg/driver"
)

func main() {
	klog.InitFlags(nil)
	endpoint := flag.String("endpoint", envOr("COSI_ENDPOINT", "unix:///var/lib/cosi/cosi.sock"),
		"COSI gRPC endpoint (unix:// or tcp://)")
	flag.Parse()

	cfg, err := driver.ConfigFromEnv()
	if err != nil {
		klog.ErrorS(err, "invalid configuration")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := driver.NewServer(*endpoint, cfg)
	if err := srv.Run(ctx); err != nil {
		klog.ErrorS(err, "server exited")
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
