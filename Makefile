GOPATH := $(shell go env GOPATH)

ifeq ($(PREFIX),)
    PREFIX := /usr/local
endif

fk: main.go fk-client/*.go cmd/*.go
	go build -o fk .

clean:
	rm fk

schema:
	wget -q http://localhost:8080/open-api-spec.json -O schema.json
	oapi-codegen -package fk -o fk-client/client.gen.go schema.json 

test:
	go test ./...

test_coverage:
	go test ./... -coverprofile=coverage.out

dep:
	go get github.com/deepmap/oapi-codegen/cmd/oapi-codegen
	go mod download

run:
	go run .

vet:
	go vet

lint:
	${GOPATH}/bin/golangci-lint run

install: fk
	install -m 755 fk $(PREFIX)/bin/fk
