# Release helpers. Usage:
#
#   make release VERSION=v0.8.1   # bump internal requires, commit, ff main, tag every module
#   make push    VERSION=v0.8.1   # push main, dev and the tags of that version
#   make untag   VERSION=v0.8.1   # delete the local tags of that version (before push only)
#   make size-check                 # stripped size of some examples; fails on forbidden deps

MODULE  := github.com/joaoprofile/gofi-sdk-go
MODULES := gofi base base/bucket/oci base/bucket/s3 base/cloud/aws base/cloud/oci \
           base/secrets/awssm base/secrets/ocivault iam msq msq/provider/kafka \
           msq/provider/nats msq/provider/oci msq/provider/rabbitmq msq/provider/redis \
           msq/provider/sqs netx netx/awssign obs sqln sqln/rdsauth

# Every module lives in a subdirectory, so every tag is <dir>/<version>; the
# repository root has no module and gets no plain v* tag.
TAGS := $(foreach m,$(MODULES),$(m)/$(VERSION))

.PHONY: release push untag check-version size-check

check-version:
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$' || { echo "use: make <target> VERSION=vX.Y.Z"; exit 1; }

release: check-version
	@test -z "$$(git status --porcelain)" || { echo "working tree is not clean: commit or stash first"; exit 1; }
	@test -z "$$(git tag -l gofi/$(VERSION))" || { echo "$(VERSION) already exists, latest is $$(git tag -l 'gofi/v*' --sort=-v:refname | head -1)"; exit 1; }
	git checkout -q dev
	sed -i -E 's#($(MODULE)[^ ]*) v[0-9]+\.[0-9]+\.[0-9]+#\1 $(VERSION)#' $$(git ls-files '*go.mod')
	@for m in $(MODULES); do (cd $$m && go build ./...) || exit 1; done
	@if [ -n "$$(git status --porcelain)" ]; then git commit -qam "chore: release $(VERSION)" && echo "committed chore: release $(VERSION)"; fi
	git checkout -q main
	git merge -q --ff-only dev
	git checkout -q dev
	@for t in $(TAGS); do git tag -a $$t -m $$t && echo "tag $$t"; done
	@echo "done: $(words $(TAGS)) tags. Next: make push VERSION=$(VERSION)"

push: check-version
	git push origin main dev
	git push origin $(TAGS)

untag: check-version
	git tag -d $(TAGS)

# Examples measured by size-check, as <dir>:<import prefixes it must not link>.
GRPC_OTEL := google.golang.org/grpc,go.opentelemetry.io/otel/sdk
SIZE_CHECK := \
	examples/sqln/filter-api:$(GRPC_OTEL) \
	examples/sqln/search:$(GRPC_OTEL),github.com/go-chi/chi \
	examples/msq/kafka/producer:$(GRPC_OTEL),github.com/jackc/pgx,github.com/golang-migrate/migrate,github.com/redis/go-redis,github.com/go-chi/chi

size-check:
	@out=$$(mktemp -d); status=0; \
	for entry in $(SIZE_CHECK); do \
		dir=$${entry%%:*}; bad=; \
		(cd $$dir && go build -trimpath -ldflags='-s -w' -o $$out/bin .) || exit 1; \
		deps=$$(cd $$dir && go list -deps .); \
		for prefix in $$(echo $${entry#*:} | tr , ' '); do \
			echo "$$deps" | grep -q "^$$prefix" && bad="$$bad $$prefix"; \
		done; \
		size=$$(du -h $$out/bin | cut -f1); \
		if [ -n "$$bad" ]; then printf '%-30s %6s  FAIL: links%s\n' $$dir $$size "$$bad"; status=1; \
		else printf '%-30s %6s  ok\n' $$dir $$size; fi; \
	done; \
	rm -rf $$out; exit $$status
