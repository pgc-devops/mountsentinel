BINARY     := mountsentinel
CMD        := ./cmd/mountsentinel
DIST       := ./dist
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS    := -ldflags "-X main.version=$(VERSION) -s -w"
INSTALL    := /usr/local/bin
NFPM       ?= $(shell command -v nfpm 2>/dev/null || echo "$(shell go env GOPATH)/bin/nfpm")

.PHONY: all build test clean install lint packages deb rpm apk nfpm-install

all: build

build:
	go build $(LDFLAGS) -o $(BINARY) $(CMD)

build-static:
	CGO_ENABLED=0 GOOS=linux go build $(LDFLAGS) -o $(BINARY) $(CMD)

test:
	go test ./...

test-verbose:
	go test -v ./...

lint:
	go vet ./...

clean:
	rm -f $(BINARY)
	rm -f $(DIST)/*.deb $(DIST)/*.rpm $(DIST)/*.apk

install: build
	install -m 755 $(BINARY) $(INSTALL)/$(BINARY)
	install -m 644 $(DIST)/mountsentinel.service /etc/systemd/system/mountsentinel.service
	@if [ ! -f /etc/mountsentinel.yml ]; then \
		install -m 640 $(DIST)/mountsentinel.yml.example /etc/mountsentinel.yml; \
	fi
	systemctl daemon-reload

# --- Package targets (require nfpm) ---

packages: deb rpm

deb: build-static
	VERSION=$(VERSION) $(NFPM) package --config nfpm.yml --packager deb --target $(DIST)/

rpm: build-static
	VERSION=$(VERSION) $(NFPM) package --config nfpm.yml --packager rpm --target $(DIST)/

apk: build-static
	VERSION=$(VERSION) $(NFPM) package --config nfpm.yml --packager apk --target $(DIST)/

nfpm-install:
	go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

dep:
	go mod tidy
	go mod download
