.PHONY: test test-go test-web e2e e2e-edge build demo
test: test-go test-web
test-go:
	cd gateway && go vet ./... && go test -race -count=1 ./...
test-web:
	cd frontend && npm run typecheck && npm test
e2e:
	cd frontend && npm run e2e
e2e-edge:
	./deploy/test-edge.sh
build:
	cd gateway && go build -o bin/ ./cmd/... && cd ../frontend && npm run build
demo:
	docker compose up --build
