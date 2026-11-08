package auth

import (
	"testing"
)

func TestRBAC(t *testing.T) {
	// 1. Создаем RBAC с пустой политикой (deny-by-default)
	rbac := NewRBAC()

	// 2. Пустая политика → 403 на всё
	if rbac.Allow("tenant-a", "s3:GetObject", "arn:aws:s3:::bucket/key") {
		t.Error("expected deny for empty policy, got allow")
	}

	// 3. Добавляем разрешение
	rbac.AddPolicy("tenant-a", "s3:GetObject", "arn:aws:s3:::bucket/*")

	// 4. Проверяем разрешенное действие
	if !rbac.Allow("tenant-a", "s3:GetObject", "arn:aws:s3:::bucket/key") {
		t.Error("expected allow for s3:GetObject on bucket/key, got deny")
	}

	// 5. Проверяем запрещенное действие (другое действие)
	if rbac.Allow("tenant-a", "s3:PutObject", "arn:aws:s3:::bucket/key") {
		t.Error("expected deny for s3:PutObject, got allow")
	}

	// 6. Проверяем запрещенный ресурс (другой бакет)
	if rbac.Allow("tenant-a", "s3:GetObject", "arn:aws:s3:::otherbucket/key") {
		t.Error("expected deny for otherbucket, got allow")
	}

	// 7. Проверяем другого арендатора (изоляция)
	if rbac.Allow("tenant-b", "s3:GetObject", "arn:aws:s3:::bucket/key") {
		t.Error("expected deny for tenant-b, got allow")
	}
}
