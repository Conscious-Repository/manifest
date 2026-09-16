# Deploy targets (big-change Phase 3a) — deployment is a repo artifact, and
# the operator owns it: these run from the laptop, over the tailnet.
METIS   = benjamin@metis.tail8f89de.ts.net
MANIFEST_DIR = /home/benjamin/src/manifest
HARNESS_DIR  = /private/harnesses

.PHONY: deploy engine-deploy units-deploy test

test:
	go test ./...

# manifest → metis: push, pull+build on the box, restart the service.
deploy:
	git push origin main
	ssh $(METIS) 'set -e; cd $(MANIFEST_DIR) && git pull --ff-only && go build -o manifest . \
	  && go build -o ~/.local/bin/manifest-sync ./cmd/manifest-sync \
	  && sudo systemctl restart manifest && systemctl is-active manifest'

# Excalibur is deprecated. Engine availability restoration is explicit and
# must preserve blocked extractor fences; see docs/excalibur-retirement/decommission.md.
engine-deploy:
	@echo 'Excalibur engine deployment retired; use the reviewed restore procedure.' >&2
	@exit 1

# unit files → metis (after editing deploy/*.service|*.target|*.path)
units-deploy:
	scp deploy/manifest.service deploy/manifest-sync.service \
	    deploy/engine-room.target deploy/private-ready.path deploy/hermes-gateway.service $(METIS):/tmp/
	ssh $(METIS) 'sudo mv /tmp/manifest.service /tmp/manifest-sync.service \
	    /tmp/engine-room.target /tmp/private-ready.path /tmp/hermes-gateway.service /etc/systemd/system/ && sudo mkdir -p /etc/systemd/system/engine-room.target.wants && sudo ln -sfn /etc/systemd/system/hermes-gateway.service /etc/systemd/system/engine-room.target.wants/hermes-gateway.service && sudo systemctl daemon-reload'
