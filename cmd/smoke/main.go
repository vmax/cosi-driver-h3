//go:build smoke

// Smoke test against the live h3llo API. Run:
//
//	H3_KEY_ID=... H3_SECRET_KEY=... H3LLO_PROJECT_ID=... go run -tags smoke ./cmd/smoke
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/vmax/cosi-driver-h3/pkg/driver"
	cosi "sigs.k8s.io/container-object-storage-interface/proto"
)

func main() {
	cfg, err := driver.ConfigFromEnv()
	if err != nil {
		fmt.Println("config:", err)
		os.Exit(1)
	}
	p := driver.NewProvisionerServer(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	name := "cosi-smoke-test"

	fmt.Println("== CreateBucket", name)
	cr, err := p.DriverCreateBucket(ctx, &cosi.DriverCreateBucketRequest{Name: name})
	check("create", err)
	fmt.Printf("   bucketId=%s region=%s\n", cr.GetBucketId(), cr.GetBucketInfo().GetS3().GetRegion())

	fmt.Println("== GrantBucketAccess")
	gr, err := p.DriverGrantBucketAccess(ctx, &cosi.DriverGrantBucketAccessRequest{
		BucketId: name, Name: "smoke", AuthenticationType: cosi.AuthenticationType_Key,
	})
	check("grant", err)
	s := gr.GetCredentials()["s3"].GetSecrets()
	fmt.Printf("   accountId=%s ak=%s... endpoint=%s bucket=%s\n",
		gr.GetAccountId(), trunc(s["accessKeyID"]), s["endpoint"], s["bucketName"])

	fmt.Println("== DeleteBucket")
	_, err = p.DriverDeleteBucket(ctx, &cosi.DriverDeleteBucketRequest{BucketId: name})
	check("delete", err)
	fmt.Println("== OK all live calls passed")
}

func check(step string, err error) {
	if err != nil {
		fmt.Printf("   FAIL %s: %v\n", step, err)
		os.Exit(1)
	}
}

func trunc(s string) string {
	if len(s) > 4 {
		return s[:4]
	}
	return s
}
