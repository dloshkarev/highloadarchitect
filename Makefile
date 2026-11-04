## test-integration: Запустить интеграционные тесты
test-integration:
	@go test -tags=integration -count=1 -timeout=10m -v ./...

## test-e2e: Запустить e2e тест
test-e2e:
	@go test -tags=e2e -count=1 -timeout=30m -v ./e2e/...

## docker-run: Запустить приложение и PostgreSQL в Docker
docker-run:
	docker compose -f deployments/docker-compose.yaml up -d --build

## docker-stop: Остановить приложение и PostgreSQL
docker-stop:
	docker compose -f deployments/docker-compose.yaml down

## docker-reset: Остановить контейнеры и удалить том Postgres, чтобы init-скрипт выполнился заново
docker-reset:
	docker compose -f deployments/docker-compose.yaml down -v

## metrics-reset: Очистить накопленные метрики (пересоздать Prometheus с пустым хранилищем)
metrics-reset:
	docker compose -f deployments/docker-compose.yaml up -d --force-recreate --renew-anon-volumes prometheus

## update-dashboard: Подставить актуальные ID pg-контейнеров в дашборд Grafana
update-dashboard:
	@python3 build/dev/update_pg_dashboard.py

## docker-fresh: Удалить контейнеры, тома и образ приложения, пересобрать и поднять всё с нуля
docker-fresh:
	docker compose -f deployments/docker-compose.yaml down -v --rmi local
	docker compose -f deployments/docker-compose.yaml up -d --build
	$(MAKE) update-dashboard

## run-linters: Запустить линтеры
run-linters:
	@golangci-lint run -c ./golangci.yml

## help: Показать справку
help:
	@echo "Доступные команды:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'