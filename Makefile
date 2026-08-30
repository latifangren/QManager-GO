.PHONY: all build build-frontend build-backend package clean test

FRONTEND_DIR := frontend
BACKEND_DIR := backend
DIST_DIR := $(BACKEND_DIR)/cmd/qmanager/dist
OUTPUT_BIN := qmanager-armv7
PKG_DIR := qmanager-armv7-pkg
TARBALL := qmanager-armv7.tar.gz

all: build

build-frontend:
	@echo "==> Building Next.js Frontend (Static Export)..."
	cd $(FRONTEND_DIR) && bun run build
	@echo "==> Syncing static build to backend embed dist..."
	rm -rf $(DIST_DIR)
	mkdir -p $(DIST_DIR)
	cp -r $(FRONTEND_DIR)/out/* $(DIST_DIR)/

build-backend:
	@echo "==> Compiling Go ARMv7 Binary (RG501Q-EU / RM520N-GL)..."
	cd $(BACKEND_DIR) && GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags="-s -w" -o ../$(OUTPUT_BIN) ./cmd/qmanager

build: build-frontend build-backend
	@echo "==> Single Binary Build Complete: $(OUTPUT_BIN)"

package: build
	@echo "==> Creating release deployment package $(TARBALL)..."
	rm -rf $(PKG_DIR) $(TARBALL)
	mkdir -p $(PKG_DIR)
	cp $(OUTPUT_BIN) $(PKG_DIR)/qmanager
	cp deploy/systemd/qmanager.service $(PKG_DIR)/
	cp deploy/install.sh $(PKG_DIR)/
	chmod +x $(PKG_DIR)/install.sh $(PKG_DIR)/qmanager
	tar -czf $(TARBALL) -C $(PKG_DIR) .
	rm -rf $(PKG_DIR)
	@echo "==> Package created: $(TARBALL)"

test:
	@echo "==> Running backend unit & regression test suite..."
	cd $(BACKEND_DIR) && go test -v -count=1 ./...

clean:
	rm -rf $(FRONTEND_DIR)/out $(FRONTEND_DIR)/.next $(DIST_DIR) $(OUTPUT_BIN) $(BACKEND_DIR)/$(OUTPUT_BIN) $(TARBALL) $(PKG_DIR)
