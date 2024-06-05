PACKAGE := github.com/HZ89/simple-ansible-connection-plugin

# REGISTRY_PREFIX :=

GO_LOCAL_VERSION = $(shell go version | cut -f3 -d' ' | cut -c 3-)

ifeq ($(origin GO_LOCAL_VERSION),undefined)
GO_LOCAL_VERSION := 1.21
endif

MAIN_BIN := target/ansible-grpc-connection-server

GIT_COMMIT:=$(shell git rev-parse "HEAD^{commit}" 2>/dev/null)

# the raw git version from `git describe` -- our starting point
GIT_VERSION_RAW:=$(shell git describe --tags --abbrev=14 "$(GIT_COMMIT)^{commit}" 2>/dev/null)

# use the number of dashes in the raw version to figure out what kind of
# version this is, and turn it into a semver-compatible version
DASHES_IN_VERSION:=$(shell echo "$(GIT_VERSION_RAW)" | sed "s/[^-]//g")

# just use the raw version by default
GIT_VERSION:=$(GIT_VERSION_RAW)

ifeq ($(DASHES_IN_VERSION), ---)
# we have a distance to a subversion (v1.1.0-subversion-1-gCommitHash)
GIT_VERSION:=$(shell echo "$(GIT_VERSION_RAW)" | sed "s/-\([0-9]\{1,\}\)-g\([0-9a-f]\{14\}\)$$/.\1\+\2/")
endif
ifeq ($(DASHES_IN_VERSION), --)
# we have distance to base tag (v1.1.0-1-gCommitHash)
GIT_VERSION:=$(shell echo "$(GIT_VERSION_RAW)" | sed "s/-g\([0-9a-f]\{14\}\)$$/+\1/")
endif

# figure out if we have new or changed files
ifeq ($(shell git status --porcelain 2>/dev/null),)
GIT_TREE_STATE:=clean
else
# append the -dirty manually, since `git describe --dirty` only considers
# changes to existing files
GIT_TREE_STATE:=dirty
GIT_VERSION:=$(GIT_VERSION)-dirty
endif

# construct a "shorter" version without the commit info, etc for use as container image tag, etc
VERSION?=$(shell echo "$(GIT_VERSION)" | grep -E -o '^v[[:digit:]]+\.[[:digit:]]+\.[[:digit:]]+(-(alpha|beta)\.[[:digit:]]+)?')

# construct the build date, taking into account SOURCE_DATE_EPOCH, which is
# used for the purpose of reproducible builds
ifdef SOURCE_DATE_EPOCH
BUILD_DATE:=$(shell date --date=@${SOURCE_DATE_EPOCH} -u +'%Y-%m-%dT%H:%M:%SZ')
else
BUILD_DATE:=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
endif

VERSION_PACKAGE := $(PACKAGE)/server/version
# set the build information version ldflags (but not other ldflags)
VERSION_LDFLAGS := -X $(VERSION_PACKAGE).gitVersion=$(GIT_VERSION) -X $(VERSION_PACKAGE).gitCommit=$(GIT_COMMIT) -X $(VERSION_PACKAGE).gitTreeState=$(GIT_TREE_STATE) -X $(VERSION_PACKAGE).buildDate=$(BUILD_DATE)
ARCH?=$(shell uname -m | sed s/aarch64/arm64/ | sed s/x86_64/amd64/)
OS?=$(shell uname -s |  tr '[:upper:]' '[:lower:]')

.PHONY: all
all: clean gen build

.PHONY: gen
gen: # @HELP generate protobuf codes
gen:
	@echo "generate golang and python protobuf codes"
	@protoc -I=./idl --go-grpc_out=. --go_out=. ./idl/connect.proto
	@python -m grpc_tools.protoc -I ./idl --python_out=./plugin --pyi_out=./plugin --grpc_python_out=./plugin ./idl/connect.proto

.PHONY: build
build: # @HELP build binaries
build: $(OS)

.PHONY: linux
linux:
	@echo "Building linux/$(ARCH) binary '$(VERSION)'"
	@mkdir -p target
	@CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build \
		-o $(MAIN_BIN)_linux_$(ARCH) \
		-ldflags "$(VERSION_LDFLAGS)" \
		./server/main.go

.PHONY: darwin
darwin:
	@echo "Building darwin/$(ARCH) binary '$(VERSION)'"
	@mkdir -p target
	@CGO_ENABLED=0 GOOS=darwin GOARCH=$(ARCH) go build \
		-o $(MAIN_BIN)_darwin_$(ARCH) \
		-ldflags "$(VERSION_LDFLAGS)" \
		./server/main.go

.PHONY: windows
windows:
	@echo "Building windows/$(ARCH) binary '$(VERSION)'"
	@mkdir -p target
	@CGO_ENABLED=0 GOOS=windows GOARCH=$(ARCH) go build \
		-o $(MAIN_BIN)_windows_$(ARCH).exe \
		-ldflags "$(VERSION_LDFLAGS)" \
		./server/main.go

.PHONY: test
test: # @HELP run a local test
	@echo "Run local test"
	@./test.sh

.PHONY: version
version: # @HELP outputs the version string
version:
	@echo "Version: $(GIT_VERSION) ($(VERSION))"
	@echo "    built from $(GIT_COMMIT) ($(GIT_TREE_STATE))"
	@echo "    built on $(BUILD_DATE)"

clean: # @HELP removes built binaries and temporary files
clean: bin-clean gen-clean

bin-clean:
	@rm -rf target

gen-clean:
	@rm -rf ./plugin/*pb2*
	@rm -rf ./server/connection/*pb.go

help: # @HELP prints this message
help:
	@echo "TARGETS:"
	@grep -E '^.*: *# *@HELP' $(MAKEFILE_LIST)    \
	    | awk '                                   \
	        BEGIN {FS = ": *# *@HELP"};           \
	        { printf "  %-30s %s\n", $$1, $$2 };  \
	    '
