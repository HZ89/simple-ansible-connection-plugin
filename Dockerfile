# syntax=docker/dockerfile:1
FROM golang:1.22-bookworm AS build

RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2 && \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28 && \
    apt update && apt install -y protobuf-compiler libpam0g-dev
WORKDIR /server
RUN --mount=target=. \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    make MAIN_BIN=/out/server DEBUG=1 build

FROM golang:1.22-bookworm AS DLV
RUN go install github.com/go-delve/delve/cmd/dlv@latest

FROM ubuntu:22.04
WORKDIR /
COPY --from=build /out/server_linux_amd64 /
COPY --from=DLV /go/bin/dlv /
RUN adduser test
CMD ["/dlv", "--listen=:40000", "--headless=true", "--api-version=2", "--accept-multiclient", "exec", "/server_linux_amd64"]