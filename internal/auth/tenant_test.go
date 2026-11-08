package auth

import (
	"testing"
)

func TestTenantIsolation(t *testing.T) {
	// 1. Создаем RBAC с политиками для двух арендаторов
	rbac := NewRBAC()
	rbac.AddPolicy("tenant-a", "s3:GetObject", "arn:aws:s3:::bucket-a/*")
	rbac.AddPolicy("tenant-b", "s3:GetObject", "arn:aws:s3:::bucket-b/*")

	// 2. Проверяем изоляцию: tenant-a не видит bucket-b
	if rbac.Allow("tenant-a", "s3:GetObject", "arn:aws:s3:::bucket-b/key") {
		t.Error("expected deny for tenant-a on bucket-b, got allow")
	}

	// 3. Проверяем изоляцию: tenant-b не видит bucket-a
	if rbac.Allow("tenant-b", "s3:GetObject", "arn:aws:s3:::bucket-a/key") {
		t.Error("expected deny for tenant-b on bucket-a, got allow")
	}

	// 4. Проверяем, что tenant-a видит свой bucket-a
	if !rbac.Allow("tenant-a", "s3:GetObject", "arn:aws:s3:::bucket-a/key") {
		t.Error("expected allow for tenant-a on bucket-a, got deny")
	}

	// 5. Проверяем, что tenant-b видит свой bucket-b
	if !rbac.Allow("tenant-b", "s3:GetObject", "arn:aws:s3:::bucket-b/key") {
		t.Error("expected allow for tenant-b on bucket-b, got deny")
	}

	// 6. Проверяем, что неизвестный арендатор получает deny
	if rbac.Allow("tenant-c", "s3:GetObject", "arn:aws:s3:::bucket-a/key") {
		t.Error("expected deny for unknown tenant-c, got allow")
	}
}
