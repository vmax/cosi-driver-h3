# syntax=docker/dockerfile:1
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOFLAGS=-trimpath go build -ldflags="-s -w" \
    -o /out/cosi-driver-h3 ./cmd/cosi-driver-h3

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/cosi-driver-h3 /cosi-driver-h3
USER 65532:65532
ENTRYPOINT ["/cosi-driver-h3"]
