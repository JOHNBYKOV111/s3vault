# Runbook — s3vault

## Общие сведения

- Сервис: S3-совместимый gateway (s3vault).
- Порт: `9000` (HTTP).
- Зависимости: PostgreSQL (метаданные), Redis (опционально, Sentinel-стенд для chaos-теста), Prometheus + Grafana (наблюдаемость).
- Health: `GET /healthz` (liveness), `GET /readyz` (readiness).
- Метрики: `GET /metrics` (Prometheus).

---

## Sentinel failover (chaos-тест)

### Назначение

Проверяет, что Redis Sentinel повышает replica до master при падении master'а, а клиент (`go-redis` `FailoverClient`) автоматически переподключается к новому master'у.

### Запуск

```bash
./scripts/chaos/sentinel-failover.sh