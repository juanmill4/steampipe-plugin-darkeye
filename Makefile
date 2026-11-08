GO ?= go
STEAMPIPE_INSTALL_DIR ?= $(HOME)/.steampipe

.PHONY: build install test

build:
	$(GO) build -trimpath -o build/darkeye.plugin .

install: build
	install -d $(STEAMPIPE_INSTALL_DIR)/plugins/local/darkeye
	install -m 755 build/darkeye.plugin $(STEAMPIPE_INSTALL_DIR)/plugins/local/darkeye/darkeye.plugin

test:
	$(GO) test ./...
	$(GO) vet ./...
