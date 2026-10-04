## test-integration: Запустить интеграционные тесты
test-integration:
	@go test -tags=integration -count=1 -timeout=10m -v ./...

## run-linters: Запустить линтеры
run-linters:
	@golangci-lint run -c ./golangci.yml

## help: Показать справку
help:
	@echo "Доступные команды:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'