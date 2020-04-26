MAKE_DIR:=$(strip $(shell dirname "$(realpath $(lastword $(MAKEFILE_LIST)))"))

.PHONY: help bin clean lint fix gomodules lint-gomodules gofmt lint-gofmt goimports lint-goimports lint-govet

default: help

# list all make targets
help:
	@grep -E '^[^_.#[:space:]].*:' "$(MAKE_DIR)/Makefile" | grep -v ':=' | cut -d':' -f1 | sort

# compile all command binaries
bin:
	scripts/build.sh

# remove compiled binaries
clean:
	scripts/clean.sh

# run all linters
lint: lint-gomodules lint-gofmt lint-goimports lint-govet

# fix (some) lint violations
fix: gofmt goimports

# update and remove unused go modules
gomodules:
	go mod tidy

# check if any go modules need updating
lint-gomodules:
	go mod verify

# format go code
gofmt:
	scripts/go-find.sh | xargs gofmt -s -w

# lint go code formatting
lint-gofmt:
	scripts/lint-gofmt.sh

# update go imports
goimports:
	scripts/go-find.sh | xargs goimports -w

# lint go imports
lint-goimports:
	scripts/lint-goimports.sh

# vet go code
lint-govet:
	scripts/lint-govet.sh
