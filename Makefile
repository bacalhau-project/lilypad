SHELL = bash

.PHONY: all
all: build

export CGO_ENABLED=0
export GOOS ?= $(shell go env GOOS)
export GOARCH ?= $(shell go env GOARCH)
export GOPATH ?= $(shell go env GOPATH)

HOMEBREW_ROOT := $(shell brew config | grep HOMEBREW_PREFIX | cut -d: -f2)
PROTOC_BREW := ${HOMEBREW_ROOT}/Cellar/protoc-gen-go
${PROTOC_BREW}:
	brew install protoc-gen-go

ABIGEN ?= ${GOPATH}/bin/abigen
${ABIGEN}: ${PROTOC_BREW}
	go install github.com/ethereum/go-ethereum/cmd/abigen@v1.10.26

%/node_modules/.bin/hardhat:
	cd $* && npm install

%/package-lock.json: %/package.json
	cd $* && npm install

.PHONY: prepare
prepare: ${ABIGEN} ${HARDHAT}

include hardhat/contracts/.deps.mk
include examples/contracts/.deps.mk

hardhat/contracts/.deps.mk: $(shell find hardhat/contracts -name '*.sol')
examples/contracts/.deps.mk: $(shell find examples/contracts -name '*.sol')

%/.deps.mk:
	echo '$^' | \
		xargs -n1 basename -s .sol | \
		tr ' ' "\n" | \
		awk -v ROOT='$(shell dirname $$(dirname $@))' -v STEM='$(shell basename $$(dirname $@))' \
		    'BEGIN { print "MKDEPS += $@"; } {\
			print ROOT "/artifacts/" STEM "/" $$1 ".sol/" $$1 ".json: " ROOT "/" STEM "/" $$1 ".sol" \
				" | " ROOT "/node_modules/.bin/hardhat"; \
			print toupper(ROOT) "_ABIJSONS += " ROOT "/artifacts/" STEM "/" $$1 ".sol/" $$1 ".json"; \
			print toupper(ROOT) "_ABIJSONS += " ROOT "/artifacts/" STEM "/" $$1 ".sol/" $$1 ".dbg.json"; \
			print toupper(ROOT) "_PACKAGES += " ROOT "/artifacts/" STEM "/" $$1 ".sol/" $$1 ".go"; \
		}' > $@

${HARDHAT_ABIJSONS}: hardhat/package-lock.json | ${HARDHAT}
	cd hardhat && npx hardhat compile --force

${EXAMPLES_ABIJSONS}: examples/package-lock.json | ${HARDHAT}
	cd examples && npm install @openzeppelin/contracts @openzeppelin/contracts-upgradeable && npx hardhat compile --force

%.go: %.json | ${ABIGEN}
	${ABIGEN} \
		--abi <(cat $< | jq -rc '.abi') \
		--bin <(cat $< | jq -rc '.bytecode') \
		--pkg $(shell dirname $@ | xargs basename -s .sol) \
		> $@

OSES     := darwin linux
ARCHES   := arm64 amd64
BASENAME := $(shell pwd | xargs basename)
BINARY   := bin/${BASENAME}-${GOOS}-${GOARCH}
BINARIES := $(foreach OS,${OSES}, $(foreach ARCH,${ARCHES},bin/${BASENAME}-${OS}-${ARCH}))

bin:
	mkdir -p $@

bin/${BASENAME}-%: ${HARDHAT_PACKAGES} $(shell find pkg -name '*.go') main.go | bin
	GOOS=$(shell echo $@ | cut -f2 -d'-') GOARCH=$(shell echo $@ | cut -f3 -d'-') go build -o $@ .

.PHONY: build
build: ${EXAMPLES_ABIJSONS} ${BINARIES}

ENV_FILE ?= hardhat/.env
ifeq ($(shell cat ${ENV_FILE} | grep WALLET_PRIVATE_KEY),)
$(warning No WALLET_PRIVATE_KEY in ${ENV_FILE})
endif
ifeq ($(shell cat ${ENV_FILE} | grep DEPLOYED_CONTRACT),)
$(warning No DEPLOYED_CONTRACT in ${ENV_FILE})
endif

.PHONY: deploy
deploy: ${BINARIES} | ops/deploy.sh ${ENV_FILE}
	cd ops && ./deploy.sh

.PHONY: run
run: ${BINARY} | ${ENV_FILE}
	env $$(cat ${ENV_FILE}) ${BINARY}

.PHONY: key
key:
	@openssl ecparam -name secp256k1 -genkey -noout | \
		openssl ec -text -noout 2>/dev/null | \
		grep priv -A 3 | tail -n +2 | tr -d ":\n[:space:]" | \
		OFS='' xargs printf WALLET_PRIVATE_KEY=%s

.SECONDARY: ${HARDHAT_PACKAGES}

.PHONY: clean
clean:
	-$(RM) ${MKDEPS}
	-$(RM) ${HARDHAT_PACKAGES} ${EXAMPLES_PACKAGES}
	-$(RM) ${HARDHAT_ABIJSONS} ${EXAMPLES_ABIJSONS}
	-${RM} -r bin/
