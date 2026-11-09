# FMEA — Анализ видов и последствий отказов

Шесть ключевых отказов s3vault: что ломается, как обнаруживается, как реагирует система, каким тестом проверено.

| # | Отказ | Влияние | Обнаружение | Реакция | Проверка |
|---|-------|---------|-------------|---------|----------|
| 1 | **Диск с чанком недоступен** (одна из двух реплик) | Потеря одной реплики, чтение из второй | Ошибка чтения из `dirA`, fallback на `dirB` (ReplicaStore.Get) | `Heal` восстанавливает файл из `dirB` в `dirA` (копия + checksum) | `TestHeal`, `TestReplicaStore` |
| 2 | **Процесс убит (SIGKILL) во время PUT** | Незавершённый PUT, потенциально битый WAL-хвост | WAL replay при старте сервера | WAL перечитывает записи, обрезает битый хвост (CRC), состояние восстанавливается: объект либо **цел**, либо **отсутствует** | `TestReplayAfterSIGKILL`, `TestReadCorruptedRecord` |
| 3 | **Postgres недоступен** (сеть, краш) | Нет записи/чтения метаданных | Healthcheck `/readyz` → 503 (флаг `SetReady(false)`), `pgxpool` ping error | Сервер переходит в **not ready** (`/readyz` 503), in-flight запросы drain, новые PUT/GET → 503 | `TestReadinessCheck`, `TestGracefulShutdown` |
| 4 | **Потеря blob'а при дедупе** (счётчик ссылок рассинхронизирован) | Объект ссылается на несуществующий blob | `GetObject` → ошибка чтения blob'а; метрика `s3_blobs_total` не растёт | GC проверяет `blob_refs`, `Heal` восстанавливает из реплики; при полной потере — объект помечается битым | `TestDedupStore` (blob удаляется только при refcount=0) |
| 5 | **Диск полон** (blob не может записаться) | PUT не может сохранить данные | `io.Copy` возвращает `ENOSPC`, ошибка в `FSStore.Put` | `507 Insufficient Storage` клиенту, метаданные **не** пишутся (атомарность: сначала blob, потом metadata) | `TestDedupStore` (Put → metadata), `TestWriteError` |
| 6 | **Split-brain: два инстанса** | Конфликт метаданных, двойная запись | Lease истёк (`expires_at < NOW()`) | Fencing через `leases` (advisory lock): второй инстанс **не может** взять lease → `503 ErrLeaseTaken`; первый продолжает работать | `TestLeases`, `TestFencing` |

## Пояснения

**1. Диск с чанком недоступен.** `ReplicaStore` пишет в **две директории** (`data`, `data-replica`). При чтении — `Get` идёт в **первую доступную** реплику. Если `dirA` недоступен, читаем из `dirB`. `Heal` восстанавливает `dirA` из `dirB` (копия + проверка checksum). Проверено `TestHeal` и `TestReplicaStore`.

**2. SIGKILL во время PUT.** WAL (write-ahead log) пишет запись **до** применения к blob-хранилищу. После SIGKILL при старте — `Replay` перечитывает WAL, проверяет CRC. Битый хвост **обрезается** (`TestReadCorruptedRecord`), валидные записи **применяются**. Объект либо **цел** (запись дошла до WAL и применилась), либо **отсутствует** (запись не дошла). Проверено `TestReplayAfterSIGKILL`.

**3. Postgres недоступен.** `/readyz` зависит от флага `isReady` (`internal/app/health.go`). При потере Postgres — `SetReady(false)` → `/readyz` 503. `ShutdownServer` делает drain in-flight запросов. Проверено `TestReadinessCheck`, `TestGracefulShutdown`. **Важно:** `s3vault` **не** переходит в read-only автоматически (это **сверх ТЗ**), но `/readyz` 503 **сигнализирует** балансировщику убрать инстанс.

**4. Потеря blob'а при дедупе.** `DedupStore` создаёт `blob_refs` для каждого объекта. Blob удаляется **только** когда `refcount = 0` (`TestDedupStore`). Если blob потерян (диск), `GetObject` вернёт ошибку; `Heal` может восстановить из реплики. При полной потере — объект **битый** (это **осознанный** риск дедупа; митигация — репликация).

**5. Диск полон.** `FSStore.Put` возвращает ошибку `ENOSPC`. Хендлер `handlePut` возвращает `500`/`507` клиенту, **метаданные не пишутся** (сначала blob, потом metadata — атомарность). Проверено `TestWriteError` (ошибка записи в store).

**6. Split-brain.** `leases` — таблица блокировок. `AcquireLease` — `INSERT ... ON CONFLICT DO NOTHING` (атомарно). Второй инстанс получает `ErrLeaseTaken` (`TestLeases`, `TestFencing`). TTL (`expires_at`) — если первый инстанс умер, lease истекает, второй может взять. Это **fencing**, а не «два мастера».

## Что **не** входит в FMEA (осознанно)

- **Redis** — `s3vault` **не использует** Redis в коде (Redis — только для Sentinel-стенда в Части 7). Поэтому отказ Redis **не влияет** на `s3vault`.
- **Grafana/Prometheus** — наблюдаемость, не критичный путь. Их отказ **не** ломает S3 API.
- **OTLP/tracing** — graceful degradation: если collector недоступен, `InitTracer` возвращает `warn`, сервер работает.

## Ссылки

- `internal/blob/replica.go` — `Heal`, `Get` (fallback).
- `internal/wal/replay.go` — `Replay`, обрезка битого хвоста.
- `internal/app/health.go` — `/readyz`, `SetReady`.
- `internal/metadata/repo_leases.go` — `AcquireLease`, fencing.
- `internal/blob/dedup.go` — `blob_refs`, refcount.
- Тесты: `TestHeal`, `TestReplicaStore`, `TestReplayAfterSIGKILL`, `TestReadinessCheck`, `TestGracefulShutdown`, `TestDedupStore`, `TestWriteError`, `TestLeases`, `TestFencing`.