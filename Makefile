LDFLAGS := -s -w
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"

.PHONY: web size build linux test e2e deploy

web:
	! git grep --untracked -nP '[\x{3000}-\x{303f}\x{4e00}-\x{9fff}\x{ff00}-\x{ffef}\x{2018}\x{2019}\x{201c}\x{201d}]' -- web/src ':!web/src/lib/i18n'
	awk 'FNR==1{d=h=0} /@media \(hover: hover\) and \(pointer: fine\)/{h=d+1;o=1} /:hover/&&!o&&!(h&&d>=h){print FILENAME":"FNR": bare :hover";e=1} {o=0;d+=gsub(/\{/,"{")-gsub(/\}/,"}");if(h&&d<h)h=0} END{exit e}' $$(git ls-files -co --exclude-standard 'web/src/*.css' 'web/src/*.svelte' ':!web/src/components/Lightbox.svelte')
	cd web && bun install --frozen-lockfile && bun run check && bun run build

size: web
	@e=$$(grep -o 'assets/[^"]*\.js' web/dist/index.html); n=$$(gzip -9c web/dist/$$e | wc -c | tr -d ' '); \
	s=$$(grep -o 'assets/[^"]*\.js' web/dist/share.html); m=$$(gzip -9c web/dist/$$s | wc -c | tr -d ' '); \
	a=$$(for f in web/dist/assets/*.js; do gzip -9c $$f | wc -c; done | awk '{s+=$$1} END {print s}'); \
	echo "entry js gzip: $$n bytes (budget 122880), share page js gzip: $$m bytes, all js gzip: $$a bytes"; test $$n -le 122880

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
