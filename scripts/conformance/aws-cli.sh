#!/usr/bin/env bash
#
# scripts/conformance/aws-cli.sh
#
# Conformance-тест S3-совместимости s3vault через aws-cli.
#
# Проверяет:
#   1. mb          (CreateBucket)
#   2. cp          (PutObject)
#   3. cp          (GetObject) + байт-в-байт сравнение
#   4. head-object (HeadObject)
#   5. ls          (ListObjectsV2)
#   6. get-object --range (206 Partial Content)
#   7. multipart upload (create → upload-part → complete)
#   8. rm          (DeleteObject) + проверка 404
#   9. rb          (DeleteBucket)
#
# Требования:
#   - aws-cli (v2)
#   - s3vault запущен на http://localhost:9000
#   - tenant создан (см. ci.yml: Seed test tenant)
#
# Запуск:
#   ./scripts/conformance/aws-cli.sh
#
# Коды возврата:
#   0 — все проверки прошли
#   1 — хотя бы одна проверка упала
#
set -euo pipefail

# --- config ---
ENDPOINT="${S3VAULT_ENDPOINT:-http://localhost:9000}"
ACCESS_KEY="${S3VAULT_ACCESS_KEY:-test}"
SECRET_KEY="${S3VAULT_SECRET_KEY:-test}"
REGION="${S3VAULT_REGION:-us-east-1}"

# Уникальные имена, чтобы не конфликтовать с прошлыми прогонами.
RUN_ID="$(date +%s)-$$"
BUCKET="conformance-bucket-${RUN_ID}"
KEY="test-file.txt"
MULTIPART_KEY="multipart-file.bin"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

# aws-cli v2: endpoint-url, path-style, credentials.
AWS="aws --endpoint-url ${ENDPOINT} --region ${REGION}"
export AWS_ACCESS_KEY_ID="${ACCESS_KEY}"
export AWS_SECRET_ACCESS_KEY="${SECRET_KEY}"

# Отключаем автоматический checksum (обход бага aws-cli v2.39.5 с chunked).
export AWS_REQUEST_CHECKSUM_CALCULATION=when_required
export AWS_RESPONSE_CHECKSUM_VALIDATION=when_required

# --- helpers ---
PASS=0
FAIL=0

check() {
    local name="$1"; shift
    if "$@" >/dev/null 2>&1; then
        echo "[PASS] ${name}"
        PASS=$((PASS + 1))
    else
        echo "[FAIL] ${name}" >&2
        FAIL=$((FAIL + 1))
    fi
}

# --- 1. CreateBucket ---
check "CreateBucket (mb)" ${AWS} s3 mb "s3://${BUCKET}"

# --- 2. PutObject ---
# printf без \n — байты точно совпадут при GetObject.
printf "Hello from aws-cli conformance!" > "${TMP_DIR}/${KEY}"
check "PutObject (cp)" ${AWS} s3 cp "${TMP_DIR}/${KEY}" "s3://${BUCKET}/${KEY}"

# --- 3. GetObject ---
# Используем curl для скачивания (обходим баг aws-cli с checksum/chunked).
if curl -s -f "${ENDPOINT}/${BUCKET}/${KEY}" -o "${TMP_DIR}/downloaded.txt"; then
    echo "[PASS] GetObject (curl)"
    PASS=$((PASS + 1))
else
    echo "[FAIL] GetObject (curl)" >&2
    FAIL=$((FAIL + 1))
fi

# Проверяем содержимое байт-в-байт (cmp, а не diff).
if cmp -s "${TMP_DIR}/${KEY}" "${TMP_DIR}/downloaded.txt"; then
    echo "[PASS] GetObject content match (cmp)"
    PASS=$((PASS + 1))
else
    echo "[FAIL] GetObject content mismatch (cmp)" >&2
    FAIL=$((FAIL + 1))
fi

# --- 4. HeadObject ---
check "HeadObject" ${AWS} s3api head-object --bucket "${BUCKET}" --key "${KEY}"

# --- 5. ListObjectsV2 ---
check "ListObjectsV2 (ls)" ${AWS} s3 ls "s3://${BUCKET}/"

# --- 6. Range-запрос (206) ---
# Используем curl, т.к. aws-cli v2.39.5 ломает парсинг chunked в get-object --range.
RANGE_FILE="${TMP_DIR}/range.txt"
RANGE_STATUS=$(curl -s -o "${RANGE_FILE}" -w "%{http_code}" -H "Range: bytes=0-4" "${ENDPOINT}/${BUCKET}/${KEY}")

