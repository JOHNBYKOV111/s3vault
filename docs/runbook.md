# Runbook — s3vault

## Общие сведения

- **Сервис:** S3-совместимый gateway (`s3vault`).
- **Порт:** `9000` (HTTP).
- **Зависимости:**
  - **PostgreSQL** — метаданные (tenants, buckets, objects, jobs, leases, audit_log).
  - **Prometheus + Grafana** — наблюдаемость (RED/USE/SLO, алерты).
  - **Redis** — **не** используется `s3vault` в коде. Redis поднимается **только** для Sentinel-стенда (chaos-тест, Часть 7).
- **Health:** `GET /healthz` (liveness), `GET /readyz` (readiness).
- **Метрики:** `GET /metrics` (Prometheus).

---

## Sentinel failover (chaos-тест)

### Назначение

Проверяет, что Redis Sentinel повышает replica до master при падении master'а, а клиент (`go-redis` `FailoverClient`) автоматически переподключается к новому master'у.

**Важно:** `s3vault` **не использует** Redis. «Gateway» в этом тесте — **тестовый клиент** (`cmd/redis-sentinel-check`), который ведёт себя как gateway (ходит через Sentinel, переподключается). Это **инфраструктурный** тест Sentinel, а не тест `s3vault`.

### Запуск

```bash
./scripts/chaos/sentinel-failover.sh