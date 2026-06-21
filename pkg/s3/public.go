// Package s3 holds S3 data-plane operations the driver performs directly
// against the backend (Ceph RGW), beyond what the h3llo management API exposes.
package s3

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config is the S3 connection for a bucket operation.
type Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
}

// newClient builds a path-style S3 client (Ceph RGW serves path-style).
func newClient(c Config) *s3.Client {
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(c.Endpoint),
		Region:       c.Region,
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, ""),
	})
}

// publicReadPolicy returns a bucket policy granting anonymous s3:GetObject on
// all objects in the bucket.
func publicReadPolicy(bucket string) string {
	return `{"Version":"2012-10-17","Statement":[{"Sid":"PublicRead","Effect":"Allow","Principal":"*","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::` + bucket + `/*"]}]}`
}

// MakeBucketPublicRead applies a public-read bucket policy. Idempotent: the
// policy is overwritten each call, so it is safe to re-run on bucket re-create.
func MakeBucketPublicRead(ctx context.Context, c Config, bucket string) error {
	cli := newClient(c)
	_, err := cli.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucket),
		Policy: aws.String(publicReadPolicy(bucket)),
	})
	if err != nil {
		return fmt.Errorf("put public-read policy on %q: %w", bucket, err)
	}
	return nil
}
