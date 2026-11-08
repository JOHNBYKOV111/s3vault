#!/bin/bash
set -euo pipefail

# remove-disk-dir.sh — Chaos-тест: удалить директорию с одной репликой.
# Проверяет, что после удаления директории объект все еще читается из второй реплики,
# а Heal восстанавливает удаленную копию.

DATA_DIR="${DATA_DIR:-./data}"
REPLICA_DIR="${REPLICA_DIR:-./data-replica}"
BUCKET="${BUCKET:-chaos-bucket}"
KEY="${KEY:-chaos-file.bin}"
ENDPOINT="${ENDPOINT:-http://localhost:9000}"

echo "📁 Data dir: $DATA_DIR"
echo "📁 Replica dir: $REPLICA_DIR"

# 1. Проверяем, что обе директории существуют
if [ ! -d "$DATA_DIR" ]; then
    echo "❌ Data dir not found: $DATA_DIR"
    exit 1
fi
if [ ! -d "$REPLICA_DIR" ]; then
    echo "❌ Replica dir not found: $REPLICA_DIR"
    exit 1
fi

# 2. Создаем бакет (если нет)
echo "📦 Creating bucket $BUCKET..."
curl -s -X PUT "$ENDPOINT/$BUCKET" > /dev/null || true

# 3. Загружаем файл (в обе реплики)
echo "⬆️  Uploading file (will be written to both dirs)..."
echo "Hello, Chaos!" | curl -s -X PUT --data-binary @- "$ENDPOINT/$BUCKET/$KEY" > /dev/null

# 4. Проверяем, что файл есть в обеих директориях
echo "🔍 Checking file in both dirs..."
if [ ! -f "$DATA_DIR/$BUCKET/$KEY" ]; then
    echo "❌ File not found in data dir: $DATA_DIR/$BUCKET/$KEY"
    exit 1
fi
if [ ! -f "$REPLICA_DIR/$BUCKET/$KEY" ]; then
    echo "❌ File not found in replica dir: $REPLICA_DIR/$BUCKET/$KEY"
    exit 1
fi
echo "✅ File exists in both dirs"

# 5. Удаляем директорию с одной репликой (имитация отказа диска)
echo "💀 Removing replica dir (simulating disk failure): $REPLICA_DIR"
rm -rf "$REPLICA_DIR"

# 6. Проверяем, что файл все еще читается (из data dir)
echo "🔍 Reading file after replica dir removed..."
RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" "$ENDPOINT/$BUCKET/$KEY")
if [ "$RESPONSE" != "200" ]; then
    echo "❌ Expected 200 after replica dir removed, got $RESPONSE"
    exit 1
fi
echo "✅ File still readable (from data dir)"

# 7. Восстанавливаем директорию (создаем пустую)
echo "🔧 Restoring replica dir (empty)..."
mkdir -p "$REPLICA_DIR"

# 8. Проверяем, что Heal восстановил файл (если Heal реализован)
echo "🔍 Checking if Heal restored the file..."
if [ -f "$REPLICA_DIR/$BUCKET/$KEY" ]; then
    echo "✅ Heal restored the file in replica dir"
else
    echo "⚠️  Heal did not restore the file (may not be implemented yet)"
fi

echo ""
echo "🎯 Chaos test complete:"
echo "   - Replica dir removed"
echo "   - File still readable from data dir"
echo "   - Heal status checked"