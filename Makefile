MAKE_DIR:=$(strip $(shell dirname "$(realpath $(lastword $(MAKEFILE_LIST)))"))

.PHONY: help bin clean lint test fix gomodules lint-gomodules gofmt lint-gofmt goimports lint-goimports lint-govet

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

# run tests
test:
	go test -v ./...

# run all linters
lint: lint-gomodules lint-gofmt lint-goimports lint-govet

# fix (some) lint violations
fix: gofmt goimports

# update and remove unused go modules (all workspace modules)
gomodules:
	for dir in $$(scripts/go-modules.sh); do \
		(cd "$$dir" && go mod tidy); \
	done
	go work vendor

# check if any go modules need updating (all workspace modules)
lint-gomodules:
	for dir in $$(scripts/go-modules.sh); do \
		(cd "$$dir" && go mod verify); \
	done

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
