# cosi-driver-h3

[![CI](https://github.com/vmax/cosi-driver-h3/actions/workflows/ci.yml/badge.svg)](https://github.com/vmax/cosi-driver-h3/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

A [Kubernetes COSI](https://kubernetes.io/blog/2022/09/02/cosi-kubernetes-object-storage-management/)
(Container Object Storage Interface) driver for [h3llo.cloud](https://app.h3llo.cloud)
S3 buckets. Lets you provision buckets and hand credentials to workloads with
plain Kubernetes objects — no SDK in your app.

## How it works

```
BucketClaim / BucketAccessClaim   (you create these)
        │
        ▼
COSI controller  ──►  Bucket / BucketAccess        (sigs.k8s.io, install once)
        │
        ▼
COSI sidecar  ──gRPC unix socket──►  THIS DRIVER  ──HTTPS──►  h3llo S3 mgmt API
```

The driver implements the COSI `Identity` + `Provisioner` gRPC services and maps
each call to the h3llo management API:

| COSI gRPC | h3llo API |
|---|---|
| `DriverGetInfo` | static name `s3.h3llo.cloud` |
| `DriverCreateBucket` | `POST /api/s3/v1/buckets` `{project_id,name}` |
| `DriverDeleteBucket` | `DELETE /api/s3/v1/buckets/{name}?project_id=` |
| `DriverGrantBucketAccess` | `POST /api/s3/v1/buckets` (idempotent) → creds inline |
| `DriverRevokeBucketAccess` | no-op (see below) |

All calls hit `https://api.h3llo.cloud` and are **HMAC-SHA256 signed** with an
API key-id + secret (same scheme as
[terraform-provider-h3](https://github.com/h3llo-cloud/terraform-provider-h3)).

### h3llo specifics

- **Bucket identity is its name.** We use the bucket name as the COSI
  `bucket_id`. Create is idempotent (re-create returns 201 + same creds);
  delete is idempotent (404 → success).
- **One shared key pair per project.** h3llo has no per-bucket access API; every
  `POST /buckets` returns the project's single `access_key_id`/
  `secret_access_key`, good for all buckets. `DriverGrantBucketAccess` re-issues
  the (idempotent) create to obtain that pair.
- **Revoke is a no-op.** Revoking one bucket would break the shared key for all
  others. Rotate the project key out-of-band instead.
- **Auth type:** only `Key` is supported (not `IAM`).

## Configuration

Driver reads config from env:

| Env | Required | Default | Notes |
|---|---|---|---|
| `H3_KEY_ID` | yes | — | API key id (UUID), console → keys → api-keys |
| `H3_SECRET_KEY` | yes | — | API secret (`h3_...`) for HMAC signing |
| `H3LLO_PROJECT_ID` | yes | — | project UUID |
| `H3_API_ENDPOINT` | no | `https://api.h3llo.cloud` | public API base |
| `H3LLO_S3_ENDPOINT` | no | `https://storage.h3llo.cloud` | S3 endpoint handed to consumers |
| `H3LLO_REGION` | no | `us-east-1` | region advertised to consumers |

## Build & test

```sh
make test        # unit tests (race + cover)
make build       # local binary -> bin/
make docker IMG=...   # container image
```

## Deploy

1. Install COSI CRDs + controller (once per cluster) from
   [kubernetes-sigs/container-object-storage-interface](https://github.com/kubernetes-sigs/container-object-storage-interface).
2. Set `H3_KEY_ID` / `H3_SECRET_KEY` / `H3LLO_PROJECT_ID` in the Secret in `deploy/driver.yaml`.
3. `kubectl apply -f deploy/driver.yaml`
4. Try it: `kubectl apply -f deploy/examples.yaml`

The granted credentials land in Secret `my-bucket-creds` (keys: `accessKeyID`,
`accessSecretKey`, `endpoint`, `region`, `bucketName`).

## Smoke test (live API)

```sh
H3_KEY_ID=... H3_SECRET_KEY=... H3LLO_PROJECT_ID=... go run -tags smoke ./cmd/smoke
```

Exercises create → grant → delete against `api.h3llo.cloud`.

## Status / TODO

- [x] HMAC auth against `api.h3llo.cloud` — verified live (create/grant/delete).
- [x] S3 data-plane endpoint: `https://storage.h3llo.cloud` (Ceph Object Gateway, SigV4).
- [ ] Pin the COSI sidecar image tag in `deploy/driver.yaml` to a release you trust.

## License

[Apache-2.0](LICENSE). Built independently; the h3 HMAC auth scheme is a clean-room reimplementation of the wire protocol used by [terraform-provider-h3](https://github.com/h3llo-cloud/terraform-provider-h3) (no code copied).
