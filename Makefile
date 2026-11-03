BINARY := runall
BINDIR := $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)

.PHONY: all install uninstall vet clean

all: install

install:
	go install .
	@echo "Installed $(BINDIR)/$(BINARY)"

uninstall:
	rm -f $(BINDIR)/$(BINARY)

vet:
	go vet ./...

clean:
	rm -f $(BINARY)