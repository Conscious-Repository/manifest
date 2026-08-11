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

# engine → metis: the harnesses repo syncs via manifest-sync; this waits for
# the current engine source to land, builds, restarts.
engine-deploy:
	ssh $(METIS) 'set -e; cd $(HARNESS_DIR) && git pull --ff-only; \
	  cd $(HARNESS_DIR)/excalibur/engine && go build -o ~/.local/bin/excalibur-engine ./cmd/excalibur \
	  && sudo systemctl restart excalibur-engine && systemctl is-active excalibur-engine'

# unit files → metis (after editing deploy/*.service|*.target|*.path)
units-deploy:
	scp deploy/manifest.service deploy/manifest-sync.service deploy/excalibur-engine.service \
	    deploy/engine-room.target deploy/private-ready.path $(METIS):/tmp/
	ssh $(METIS) 'sudo mv /tmp/manifest.service /tmp/manifest-sync.service /tmp/excalibur-engine.service \
	    /tmp/engine-room.target /tmp/private-ready.path /etc/systemd/system/ && sudo systemctl daemon-reload'
