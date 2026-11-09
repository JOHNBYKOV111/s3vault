#!/usr/bin/env python3
"""
scripts/conformance/boto3_conformance.py

Conformance-тест S3-совместимости s3vault через boto3 (AWS SDK для Python).

Проверяет:
  1. CreateBucket
  2. PutObject
  3. GetObject
  4. HeadObject
  5. ListObjectsV2
  6. Range-запрос (206 Partial Content)
  7. Multipart upload (create → upload_part → complete)
  8. DeleteObject
  9. DeleteBucket

Требования:
  - pip install boto3
  - s3vault запущен на http://localhost:9000
  - (опционально) tenant создан (см. ci.yml: Seed test tenant)

Запуск:
  python3 scripts/conformance/boto3_conformance.py

Коды возврата:
  0 — все проверки прошли
  1 — хотя бы одна проверка упала
"""

import os
import sys
import uuid

import boto3
from botocore.client import Config
from botocore.exceptions import ClientError

# --- config ---
ENDPOINT = os.environ.get("S3VAULT_ENDPOINT", "http://localhost:9000")
ACCESS_KEY = os.environ.get("S3VAULT_ACCESS_KEY", "test")
SECRET_KEY = os.environ.get("S3VAULT_SECRET_KEY", "test")
REGION = os.environ.get("S3VAULT_REGION", "us-east-1")

# Уникальные имена, чтобы не конфликтовать с прошлыми прогонами.
RUN_ID = uuid.uuid4().hex[:8]
BUCKET = f"conformance-bucket-{RUN_ID}"
KEY = "test-file.txt"
MULTIPART_KEY = "multipart-file.bin"


def make_client():
    return boto3.client(
        "s3",
        endpoint_url=ENDPOINT,
        aws_access_key_id=ACCESS_KEY,
        aws_secret_access_key=SECRET_KEY,
        region_name=REGION,
        config=Config(signature_version="s3v4", s3={"addressing_style": "path"}),
    )


def check(name, fn):
    """Запускает проверку, ловит исключения, логирует PASS/FAIL."""
    try:
        fn()
        print(f"[PASS] {name}")
        return True
    except Exception as e:
        print(f"[FAIL] {name}: {e}")
        return False


def main():
    client = make_client()
    results = []

    # 1. CreateBucket
    def create_bucket():
        try:
            client.create_bucket(Bucket=BUCKET)
        except ClientError as e:
            if e.response["Error"]["Code"] != "BucketAlreadyExists":
                raise
    results.append(check("CreateBucket", create_bucket))

    # 2. PutObject
    body = b"Hello from boto3 conformance!"

    def put_object():
        client.put_object(Bucket=BUCKET, Key=KEY, Body=body)
    results.append(check("PutObject", put_object))

    # 3. GetObject
    def get_object():
        resp = client.get_object(Bucket=BUCKET, Key=KEY)
        data = resp["Body"].read()
        assert data == body, f"expected {body!r}, got {data!r}"
    results.append(check("GetObject", get_object))

    # 4. HeadObject
    def head_object():
        resp = client.head_object(Bucket=BUCKET, Key=KEY)
        assert resp["ContentLength"] == len(body), \
            f"expected ContentLength={len(body)}, got {resp['ContentLength']}"
    results.append(check("HeadObject", head_object))

    # 5. ListObjectsV2
    def list_objects():
        resp = client.list_objects_v2(Bucket=BUCKET)
        keys = [obj["Key"] for obj in resp.get("Contents", [])]
        assert KEY in keys, f"expected {KEY} in {keys}"
    results.append(check("ListObjectsV2", list_objects))

    # 6. Range-запрос (206)
    def range_request():
        resp = client.get_object(Bucket=BUCKET, Key=KEY, Range="bytes=0-4")
        assert resp["ResponseMetadata"]["HTTPStatusCode"] == 206, \
            f"expected 206, got {resp['ResponseMetadata']['HTTPStatusCode']}"
        data = resp["Body"].read()
        assert data == body[:5], f"expected {body[:5]!r}, got {data!r}"
        assert "ContentRange" in resp, "expected ContentRange header"
    results.append(check("Range (206)", range_request))

    # 7. Multipart upload
    def multipart():
        mp = client.create_multipart_upload(Bucket=BUCKET, Key=MULTIPART_KEY)
        upload_id = mp["UploadId"]
        parts = []
        for i, chunk in enumerate([b"part1-", b"part2-", b"part3"], start=1):
            resp = client.upload_part(
                Bucket=BUCKET, Key=MULTIPART_KEY, UploadId=upload_id,
                PartNumber=i, Body=chunk,
            )
            parts.append({"PartNumber": i, "ETag": resp["ETag"]})
        client.complete_multipart_upload(
            Bucket=BUCKET, Key=MULTIPART_KEY, UploadId=upload_id,
            MultipartUpload={"Parts": parts},
        )
        resp = client.get_object(Bucket=BUCKET, Key=MULTIPART_KEY)
        data = resp["Body"].read()
        expected = b"part1-part2-part3"
        assert data == expected, f"expected {expected!r}, got {data!r}"
    results.append(check("Multipart upload", multipart))

    # 8. DeleteObject
    def delete_object():
        client.delete_object(Bucket=BUCKET, Key=KEY)
        try:
            client.head_object(Bucket=BUCKET, Key=KEY)
            raise AssertionError("expected 404 after delete")
        except ClientError as e:
            if e.response["Error"]["Code"] not in ("404", "NoSuchKey"):
                raise
    results.append(check("DeleteObject", delete_object))

    # 9. DeleteBucket
    def delete_bucket():
        # Удаляем multipart-объект, чтобы бакет был пустым.
        client.delete_object(Bucket=BUCKET, Key=MULTIPART_KEY)
        client.delete_bucket(Bucket=BUCKET)
        try:
            client.head_bucket(Bucket=BUCKET)
            raise AssertionError("expected 404 after delete_bucket")
        except ClientError as e:
            if e.response["Error"]["Code"] != "404":
                raise
    results.append(check("DeleteBucket", delete_bucket))

    # --- итог ---
    passed = sum(results)
    total = len(results)
    print(f"\n{passed}/{total} checks passed")
    if passed != total:
        print("CONFORMANCE FAILED", file=sys.stderr)
        return 1
    print("CONFORMANCE PASSED")
    return 0


if __name__ == "__main__":
    sys.exit(main())