BINARY   ?= dsh-cli
GOPROXY  ?= https://goproxy.cn,direct
export GOPROXY

# The linker maps its atomic output through a temp dir (GOTMPDIR, default
# $TMPDIR). On hosts whose /tmp is a small tmpfs a 20MB+ link fails with
# "mapping output file failed: disk quota exceeded", so the project
# carries its own temp dir.
GOTMPDIR ?= $(CURDIR)/.gotmp
export GOTMPDIR

# Sandboxes may mount the user's ~/.cache read-only, where the default
# build cache (GOCACHE) fails every compile with "read-only file system".
# Point it at a project-local cache instead (respects an explicit GOCACHE).
GOCACHE ?= $(CURDIR)/.gocache
export GOCACHE

# go uses GOTMPDIR/GOCACHE but never creates them itself, so a fresh
# checkout dies on the first compile with "no such file or directory".
# A target directory is up-to-date as long as it exists — one mkdir each.
.gotmp:
	mkdir -p $@
.gocache:
	mkdir -p $@

.PHONY: all deps build release release-clean test test-race vet lint check run clean install

all: build

# one-time dependency download (run on a host with network access)
deps: .gotmp .gocache
	go mod download
	go mod tidy

# -buildvcs=false: some sandboxes expose an empty read-only .git that breaks VCS stamping
build: .gotmp .gocache
	go build -buildvcs=false -o $(BINARY) .

# release: one versioned artifact per (os, arch) pair into releases/:
# a .tar.gz (dsh-cli-<ver>-<os>-<arch>.tar.gz, the binary inside is
# dsh-cli); windows keeps the plain exe (dsh-cli-<ver>-<arch>.exe).
# The last artifact is a source tarball (dsh-cli-<ver>-src.tar.gz):
# the working tree minus build state and local dev files (the .gitignore
# conventions), tar members stripped of the leading ./.
# <ver> is read from internal/version/version.go — no Makefile sync.
# CGO stays off: every dependency is pure Go, so no cross toolchain is
# needed. Matrix:
# i386 (Go's 386) on every OS but macOS — the toolchain dropped
# darwin/386, so macOS gets arm64 instead — and dragonfly (no
# dragonfly/386 in the toolchain). Override:
# make release PLATFORMS="linux/arm64 ...".
# Artifact version: extracted from the source (single source of truth).
VERSION   := $(shell sed -n 's/.*const Version = "\([^"]*\)".*/\1/p' internal/version/version.go)
RELEASE_DIR ?= releases
PLATFORMS   ?= linux/amd64 linux/386 linux/arm64 darwin/amd64 darwin/arm64 freebsd/amd64 freebsd/386 freebsd/arm64 openbsd/amd64 openbsd/386 openbsd/arm64 netbsd/amd64 netbsd/386 netbsd/arm64 dragonfly/amd64 windows/amd64 windows/386

release: .gotmp .gocache
	@mkdir -p $(RELEASE_DIR)
	@rm -f $(RELEASE_DIR)/$(BINARY)-*
	@st=$$(mktemp -d); trap 'rm -rf "$$st"' EXIT; \
	for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		name="$(BINARY)-$(VERSION)-$$os-$$arch"; \
		if [ "$$os" = "windows" ]; then \
			out="$(RELEASE_DIR)/$$name.exe"; \
		else \
			out="$(RELEASE_DIR)/$$name.tar.gz"; \
		fi; \
		echo "release: $$os/$$arch -> $$out"; \
		GOOS="$$os" GOARCH="$$arch" CGO_ENABLED=0 \
			go build -buildvcs=false -trimpath -ldflags "-s -w" \
			-o "$$st/$(BINARY)" . || exit 1; \
		if [ "$$os" = "windows" ]; then \
			mv "$$st/$(BINARY)" "$$out" || exit 1; \
		else \
			tar -czf "$$out" -C "$$st" "$(BINARY)" || exit 1; \
		fi; \
	done; \
	echo "release: src -> $(RELEASE_DIR)/$(BINARY)-$(VERSION)-src.tar.gz"; \
	tar --transform 's|^\./||' -czf "$(RELEASE_DIR)/$(BINARY)-$(VERSION)-src.tar.gz" \
		--exclude='.git' --exclude='.gocache' --exclude='.gotmp' \
		--exclude='releases' --exclude='$(RELEASE_DIR)' \
		--exclude='./$(BINARY)' --exclude='./AGENTS.md' --exclude='./smoke.sh' \
		--exclude='*.log' . || exit 1

release-clean:
	rm -rf $(RELEASE_DIR)

test: .gotmp .gocache
	go test ./...

# -race: the state engine's COW contract and the UI's goroutine rules
# (tea.Cmd workers must not touch the model) are only visible here.
test-race: .gotmp .gocache
	go test -race ./...

vet: .gotmp .gocache
	go vet ./...

# lint: golangci-lint under the project's minimal config. The binary is
# optional (check stays vet+test+build) — install with:
# go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
lint: .gotmp .gocache
	golangci-lint run ./...

check: vet test-race build

run: .gotmp .gocache
	go run -buildvcs=false .

# install also drops the locale catalogs in <GOBIN>/locales so the face
# files are found beside the executable (see internal/i18n SearchDirs);
# the built-in English needs no file at all.
install: .gotmp .gocache
	go install -buildvcs=false .
	@dest="$$(go env GOBIN)"; [ -n "$$dest" ] || dest="$$(go env GOPATH)/bin"; \
	if ls locales/*.json >/dev/null 2>&1; then \
		mkdir -p "$$dest/locales" && cp locales/*.json "$$dest/locales/" && \
		echo "installed locale catalogs to $$dest/locales"; fi

clean:
	rm -f $(BINARY)
