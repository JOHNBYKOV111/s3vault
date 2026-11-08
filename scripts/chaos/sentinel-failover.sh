#!/usr/bin/env bash
#
# scripts/chaos/sentinel-failover.sh
#
# Chaos-тест Sentinel failover для s3vault (ТЗ Часть 7).
#
# Сценарий:
#   1. Поднять стенд: redis-master + redis-replica + sentinel.
#   2. Запустить тестовый клиент (сервис redis-sentinel-check) в Docker-сети,
#      который ходит через Sentinel и логирует, к какому master подключён.
#   3. Дождаться, пока клиент увидит master (OK).
#   4. Имитировать падение Redis-процесса (shutdown nosave) — не убиваем
#      контейнер, чтобы не сломать bridge-сеть Docker и не менять IP-адреса.
#   5. Дождаться, пока Sentinel повысит replica до master.
#   6. Проверить, что клиент переподключился: в логах есть
#      "FAILOVER: was <old>, now <new>".
#   7. Погасить стенд (docker compose down -v).
#
# DoD Части 7: "Sentinel failover зелёный".
#
# Требования:
#   - docker + docker compose v2
#
# Запуск из корня проекта:
#   ./scripts/chaos/sentinel-failover.sh
#
set -euo pipefail

# --- paths ---
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/deploy/docker-compose.sentinel.yml"
CLIENT_SERVICE="redis-sentinel-check"
LOG_FILE="$(mktemp -t sentinel-client-XXXXXX.log)"
CLIENT_PID=""
MASTER_CONTAINER_ID=""

# --- params ---
SENTINEL_NAME="mymaster"
SENTINEL_PORT="26379"
WAIT_MASTER_TIMEOUT=60   # сек, ждём первого OK от клиента
WAIT_FAILOVER_TIMEOUT=90 # сек, ждём повышения replica
CHECK_INTERVAL=1         # сек, интервал опроса

# --- colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${YELLOW}[sentinel-failover]${NC} $*"; }
ok()   { echo -e "${GREEN}[OK]${NC} $*"; }
fail() { echo -e "${RED}[FAIL]${NC} $*" >&2; }

cleanup() {
    local code=$?
    if [[ -n "${CLIENT_PID}" ]] && kill -0 "${CLIENT_PID}" 2>/dev/null; then
        kill -TERM "${CLIENT_PID}" 2>/dev/null || true
        wait "${CLIENT_PID}" 2>/dev/null || true
    fi
    # Возвращаем restart-политику master'а (если контейнер ещё существует).
    # Это нужно, чтобы стенд был в исходном состоянии после теста.
    if [[ -n "${MASTER_CONTAINER_ID}" ]]; then
        docker update --restart=unless-stopped "${MASTER_CONTAINER_ID}" >/dev/null 2>&1 || true
    fi
    log "cleanup: docker compose down -v"
    docker compose -f "${COMPOSE_FILE}" down -v --remove-orphans >/dev/null 2>&1 || true
    if [[ -f "${LOG_FILE}" ]]; then
        log "client log: ${LOG_FILE}"
    fi
    exit "${code}"
}
trap cleanup EXIT INT TERM

# --- 1. поднять стенд ---
log "step 1: up sentinel stand"
docker compose -f "${COMPOSE_FILE}" up -d --build

# Ждём, пока sentinel начнёт отвечать на PING.
log "waiting for sentinel on localhost:${SENTINEL_PORT}..."
for i in $(seq 1 30); do
    if docker compose -f "${COMPOSE_FILE}" exec -T sentinel \
        redis-cli -p "${SENTINEL_PORT}" ping 2>/dev/null | grep -q PONG; then
        ok "sentinel is up"
        break
    fi
    if [[ "${i}" -eq 30 ]]; then
        fail "sentinel did not become ready in 30s"
        exit 1
    fi
    sleep 1
done

# --- 2. запустить тестовый клиент в Docker-сети ---
log "step 2: start client (service ${CLIENT_SERVICE})"
# Клиент запускается через `docker compose run` — внутри Docker-сети,
# где redis-master и sentinel резолвятся по именам сервисов.
# Логи клиента пишем в файл (stdout/stderr контейнера).
docker compose -f "${COMPOSE_FILE}" run --rm -T "${CLIENT_SERVICE}" >"${LOG_FILE}" 2>&1 &
CLIENT_PID=$!
log "client pid=${CLIENT_PID}, log=${LOG_FILE}"

