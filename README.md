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
| `H3LLO_REGION` | no | `ru-1` | region advertised to consumers |

## Build & test

```sh
make test        # unit tests (race + cover)
make build       # local binary -> bin/
make docker IMG=...   # container image
```

## Deploy

Install via the Helm chart (see [`charts/cosi-driver-h3`](charts/cosi-driver-h3)).

1. Install COSI CRDs + controller once per cluster:
   ```sh
   kubectl apply -k 'https://github.com/kubernetes-sigs/container-object-storage-interface//?ref=release-0.2'
   ```
2. Install the driver:
   ```sh
   helm install h3-cosi ./charts/cosi-driver-h3 \
     --namespace h3llo-cosi --create-namespace \
     --set h3.projectId=<PROJECT_UUID> \
     --set credentials.keyId=<API_KEY_ID> \
     --set credentials.secretKey=<API_SECRET>
   ```
   Override the COSI sidecar image with `--set sidecar.image.repository=...,sidecar.image.tag=...`
   (add `--set imagePullSecrets[0].name=<secret>` for a private one).
3. Try it: `kubectl apply -f deploy/examples.yaml`

The granted credentials land in Secret `my-bucket-creds` (keys: `accessKeyID`,
`accessSecretKey`, `endpoint`, `region`, `bucketName`).

## Smoke test (live API)

```sh
H3_KEY_ID=... H3_SECRET_KEY=... H3LLO_PROJECT_ID=... go run -tags smoke ./cmd/smoke
```

Exercises create → grant → delete against `api.h3llo.cloud`.

## Validation status

Tested live on a k3s cluster against the real h3llo backend, COSI v1alpha2
(`sigs.k8s.io/container-object-storage-interface` @ `46fde39`):

- [x] **Create** — BucketClaim → Bucket provisioned in h3llo, `readyToUse`.
- [x] **GrantAccess** — BucketAccess → Secret with S3 creds + endpoint delivered.
- [x] **RevokeAccess** — no-op revoke, finalizer released.
- [ ] **Delete** — see limitation below.

## Known limitations

### Bucket deletion (upstream gap)

On the current COSI v1alpha2 build the **sidecar does not implement Bucket
deletion** — its bucket reconciler is a `// TODO` that removes the protection
finalizer and returns `"deletion is not yet implemented"`, so it never calls
the driver. Result: the k8s `Bucket` object is removed but the **backend bucket
is orphaned**. Tracked in
[issue #165](https://github.com/kubernetes-sigs/container-object-storage-interface/issues/165)
("Bucket Sidecar deletion"); implemented by draft
[PR #320](https://github.com/kubernetes-sigs/container-object-storage-interface/pull/320).

This driver's `DriverDeleteBucket` **is** implemented and correct (unit-tested +
standalone-verified, and matches PR #320's call contract); it is simply not
invoked yet. Until #320 merges, delete buckets out-of-band
(`DELETE /api/s3/v1/buckets/{name}`) and clear the stuck finalizer.

Also note [#227](https://github.com/kubernetes-sigs/container-object-storage-interface/issues/227)
(provision/deprovision reconcile race) — a controller restart may be needed to
unstick the first reconcile on rc builds.

## Bucket naming

By default the backend bucket is named `bc-<uuid>` (the COSI controller's name).
Set `bucketNamePrefix` on the BucketClass to prepend a prefix while keeping the
UUID suffix (so names stay unique):

```yaml
spec:
  driverName: s3.h3llo.cloud
  deletionPolicy: Delete
  parameters:
    bucketNamePrefix: "team-a-"   # → team-a-bc-<uuid>
```

h3llo bucket-name rules: `^[a-z][a-z0-9-]*[a-z0-9]$`, ≤ 63 chars. Since
`bc-<uuid>` is 39 chars, the prefix must be ≤ 24 chars and lowercase. The driver
validates the final name and rejects violations with gRPC `InvalidArgument`
(surfaced on `Bucket.status.error` + an Event). Enable the chart's optional
`ValidatingAdmissionPolicy` (`--set validatingAdmissionPolicy.enabled=true`) to
reject a bad prefix at BucketClass apply time instead.

Static / exact names are intentionally **not** supported as a parameter
(h3llo create is idempotent-by-name → multiple claims would silently share one
backend bucket, and a delete would drop shared data). To bind a specific
existing bucket, use COSI **static provisioning** instead — set
`BucketClaim.spec.existingBucketName` to the backend bucket name. The driver
implements `DriverGetExistingBucket` (verifies the bucket exists in h3llo and
returns its id + S3 protocol info).

Consumers should always read the bucket name from the BucketAccess Secret
(`COSI_S3_BUCKET_ID`), never hardcode it — the prefix is transparent to them.

## Public buckets

COSI has no public-bucket concept, so this is a driver extension via
`BucketClass.parameters`. Set `public: "true"` and the driver applies a
public-read `PutBucketPolicy` to each provisioned bucket S3-natively on the
Ceph RGW backend (the h3llo management API has no public toggle — its
`/buckets/public` is console/JWT-only):

```yaml
apiVersion: objectstorage.k8s.io/v1alpha2
kind: BucketClass
metadata:
  name: h3llo-public
spec:
  driverName: s3.h3llo.cloud
  deletionPolicy: Delete
  parameters:
    public: "true"   # also accepts "read" / "public-read"
```

Public grants anonymous **read** (`s3:GetObject`); writes still need the
BucketAccess credentials. The anonymous object URL is tenant-prefixed:

```
https://storage.h3llo.cloud/{projectId-with-dashes-as-underscores}:{bucketName}/{key}
```

(COSI's v1alpha2 bucket-info is a fixed struct with no free-form field, so the
public URL isn't surfaced through the credentials Secret — construct it from the
project ID + bucket ID.)

## License

[Apache-2.0](LICENSE). Built independently; the h3 HMAC auth scheme is a clean-room reimplementation of the wire protocol used by [terraform-provider-h3](https://github.com/h3llo-cloud/terraform-provider-h3) (no code copied).