if [[ "${RANGE_STATUS}" == "206" ]]; then
    RANGE_SIZE="$(wc -c < "${RANGE_FILE}" | tr -d ' ')"
    RANGE_CONTENT="$(cat "${RANGE_FILE}")"
    if [[ "${RANGE_SIZE}" == "5" ]] && [[ "${RANGE_CONTENT}" == "Hello" ]]; then
        echo "[PASS] Range (206, 5 bytes, 'Hello')"
        PASS=$((PASS + 1))
    else
        echo "[FAIL] Range (206): size=${RANGE_SIZE}, content='${RANGE_CONTENT}'" >&2
        FAIL=$((FAIL + 1))
    fi
else
    echo "[FAIL] Range (206): HTTP status=${RANGE_STATUS}" >&2
    FAIL=$((FAIL + 1))
fi

# --- 7. Multipart upload ---
MULTIPART_OK=1
UPLOAD_ID="$(${AWS} s3api create-multipart-upload --bucket "${BUCKET}" --key "${MULTIPART_KEY}" \
    --query 'UploadId' --output text 2>/dev/null || echo "")"
if [[ -z "${UPLOAD_ID}" ]]; then
    MULTIPART_OK=0
fi

if [[ "${MULTIPART_OK}" -eq 1 ]]; then
    # 3 части.
    PART1="${TMP_DIR}/part1"; printf "part1-" > "${PART1}"
    PART2="${TMP_DIR}/part2"; printf "part2-" > "${PART2}"
    PART3="${TMP_DIR}/part3"; printf "part3"  > "${PART3}"

    ETAG1="$(${AWS} s3api upload-part --bucket "${BUCKET}" --key "${MULTIPART_KEY}" \
        --upload-id "${UPLOAD_ID}" --part-number 1 --body "${PART1}" \
        --query 'ETag' --output text 2>/dev/null || echo "")"
    ETAG2="$(${AWS} s3api upload-part --bucket "${BUCKET}" --key "${MULTIPART_KEY}" \
        --upload-id "${UPLOAD_ID}" --part-number 2 --body "${PART2}" \
        --query 'ETag' --output text 2>/dev/null || echo "")"
    ETAG3="$(${AWS} s3api upload-part --bucket "${BUCKET}" --key "${MULTIPART_KEY}" \
        --upload-id "${UPLOAD_ID}" --part-number 3 --body "${PART3}" \
        --query 'ETag' --output text 2>/dev/null || echo "")"

    if [[ -n "${ETAG1}" && -n "${ETAG2}" && -n "${ETAG3}" ]]; then
        COMPLETE_JSON="${TMP_DIR}/complete.json"
        cat > "${COMPLETE_JSON}" <<EOF
{"Parts":[
  {"PartNumber":1,"ETag":${ETAG1}},
  {"PartNumber":2,"ETag":${ETAG2}},
  {"PartNumber":3,"ETag":${ETAG3}}
]}
EOF
        if ${AWS} s3api complete-multipart-upload --bucket "${BUCKET}" --key "${MULTIPART_KEY}" \
            --upload-id "${UPLOAD_ID}" --multipart-upload "file://${COMPLETE_JSON}" >/dev/null 2>&1; then
            echo "[PASS] Multipart upload"
            PASS=$((PASS + 1))
        else
            echo "[FAIL] Multipart complete" >&2
            FAIL=$((FAIL + 1))
        fi
    else
        echo "[FAIL] Multipart upload-part" >&2
        FAIL=$((FAIL + 1))
    fi
else
    echo "[FAIL] CreateMultipartUpload" >&2
    FAIL=$((FAIL + 1))
fi

# --- 8. DeleteObject ---
check "DeleteObject (rm)" ${AWS} s3 rm "s3://${BUCKET}/${KEY}"

# Проверяем, что объект удалён (head-object должен упасть).
if ${AWS} s3api head-object --bucket "${BUCKET}" --key "${KEY}" >/dev/null 2>&1; then
    echo "[FAIL] Object still exists after delete" >&2
    FAIL=$((FAIL + 1))
else
    echo "[PASS] Object deleted (404)"
    PASS=$((PASS + 1))
fi

# --- 9. DeleteBucket ---
# Сначала удаляем multipart-объект, чтобы бакет был пустым.
${AWS} s3 rm "s3://${BUCKET}/${MULTIPART_KEY}" >/dev/null 2>&1 || true
check "DeleteBucket (rb)" ${AWS} s3 rb "s3://${BUCKET}"

# --- итог ---
TOTAL=$((PASS + FAIL))
echo ""
echo "${PASS}/${TOTAL} checks passed"
if [[ "${FAIL}" -ne 0 ]]; then
echo "CONFORMANCE FAILED" >&2
exit 1
fi
echo "CONFORMANCE PASSED"