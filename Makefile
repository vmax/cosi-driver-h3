IMG ?= ghcr.io/vmax/cosi-driver-h3:latest

.PHONY: build test lint vet docker push deploy clean

build:
	CGO_ENABLED=0 go build -o bin/cosi-driver-h3 ./cmd/cosi-driver-h3

test:
	go test ./... -race -cover

vet:
	go vet ./...

docker:
	docker build -t $(IMG) .

push: docker
	docker push $(IMG)

deploy:
	kubectl apply -f deploy/driver.yaml

clean:
	rm -rf bin
