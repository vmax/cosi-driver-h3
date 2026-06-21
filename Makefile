IMG ?= ghcr.io/vmax/cosi-driver-h3:latest
NAMESPACE ?= h3llo-cosi
RELEASE ?= h3-cosi

.PHONY: build test lint vet docker push deploy lint-chart clean

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

lint-chart:
	helm lint charts/cosi-driver-h3 --set credentials.keyId=x,credentials.secretKey=x,h3.projectId=x

# Requires H3_KEY_ID, H3_SECRET_KEY, H3LLO_PROJECT_ID in the environment.
deploy:
	helm upgrade --install $(RELEASE) charts/cosi-driver-h3 \
	  --namespace $(NAMESPACE) --create-namespace \
	  --set h3.projectId=$(H3LLO_PROJECT_ID) \
	  --set credentials.keyId=$(H3_KEY_ID) \
	  --set credentials.secretKey=$(H3_SECRET_KEY)

clean:
	rm -rf bin
