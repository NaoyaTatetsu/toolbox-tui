BINARY := tt
# Install into the directory that actually wins on $PATH. `go install` puts the
# binary in GOBIN, which on this machine sits behind ~/.local/bin — a stale copy
# there silently shadows every new build.
INSTALL_DIR ?= $(HOME)/.local/bin
STAMP := $(shell date '+%Y-%m-%d %H:%M:%S')
LDFLAGS := -X 'main.buildStamp=$(STAMP)'

.PHONY: build test vet install install-gobin run doctor frames clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/tt

test:
	go test ./...

vet:
	go vet ./...

install:
	@mkdir -p $(INSTALL_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(INSTALL_DIR)/$(BINARY) ./cmd/tt
	@echo "installed $(INSTALL_DIR)/$(BINARY)"
	@echo "which tt → $$(command -v $(BINARY) || echo 'not on PATH')"

# Install to GOBIN instead, for setups where that is the directory on $PATH.
install-gobin:
	go install -ldflags "$(LDFLAGS)" ./cmd/tt

run: build
	./$(BINARY)

doctor: build
	./$(BINARY) doctor

# Print one rendered frame of each view against the live project — handy when
# changing layout code without wanting to drive the TUI by hand.
frames:
	LIVE=1 go test ./internal/ui -run TestLiveRender -v

clean:
	rm -f $(BINARY)
