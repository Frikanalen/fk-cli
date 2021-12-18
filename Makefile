GOPATH := $(shell go env GOPATH)

test:
	go test ./...

test_coverage:
	go test ./... -coverprofile=coverage.out

dep:
	go mod download

build:
	go build -o fk .

run:
	go run .

vet:
	go vet

lint:
	${GOPATH}/bin/golangci-lint run