#!/usr/bin/env bash
#
# scripts/chaos/sentinel-failover.sh
#
# Chaos-тест Sentinel failover для s3vault (ТЗ Часть 7).
#
# Сценарий:
#   1. Поднять стенд: redis-master + redis-replica + sentinel.
#   2. Запустить тестовый клиент (сервис redis-sentinel-check),
#      который ходит через Sentinel и логирует, к какому master подключён.
#   3. Дождаться, пока клиент увидит master (OK).
#   4. Убить master (docker compose kill) — Sentinel видит падение.
#   5. Дождаться, пока Sentinel повысит replica до master
#      и проверить, что новый IP реально стал master'ом (INFO replication).
#   6. Проверить, что клиент переподключился: в логах есть
#      "FAILOVER: was <old>, now <new>".
#   7. Погасить стенд (docker compose down -v).
#
# ВАЖНО: используется bridge-сеть со СТАТИЧЕСКИМИ IP (см. docker-compose.sentinel.yml).
# Статические IP нужны, чтобы после `docker kill redis-master` контейнер
# получил тот же IP (172.28.0.10), и Sentinel не терял адрес.
# Проверки Sentinel идут через `docker compose exec` (внутри сети),
# чтобы не зависеть от проброса портов на хост.
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

# --- params ---
SENTINEL_NAME="mymaster"
SENTINEL_PORT="26379"
WAIT_MASTER_TIMEOUT=60   # сек, ждём первого OK от клиента
WAIT_FAILOVER_TIMEOUT=120 # сек, ждём повышения replica (увеличено)
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

# Ждём, пока sentinel начнёт отвечать на PING (через docker compose exec,
# чтобы не зависеть от проброса портов на хост).
log "waiting for sentinel (port ${SENTINEL_PORT})..."
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

# --- 2. запустить тестовый клиент ---
log "step 2: start client (service ${CLIENT_SERVICE})"
# Клиент запускается через `docker compose run` в bridge-сети.
# Логи клиента пишем в файл (stdout/stderr контейнера).
docker compose -f "${COMPOSE_FILE}" run --rm -T "${CLIENT_SERVICE}" >"${LOG_FILE}" 2>&1 &
CLIENT_PID=$!
log "client pid=${CLIENT_PID}, log=${LOG_FILE}"

# --- 3. дождаться первого OK ---
log "step 3: wait for first OK from client (timeout ${WAIT_MASTER_TIMEOUT}s)"
deadline=$(( $(date +%s) + WAIT_MASTER_TIMEOUT ))
while true; do
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

# --- 4. убить master (docker compose kill) ---
log "step 4: kill redis-master"
docker compose -f "${COMPOSE_FILE}" kill redis-master
ok "redis-master killed"

# --- 5. дождаться повышения replica и проверить, что новый IP — master ---
log "step 5: wait for failover (timeout ${WAIT_FAILOVER_TIMEOUT}s)"
deadline=$(( $(date +%s) + WAIT_FAILOVER_TIMEOUT ))
NEW_MASTER=""
while true; do
    # SENTINEL get-master-addr-by-name mymaster → "<ip>\n<port>"
    addr="$(docker compose -f "${COMPOSE_FILE}" exec -T sentinel \
        redis-cli -p "${SENTINEL_PORT}" \
        SENTINEL get-master-addr-by-name "${SENTINEL_NAME}" 2>/dev/null || true)"
    ip="$(echo "${addr}" | sed -n '1p' | tr -d '\r')"
    port="$(echo "${addr}" | sed -n '2p' | tr -d '\r')"
    if [[ -n "${ip}" && -n "${port}" && "${ip}" != "${OLD_MASTER%%:*}" ]]; then
        # Проверяем, что новый IP реально мастер (INFO replication: role:master)
        role="$(docker compose -f "${COMPOSE_FILE}" exec -T sentinel \
            redis-cli -h "${ip}" -p "${port}" INFO replication 2>/dev/null | grep '^role:' | tr -d '\r')"
        if [[ "${role}" == "role:master" ]]; then
            NEW_MASTER="${ip}:${port}"
            ok "sentinel promoted new master: ${NEW_MASTER} (role:master confirmed)"
            break
        fi
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
    # Сначала проверяем лог: если FAILOVER: was уже есть — успех,
    # даже если клиент уже вышел (docker compose run завершается).
    if grep -q "FAILOVER: was" "${LOG_FILE}" 2>/dev/null; then
        ok "client reconnected"
        grep "FAILOVER: was" "${LOG_FILE}" | tail -1
        break
    fi
    # Если клиент ещё жив — ждём дальше.
    if kill -0 "${CLIENT_PID}" 2>/dev/null; then
        :
    else
        # Клиент вышел, но FAILOVER: was в логе нет. Пробуем ещё раз
        # (возможно, лог дописался после выхода).
        if grep -q "FAILOVER: was" "${LOG_FILE}" 2>/dev/null; then
            ok "client reconnected"
            grep "FAILOVER: was" "${LOG_FILE}" | tail -1
            break
        fi
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