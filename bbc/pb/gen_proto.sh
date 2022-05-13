#!/usr/bin/env sh

src_path="$(realpath "${0%/*}")"
cd "$src_path" && \
    protoc --go_out=. --go-grpc_out=. \
    --go_opt=paths=source_relative \
    --go-grpc_opt=paths=source_relative \
    bbc.proto

