package metadata

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestAuditAppendOnly(t *testing.T) {
	repo, cleanup := SetupTestRepoWithContainer(t)
	defer cleanup()

	ctx := context.Background()

	// Используем уникальный action, чтобы не пересекаться с другими тестами.
	action := fmt.Sprintf("TEST_ACTION_%d", time.Now().UnixNano())

	// 1. Пишем запись в аудит.
	logEntry := &AuditLog{
		TenantID: "3bfb08a3-cc0e-451b-b8cb-51a5ba52a43b",
		Action:   action,
		Resource: "test-resource",
		Result:   "success",
	}

	if err := repo.WriteAudit(ctx, logEntry); err != nil {
		t.Fatalf("failed to write audit: %v", err)
	}

	// 2. Проверяем, что запись есть.
	var count int
	if err := repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM audit_log WHERE action = $1", action).Scan(&count); err != nil {
		t.Fatalf("failed to query audit: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 audit record, got %d", count)
	}

	// 3. UPDATE игнорируется правилом audit_log_no_update (DO INSTEAD NOTHING):
	// ошибки нет, но данные не меняются.
	if _, err := repo.GetPool().Exec(ctx, "UPDATE audit_log SET result = 'hacked' WHERE action = $1", action); err != nil {
		t.Fatalf("UPDATE returned unexpected error: %v", err)
	}

	var result string
	if err := repo.GetPool().QueryRow(ctx, "SELECT result FROM audit_log WHERE action = $1", action).Scan(&result); err != nil {
		t.Fatalf("failed to query audit: %v", err)
	}
	if result != "success" {
		t.Errorf("expected result 'success' (UPDATE should be ignored), got '%s'", result)
	}

	// 4. DELETE игнорируется правилом audit_log_no_delete (DO INSTEAD NOTHING):
	// ошибки нет, но запись остаётся.
	if _, err := repo.GetPool().Exec(ctx, "DELETE FROM audit_log WHERE action = $1", action); err != nil {
		t.Fatalf("DELETE returned unexpected error: %v", err)
	}

	if err := repo.GetPool().QueryRow(ctx, "SELECT COUNT(*) FROM audit_log WHERE action = $1", action).Scan(&count); err != nil {
		t.Fatalf("failed to query audit: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 audit record after DELETE (DELETE should be ignored), got %d", count)
	}
}
