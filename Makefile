.PHONY: build build-frontend test fmt vet package-deb clean check-version

# Releases only: the version comes from the tag on HEAD, empty when there is none.
VERSION ?= $(patsubst v%,%,$(shell git describe --tags --exact-match 2>/dev/null))
RELEASE ?= $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "0")
BINARY := fastoshop
BIN_DIR := build/bin
MODULE := github.com/fastogt/fastoshop
# Версия вшивается линковщиком: иначе `fastoshop -version` врёт о том,
# какой релиз стоит у клиента.
LDFLAGS := -ldflags "-s -w -X $(MODULE)/app/version.VersionApp=$(VERSION)-$(RELEASE)"

check-version:
	@test -n "$(VERSION)" || { \
		echo "HEAD is not tagged: tag a release (git tag -a vX.Y.Z -m ...) or pass VERSION=X.Y.Z"; \
		exit 1; \
	}

build: check-version
	@mkdir -p $(BIN_DIR)
	cd src && go build $(LDFLAGS) -o ../$(BIN_DIR)/$(BINARY) ./cmd/fastoshop.go

build-frontend:
	cd web && npm install && npm run build

test:
	cd src && go test ./...

fmt:
	cd src && go fmt ./...

vet:
	cd src && go vet ./...

package-deb: build build-frontend
	VERSION=$(VERSION) RELEASE=$(RELEASE) ARCH=amd64 nfpm package -f nfpm.yaml -p deb -t build/

clean:
	rm -rf build web/dist
