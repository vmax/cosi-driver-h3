# cosi-driver-h3 Helm chart

Installs the COSI driver for h3llo.cloud S3 (driver + COSI sidecar, SA, RBAC,
credentials Secret, and optional BucketClass/BucketAccessClass).

## Prerequisites

COSI CRDs + controller must be installed once per cluster:

```sh
kubectl apply -k 'https://github.com/kubernetes-sigs/container-object-storage-interface//?ref=release-0.2'
```

## Install

```sh
helm install h3-cosi ./charts/cosi-driver-h3 \
  --namespace h3llo-cosi --create-namespace \
  --set h3.projectId=<PROJECT_UUID> \
  --set credentials.keyId=<API_KEY_ID> \
  --set credentials.secretKey=<API_SECRET>
```

Or reference an existing Secret (keys `H3_KEY_ID`, `H3_SECRET_KEY`,
`H3LLO_PROJECT_ID`):

```sh
helm install h3-cosi ./charts/cosi-driver-h3 \
  --namespace h3llo-cosi --create-namespace \
  --set credentials.existingSecret=my-h3-creds
```

## Key values

| Key | Default | Notes |
|---|---|---|
| `h3.projectId` | `""` | **required** project UUID |
| `credentials.keyId` / `credentials.secretKey` | `""` | HMAC key (required unless `existingSecret`) |
| `credentials.existingSecret` | `""` | use a pre-made Secret instead |
| `h3.apiEndpoint` | `https://api.h3llo.cloud` | management API |
| `h3.s3Endpoint` | `https://storage.h3llo.cloud` | S3 data plane |
| `h3.region` | `ru-1` | |
| `sidecar.image.tag` | pinned rc | pin to a trusted COSI sidecar release |
| `bucketClass.create` / `bucketAccessClass.create` | `true` | create default classes |

## Known limitation

Bucket deletion depends on upstream COSI sidecar support
([PR #320](https://github.com/kubernetes-sigs/container-object-storage-interface/pull/320)).
On builds without it, deleting a BucketClaim removes the k8s objects but may
orphan the backend bucket. See the project README.
