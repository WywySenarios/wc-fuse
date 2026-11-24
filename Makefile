ANSIBLE := uv run --frozen ansible-playbook
ARTIFACT := ansible/roles/wc_fuse/files/wc-fuse-daemon

.PHONY: daemon check provision uninstall test

daemon:
	go build -o $(ARTIFACT) ./cmd/wc-fuse-daemon

check: daemon
	$(ANSIBLE) ansible/site.yml --check --diff $(ARGS)

provision: daemon
	$(ANSIBLE) ansible/site.yml $(ARGS)

uninstall:
	$(ANSIBLE) ansible/uninstall.yml

test:
	pre-commit run --all-files
