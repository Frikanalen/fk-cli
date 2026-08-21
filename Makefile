GOPATH := $(shell go env GOPATH)

# Stamped into the binary so `fk --version` reports something useful.
# Release builds pass the tag name here instead; see .github/workflows/release.yaml.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github/frikanalen/fk-cli/cmd.version=$(VERSION)

ifeq ($(PREFIX),)
    PREFIX := /usr/local
endif

fk: generate main.go fk-client/*.go cmd/*.go
	go build -ldflags "$(LDFLAGS)" -o fk .

clean:
	rm -f fk
	rm -rf fk-client/generated

# Fetches the current OpenAPI schema from a running backend into schema.yaml.
schema:
	./update-schema.sh $(if $(API),$(API)/api/schema/)

# Regenerates the Go API client from the schema.yaml checked into the repo.
generate:
	./generate-client.sh

test: generate
	go test ./...

test_coverage: generate
	go test ./... -coverprofile=coverage.out

dep:
	go mod download

run: generate
	go run .

vet: generate
	go vet ./...

lint: generate
	${GOPATH}/bin/golangci-lint run

install: fk
	install -m 755 fk $(PREFIX)/bin/fk

.PHONY: fk clean schema generate test test_coverage dep run vet lint install
