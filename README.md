S3Vault

S3Vault — полу-распределённое, S3-совместимое объектное хранилище с дедупликацией, репликацией и отказоустойчивостью. Написан на Go, использует Postgres (single-writer + replica), Redis Sentinel и локальный blob-слой с репликами и self-heal.

Ключевые возможности
S3-совместимость — проверено с официальным aws-cli (11/11) и boto3 (9/9): PutObject, GetObject, Range (206), Multipart, Delete, List.
Дедупликация (content-addressed) — одинаковые объекты хранятся один раз, счётчик ссылок и GC.
Репликация blob'ов — каждая копия хранится на 2 нодах (shard = hash(blob_id) % N), self-heal при потере.
Отказоустойчивость — Postgres single-writer + advisory lock/lease (fencing), Redis Sentinel (failover), WAL с replay.
Наблюдаемость — Prometheus (RED/USE + бизнес-метрики), Grafana (SLO + error budget), OpenTelemetry (Jaeger/Tempo), structured JSON-логи с trace_id.
Безопасность — SigV4, RBAC (allow/deny), изоляция tenant'ов, envelope-шифрование (AES-256-GCM, DEK/KEK).
🏗 Архитектура
text
┌─────────────────────────────────────────────────────────────┐
│                    Load Balancer (nginx/haproxy)            │
│                    TLS termination, rate limit              │
└─────────────────────────────┬───────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
┌───────▼───────┐     ┌───────▼───────┐     ┌───────▼───────┐
│ S3 Gateway 1  │     │ S3 Gateway 2  │     │ S3 Gateway N  │
│ (stateless)   │     │ (stateless)   │     │ (stateless)   │
└───────┬───────┘     └───────┬───────┘     └───────┬───────┘
        └─────────────────────┼─────────────────────┘
                              │
┌─────────────────────────────┼─────────────────────────────┐
│                    Postgres + Redis Sentinel              │
│  (metadata: single-writer, jobs: SKIP LOCKED, cache)      │
└─────────────────────────────┬─────────────────────────────┘
                              │
┌─────────────────────────────▼─────────────────────────────┐
│              Blob Layer (FS + 2 replicas, self-heal)      │
└───────────────────────────────────────────────────────────┘
Быстрый старт
1. Поднять инфраструктуру
bash
git clone https://github.com/JOHNBYKOV111/s3vault.git
cd s3vault
docker compose -f deploy/docker-compose.yml up -d postgres
2. Запустить сервер
bash
DATABASE_URL="postgres://s3vault:s3vault@localhost:5433/s3vault?sslmode=disable" \
  go run ./cmd/server
Сервер поднимется на http://localhost:9000.

3. Проверить
bash
# healthcheck
curl -i http://localhost:9000/healthz   # → 200 {"status":"ok"}
curl -i http://localhost:9000/readyz    # → 200

# S3 через aws-cli
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test

aws --endpoint-url http://localhost:9000 s3 mb s3://demo
echo "Hello S3Vault" > /tmp/hello.txt
aws --endpoint-url http://localhost:9000 s3 cp /tmp/hello.txt s3://demo/hello.txt
aws --endpoint-url http://localhost:9000 s3 ls s3://demo/

Бизнес-ценность
Use Cases (подробнее — )
Бэкапы баз данных — версионирование, multipart для больших дампов.
Хранилище медиа (картинки/видео) — дедупликация экономит до 90% места.
Архив логов и аналитики — Range-запросы, долгосрочное хранение.
Cost Model (подробнее — )
Дедупликация по SHA-256 даёт экономию до 99% при хранении копий:

100 копий файла 1 ГБ = 1 ГБ на диске, а не 100 ГБ. Счётчик ссылок и GC удаляют blob только когда удалён последний объект.

Проверка качества
bash
# Unit + race
go test ./... -race -count=1

# Conformance (нужен запущенный сервер)
./scripts/conformance/aws-cli.sh                  # → 11/11 PASSED
python3 scripts/conformance/boto3_conformance.py  # → 9/9 PASSED

# Chaos (Sentinel failover)
./scripts/chaos/sentinel-failover.sh              # → PASSED

# Интеграционные тесты (testcontainers)
go test -tags=integration ./test/integration/...
Демонстрация работы
Все проверки пройдены вживую (без моков, на реальной инфраструктуре).

S3 Conformance & Health
Healthcheck (200 OK)
AWS CLI (Upload, List, Delete)
Range-запрос (206 Partial Content)
Тестирование и отказоустойчивость
Conformance Tests (11/11 & 9/9)
Integration Tests (with -race)
Sentinel Chaos Failover
Наблюдаемость и CI
Grafana Dashboard (RED/USE + SLO)
CI/CD Pipeline (GitHub Actions)

Документация
Документ	О чём
3 бизнес-сценария
Математика экономии от дедупа
Изоляция tenant'ов
Анализ рисков (FMEA)
Восстановление ≤ 1 ч (SRE)

Лицензия
Этот проект распространяется под лицензией MIT.