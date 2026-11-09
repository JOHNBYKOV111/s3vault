S3Vault — semi-distributed (полураспределённое) S3-совместимое объектное хранилище на Go, спроектированное для высокой нагрузки, экономии дискового пространства и отказоустойчивости.

Ключевые возможности:
S3-совместимость: проверено официальными клиентами aws-cli (11/11) и boto3 (9/9). Поддержка PutObject, GetObject, Range (206), Multipart, Delete, List.
Semi-distributed архитектура: stateless S3 Gateways за Load Balancer'ом, Postgres single-writer (fencing через advisory locks), Redis Sentinel (автоматический failover), Blob Layer с шардированием (hash(blob_id) % N) и репликацией.
Дедупликация (content-addressed): одинаковые объекты хранятся в единственном экземпляре (SHA-256), счётчик ссылок и GC. Экономия диска до 99%.
Репликация и Self-heal: каждый blob хранится на 2 нодах, при потере копии данные автоматически восстанавливаются.
Отказоустойчивость: WAL с replay после SIGKILL, Redis Sentinel failover, graceful shutdown.
Безопасность: SigV4, RBAC (allow/deny), строгая изоляция tenant'ов, envelope-шифрование (AES-256-GCM, DEK/KEK).
Наблюдаемость: Prometheus (RED/USE + бизнес-метрики), Grafana (SLO + Error Budget), OpenTelemetry (Tracing), JSON-логи с trace_id.

Тестирование и качество:
No Mocks Policy: 100% тестов на реальной инфраструктуре (Testcontainers Postgres, реальный диск t.TempDir, реальные HTTP-запросы). Полное отсутствие заглушек для бизнес-логики.

Типы тестов в проекте:
Интеграционные тесты с PostgreSQL (Testcontainers): проверка SQL-транзакций, изоляции tenant'ов (A не видит B), SKIP LOCKED (два воркера не берут одну задачу), Fencing (advisory locks, защита от split-brain), append-only аудита.
Интеграционные тесты Blob-слоя (реальный диск): дедупликация (один blob на два объекта, счётчик ссылок, GC), репликация (2 копии), self-heal (автоматическое восстановление при потере копии).
Тесты HTTP-слоя (S3 API): CreateBucket, ListObjectsV2, Multipart Upload (state machine), Range (206/416), SigV4 (403 при неверной подписи), защита от path traversal (../etc/passwd), парсинг путей.
Тесты WAL (Write-Ahead Log): group commit, битая CRC (ошибка вместо молчаливого повреждения), replay после SIGKILL (убийство процесса → перезапуск → восстановление состояния).
Тесты безопасности и криптографии: RBAC (allow/deny), изоляция тенантов A/B, хэширование ключей, Envelope-шифрование (AES-256-GCM, DEK/KEK).
Тесты наблюдаемости: инкремент метрик Prometheus, создание трейсов (OpenTelemetry), structured JSON-логи с trace_id.
Тесты устойчивости (Chaos Engineering): Sentinel Failover (мастер убит → реплика повышена → клиент переподключен), WAL Replay (kill9-during-put), Self-heal (remove-disk-dir).
Fuzz-тестирование: FuzzParseS3Path подает случайные байты в парсер путей, доказывая отсутствие паник и уязвимостей.
Тесты конфигурации и устойчивости: валидация env (отсутствие переменных → ошибка), retry с backoff, отмена по контексту.
Race Detector: весь код проверен флагом -race. Отсутствие гонок данных.
Graceful Shutdown: проверка, что при SIGTERM сервер дожидается завершения запросов и закрывает WAL.
CI/CD: GitHub Actions (build, test, chaos, E2E).

Демонстрация: Все проверки пройдены вживую. Скриншоты ключевых результатов в папке docs/demo/ и встроены в README.

Статус: проект доведён до стадии production-ready: semi-distributed архитектура с честной реализацией, полный S3 Conformance (11/11), пройденные chaos-тесты отказоустойчивости (Sentinel Failover), настроенный CI/CD с автоматическим прогоном тестов и полная эксплуатационная документация (Runbook для восстановления ≤ 1 ч, FMEA, ADR). Система готова к развертыванию в отказоустойчивой среде, а код демонстрирует понимание распределённых систем, конкурентности в Go и инженерии надёжности.

Стек: Go, Python, PostgreSQL, Redis Sentinel, Prometheus, Grafana, OpenTelemetry, Docker.
Лицензия: MIT.
