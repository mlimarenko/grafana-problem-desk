SHELL := /bin/bash
.DELETE_ON_ERROR:
GO ?= go
GOOS ?= linux
GOARCH ?= amd64
FRONTEND := $(wildcard src/*.ts src/*.tsx src/pages/*.tsx docs/*/*.md docs/assets/*) src/plugin.json webpack.config.cjs tsconfig.json
BACKEND := $(wildcard pkg/*.go pkg/*/*.go) go.mod go.sum

.PHONY: build test package dev stop clean

node_modules/.problem-desk-deps: package.json package-lock.json
	npm ci --no-audit --no-fund
	touch $@

dist/module.js: $(FRONTEND) node_modules/.problem-desk-deps docs/assets/logo.svg README.md LICENSE CHANGELOG.md
	npm run typecheck
	npm run build
	chmod -R a+rX dist

dist/gpx_problemdesk_$(GOOS)_$(GOARCH): $(BACKEND)
	mkdir -p dist
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -o $@ ./pkg
	chmod 755 dist $@

build: dist/module.js dist/gpx_problemdesk_$(GOOS)_$(GOARCH)

test: node_modules/.problem-desk-deps
	npm run typecheck
	npm test
	$(GO) test ./pkg/...

package: build
	python3 scripts/package-plugin.py

dev: build
	docker compose up -d --wait

stop:
	docker compose down

clean:
	git clean -fdX -- dist artifacts
