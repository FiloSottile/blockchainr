GO ?= go
GOPATH := $(CURDIR)

blockchainr: dabloom
	$(GO) install blockchainr

analyzr:
	$(GO) install analyzr

all: blockchainr analyzr btcd addblock

dabloom:
	@# @$(MAKE) -C src/github.com/bitly/dablooms DESTDIR=.. prefix=/dablooms install
	@# @CGO_LDFLAGS="-L$(CURDIR)/src/github.com/bitly/dablooms/lib/"
	@# CGO_CFLAGS="-I$(CURDIR)/src/github.com/bitly/dablooms/include/"
	$(MAKE) -C src/github.com/bitly/dablooms install
	$(GO) install github.com/bitly/dablooms/godablooms

btcd:
	$(GO) install github.com/conformal/btcd

addblock:
	$(GO) install github.com/conformal/btcd/util/addblock
