BINARY     := terraform-provider-bella
VERSION    := $(shell cat VERSION 2>/dev/null || echo "0.1.0")
OS         := $(shell go env GOOS)
ARCH       := $(shell go env GOARCH)
PLUGIN_DIR := $(HOME)/.terraform.d/plugins/registry.terraform.io/cosmic-chimps/bella-baxter/$(VERSION)/$(OS)_$(ARCH)

# Local .terraformrc — scoped to this repo, does NOT touch ~/.terraformrc
LOCAL_TFRC := $(CURDIR)/.terraformrc

# Export TF_CLI_CONFIG_FILE for all terraform commands invoked from make targets.
# This ensures dev_overrides are active without requiring a manual `export`.
export TF_CLI_CONFIG_FILE := $(LOCAL_TFRC)

.PHONY: build install dev lock-examples lock-demo-look-ma-no-secrets restore clean

## build: compile the provider binary in the current directory
build:
	go build -o $(BINARY) .

## install: build and copy the binary into Terraform's local plugin cache
##          (used by `terraform init` without dev_overrides)
install: build
	@mkdir -p $(PLUGIN_DIR)
	cp $(BINARY) $(PLUGIN_DIR)/$(BINARY)
	@echo "Installed to $(PLUGIN_DIR)/$(BINARY)"

## dev: build + write a LOCAL .terraformrc with dev_overrides
##      This does NOT modify ~/.terraformrc — it is scoped to this directory.
##      Use it by setting TF_CLI_CONFIG_FILE before any terraform command:
##
##        export TF_CLI_CONFIG_FILE=$(pwd)/.terraformrc
##        cd examples && terraform plan
##
##      Or add it to your shell session once:
##        echo 'export TF_CLI_CONFIG_FILE=/path/to/terraform-provider/.terraformrc' >> ~/.zshrc
##
##      dev_overrides needs a DIRECTORY path (not the binary file).
##      After running this, skip 'terraform init' for the bella provider.
##      (First-time: also run 'make lock-examples' for the hashicorp lock files.)
dev: build
	@DIR="$(CURDIR)"; \
	printf 'provider_installation {\n  dev_overrides {\n    "cosmic-chimps/bella-baxter" = "%s"\n  }\n  direct {}\n}\n' "$$DIR" > "$(LOCAL_TFRC)"; \
	echo "Written: $(LOCAL_TFRC)"; \
	echo "  \"cosmic-chimps/bella\" = \"$$DIR\""
	@echo ""
	@echo "Activate for your shell session:"
	@echo "  export TF_CLI_CONFIG_FILE=$(LOCAL_TFRC)"
	@echo ""
	@echo "Then:"
	@echo "  make lock-examples   # once, if lock files are missing"
	@echo "  cd examples && terraform plan"

## lock-examples: generate .terraform.lock.hcl for hashicorp providers in all examples
##                (bella is excluded — handled by dev_overrides)
##                Does NOT require .terraform/ to exist — safe to run from scratch.
lock-examples:
	@OS=$$(go env GOOS); ARCH=$$(go env GOARCH); \
	echo "→ examples (random only)"; \
	( cd examples && \
	  terraform providers lock \
	    -platform=$${OS}_$${ARCH} \
	    registry.terraform.io/hashicorp/random \
	    2>&1 | grep -E 'Success|Error|Obtained|Saved|Lock' || true ); \
	echo "→ examples/ec2-with-bella-ssh (aws, tls, local, null)"; \
	( cd examples/ec2-with-bella-ssh && \
	  terraform providers lock \
	    -platform=$${OS}_$${ARCH} \
	    registry.terraform.io/hashicorp/aws \
	    registry.terraform.io/hashicorp/tls \
	    registry.terraform.io/hashicorp/local \
	    registry.terraform.io/hashicorp/null \
	    2>&1 | grep -E 'Success|Error|Obtained|Saved|Lock' || true )
	@$(MAKE) lock-demo-look-ma-no-secrets

## lock-demo-look-ma-no-secrets: generate .terraform.lock.hcl for the look-ma-no-secrets demo
##                                (bella excluded — handled by dev_overrides)
lock-demo-look-ma-no-secrets:
	@OS=$$(go env GOOS); ARCH=$$(go env GOARCH); \
	DEMO_DIR="$(CURDIR)/../../../demos/look-ma-no-secrets/terraform"; \
	echo "→ demos/look-ma-no-secrets/terraform (aws, random, tls, local, null)"; \
	( cd "$$DEMO_DIR" && \
	  terraform providers lock \
	    -platform=$${OS}_$${ARCH} \
	    registry.terraform.io/hashicorp/aws \
	    registry.terraform.io/hashicorp/random \
	    registry.terraform.io/hashicorp/tls \
	    registry.terraform.io/hashicorp/local \
	    registry.terraform.io/hashicorp/null \
	    2>&1 | grep -E 'Success|Error|Obtained|Saved|Lock' || true )

## restore: re-install hashicorp provider binaries after deleting .terraform/
##          Run this instead of 'terraform init' (which fails on bella).
##          terraform init installs providers even on failure; we then fix the lock file.
restore: dev
	@echo "Re-installing hashicorp providers in example directories..."
	@for dir in examples examples/ec2-with-bella-ssh; do \
	  echo "→ $$dir (init — bella error expected and ignored)"; \
	  ( cd "$$dir" && terraform init -no-color 2>&1 | grep -v "cosmic-chimps/bella-baxter" | grep -E 'Installed|Error|Warning' || true ); \
	done
	@$(MAKE) lock-examples
	@echo ""
	@echo "Done. Try: cd examples && terraform plan"

## clean: remove the built binary and local .terraformrc
clean:
	rm -f $(BINARY) $(LOCAL_TFRC)