# --- 3. дождаться первого OK ---
log "step 3: wait for first OK from client (timeout ${WAIT_MASTER_TIMEOUT}s)"
deadline=$(( $(date +%s) + WAIT_MASTER_TIMEOUT ))
while true; do
    # Ищем "[OK]" в любом месте строки: клиент пишет "2026/11/08 ... [OK] ping ..."
    # (log.LstdFlags ставит таймстамп в начало). ^\[OK\] не сработал бы.
    if grep -q '\[OK\]' "${LOG_FILE}" 2>/dev/null; then
        ok "client connected to master"
        break
    fi
    if ! kill -0 "${CLIENT_PID}" 2>/dev/null; then
        fail "client exited prematurely; log:"
        cat "${LOG_FILE}" >&2
        exit 1
    fi
    if [[ "$(date +%s)" -ge "${deadline}" ]]; then
        fail "client did not connect within ${WAIT_MASTER_TIMEOUT}s; log:"
        cat "${LOG_FILE}" >&2
        exit 1
    fi
    sleep 1
done

OLD_MASTER="$(grep '\[OK\]' "${LOG_FILE}" | head -1 | sed -n 's/.*master=\([^ ]*\).*/\1/p')"
log "old master: ${OLD_MASTER}"

# --- 4. имитировать падение Redis-процесса (shutdown nosave) ---
log "step 4: shutdown redis-master (nosave)"
MASTER_CONTAINER_ID="$(docker compose -f "${COMPOSE_FILE}" ps -q redis-master)"
if [[ -z "${MASTER_CONTAINER_ID}" ]]; then
    fail "redis-master container not found"
    exit 1
fi
# Временно отключаем restart, чтобы Docker не поднял контейнер обратно
# сразу после shutdown (иначе Sentinel отменит failover).
docker update --restart=no "${MASTER_CONTAINER_ID}" >/dev/null
# Имитируем падение Redis-процесса, а не убийство контейнера.
# Сеть Docker не ломается, IP-адреса не меняются.
docker compose -f "${COMPOSE_FILE}" exec -T redis-master \
    redis-cli -p 6379 shutdown nosave || true
ok "redis-master shutdown (nosave)"

# --- 5. дождаться повышения replica ---
log "step 5: wait for failover (timeout ${WAIT_FAILOVER_TIMEOUT}s)"
deadline=$(( $(date +%s) + WAIT_FAILOVER_TIMEOUT ))
NEW_MASTER=""
while true; do
    # SENTINEL get-master-addr-by-name mymaster → "<ip>\n<port>"
    addr="$(docker compose -f "${COMPOSE_FILE}" exec -T sentinel \
        redis-cli -p "${SENTINEL_PORT}" \
        SENTINEL get-master-addr-by-name "${SENTINEL_NAME}" 2>/dev/null || true)"
    # Парсим: первая строка — ip, вторая — port.
    ip="$(echo "${addr}" | sed -n '1p' | tr -d '\r')"
    port="$(echo "${addr}" | sed -n '2p' | tr -d '\r')"
    if [[ -n "${ip}" && -n "${port}" && "${ip}" != "${OLD_MASTER%%:*}" ]]; then
        NEW_MASTER="${ip}:${port}"
        ok "sentinel promoted new master: ${NEW_MASTER}"
        break
    fi
    if [[ "$(date +%s)" -ge "${deadline}" ]]; then
        fail "failover did not happen within ${WAIT_FAILOVER_TIMEOUT}s"
        log "sentinel log:"
        docker compose -f "${COMPOSE_FILE}" logs --tail=50 sentinel >&2 || true
        exit 1
    fi
    sleep "${CHECK_INTERVAL}"
done

# --- 6. проверить, что клиент переподключился ---
log "step 6: wait for client to reconnect to ${NEW_MASTER}"
deadline=$(( $(date +%s) + WAIT_FAILOVER_TIMEOUT ))
while true; do
    # Ищем "FAILOVER: was" в любом месте строки.
    if grep -q "FAILOVER: was" "${LOG_FILE}" 2>/dev/null; then
        ok "client reconnected"
        grep "FAILOVER: was" "${LOG_FILE}" | tail -1
        break
    fi
    if ! kill -0 "${CLIENT_PID}" 2>/dev/null; then
        fail "client exited during failover; log:"
        cat "${LOG_FILE}" >&2
        exit 1
    fi
    if [[ "$(date +%s)" -ge "${deadline}" ]]; then
        fail "client did not log FAILOVER within ${WAIT_FAILOVER_TIMEOUT}s; log tail:"
        tail -20 "${LOG_FILE}" >&2
        exit 1
    fi
    sleep "${CHECK_INTERVAL}"
done

ok "SENTINEL FAILOVER TEST PASSED"