## test-integration: Запустить интеграционные тесты
test-integration:
	@go test -tags=integration -count=1 -timeout=10m -v ./...

## test-e2e: Запустить e2e тест
test-e2e:
	@go test -tags=e2e -count=1 -timeout=10m -v ./e2e/...

## docker-run: Запустить приложение и PostgreSQL в Docker
docker-run:
	docker compose -f deployments/docker-compose.yaml up -d --build

## docker-stop: Остановить приложение и PostgreSQL
docker-stop:
	docker compose -f deployments/docker-compose.yaml down

## run-linters: Запустить линтеры
run-linters:
	@golangci-lint run -c ./golangci.yml

## help: Показать справку
help:
	@echo "Доступные команды:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'