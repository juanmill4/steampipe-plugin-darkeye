STEAMPIPE_INSTALL_DIR ?= ~/.steampipe
BUILD_TAGS = netgo

install:
	go build -o $(STEAMPIPE_INSTALL_DIR)/plugins/hub.steampipe.io/plugins/juanmill4/darkeye@latest/steampipe-plugin-darkeye.plugin -tags "${BUILD_TAGS}" *.go

test:
	go test ./...
	go vet ./...

.PHONY: install test
