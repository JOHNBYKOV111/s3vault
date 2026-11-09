# ADR-004: Tenant Isolation

## Status

Accepted

## Context

s3vault — мультитенантное S3-совместимое хранилище. Один инстанс обслуживает **несколько клиентов (tenant'ов)**, и данные одного tenant'а **не должны** быть доступны другому, даже если злоумышленник знает ключ (key) или bucket name.

Требования:
- **Изоляция данных**: tenant A не видит объекты tenant B.
- **Изоляция бакетов**: имена бакетов могут совпадать у разных tenant'ов, но это **разные** бакеты.
- **Аудит**: каждое действие записывается с `tenant_id`.
- **RBAC**: доступ к операциям (`GetObject`, `PutObject`, `ListBucket`) проверяется по политике tenant'а.
- **Производительность**: фильтрация по `tenant_id` не должна «убивать» latency (нужны индексы).

## Decision

**Изоляция на уровне БД (row-level) через `tenant_id` во всех таблицах, с обязательной фильтрацией в Repository.**

### Схема БД

Во всех таблицах, где есть данные tenant'а, присутствует `tenant_id`:

```sql
CREATE TABLE tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE buckets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, name)  -- имя бакета уникально ВНУТРИ tenant'а
);

CREATE TABLE objects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bucket_id UUID NOT NULL REFERENCES buckets(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    -- ...
    UNIQUE(bucket_id, key)  -- ключ уникален ВНУТРИ бакета
);