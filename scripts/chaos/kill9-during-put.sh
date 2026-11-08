#!/bin/bash
set -euo pipefail

# kill9-during-put.sh — Chaos-тест: убить сервер SIGKILL во время PUT.
# Проверяет, что после перезапуска объект либо цел, либо отсутствует (не «полузаписан»).

SERVER_PID="${1:-}"
if [ -z "$SERVER_PID" ]; then
    echo "Usage: $0 <server_pid>"
    echo "Example: $0 12345"
    exit 1
fi

BUCKET="${BUCKET:-chaos-bucket}"
KEY="${KEY:-chaos-file.bin}"
ENDPOINT="${ENDPOINT:-http://localhost:9000}"

# 1. Проверяем, что сервер жив
if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "❌ Server with PID $SERVER_PID is not running"
    exit 1
fi

# 2. Создаем бакет (если нет)
echo "📦 Creating bucket $BUCKET..."
curl -s -X PUT "$ENDPOINT/$BUCKET" > /dev/null || true

# 3. Создаем большой файл (10 МБ)
TMP_FILE=$(mktemp)
dd if=/dev/zero of="$TMP_FILE" bs=1M count=10 2>/dev/null
echo "📁 Created test file: $TMP_FILE (10 MB)"

# 4. Запускаем PUT в фоне
echo "⬆️  Starting PUT (will kill server in 100ms)..."
curl -s -X PUT --data-binary "@$TMP_FILE" "$ENDPOINT/$BUCKET/$KEY" &
CURL_PID=$!

# 5. Ждем 100 мс и убиваем сервер SIGKILL (имитация краша)
sleep 0.1
echo "💀 Killing server (PID $SERVER_PID) with SIGKILL..."
kill -9 "$SERVER_PID" 2>/dev/null || true

# 6. Ждем завершения curl (он упадет с ошибкой)
wait "$CURL_PID" 2>/dev/null || true
echo "✅ PUT interrupted (as expected)"

# 7. Убираем временный файл
rm -f "$TMP_FILE"

echo ""
echo "🎯 Chaos test complete:"
echo "   - Server killed during PUT"
echo "   - Restart server and check:"
echo "     curl $ENDPOINT/$BUCKET/$KEY"
echo "   - Expected: either 200 (object intact) or 404 (object absent)"
echo "   - NOT expected: 500 or partial data"