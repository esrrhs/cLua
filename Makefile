# Reproducible build and test entry for cLua.
# From a clean checkout: `make` then `make test`.
# Override tools with `make CMAKE=cmake GO=go LUA=lua5.4`.
# Keep compilation serial by default to avoid high memory use.

CMAKE ?= cmake
GO ?= go
BUILD_DIR ?= build
CMAKE_ARGS ?=
GOFLAGS ?= -mod=readonly
JOBS ?= 1

.PHONY: all libclua clua clua_helper test tidy clean

all: libclua clua clua_helper

libclua: libclua.so

libclua.so: main.cpp CMakeLists.txt
	mkdir -p $(BUILD_DIR)
	$(CMAKE) -S . -B $(BUILD_DIR) $(CMAKE_ARGS)
	$(CMAKE) --build $(BUILD_DIR) -j$(JOBS)
	cp -f $(BUILD_DIR)/libclua.so $@

clua: clua.go go.mod go.sum
	$(GO) build $(GOFLAGS) -o clua clua.go

clua_helper: clua_helper.go go.mod go.sum
	$(GO) build $(GOFLAGS) -o clua_helper clua_helper.go

# End-to-end: Lua collection -> clua parse -> LCOV export, with content assertions.
test: all
	$(GO) test $(GOFLAGS) -count=1 -p 1 ./e2e

# Refresh go.sum after dependency changes. Do not disable the checksum database.
tidy:
	$(GO) mod tidy

clean:
	rm -rf $(BUILD_DIR) libclua.so clua clua_helper
	rm -f test.cov test.info luacov.data luacov.log
	rm -rf htmltest
