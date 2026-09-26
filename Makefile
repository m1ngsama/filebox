LDFLAGS := -s -w
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"

.PHONY: web size build linux test e2e deploy

web:
	cd web && bun install --frozen-lockfile && bun run check && bun run build

size: web
	@n=$$(cat web/dist/assets/*.js | gzip -9 | wc -c | tr -d ' '); echo "js gzip: $$n bytes (budget 122880)"; test $$n -le 122880

build: web
	$(GOBUILD) -o bin/filebox ./cmd/filebox

linux: web
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o bin/filebox-linux-amd64 ./cmd/filebox

test:
	go test -race ./...

e2e: build
	cd e2e && bun install --frozen-lockfile && bunx playwright test

deploy: linux
	scp bin/filebox-linux-amd64 greenarch:/tmp/filebox
	ssh greenarch 'sudo install -m 755 /tmp/filebox /usr/local/bin/filebox && rm /tmp/filebox && sudo systemctl restart filebox && systemctl is-active filebox'
