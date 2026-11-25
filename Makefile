# netgrab developer tasks.

GOPATH_BIN := $(shell go env GOPATH)/bin

.PHONY: proto build test tidy

# Regenerate Go from the protobuf model. Requires protoc plus
# protoc-gen-go (go install google.golang.org/protobuf/cmd/protoc-gen-go@latest).
proto:
	PATH="$(PATH):$(GOPATH_BIN)" protoc --go_out=. --go_opt=module=netgrab -I proto proto/netgrab/v1/device.proto

build:
	go build ./...

test:
	go test ./...

tidy:
	go mod tidy
