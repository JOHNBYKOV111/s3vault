package metadata

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuditAppendOnly(t *testing.T) {
	connString := "postgres://s3vault:s3vault@localhost:5433/s3vault?sslmode=disable"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer pool.Close()

	repo := &PostgresRepo{pool: pool}

	// Очищаем таблицу аудита перед тестом (чтобы не было старых записей)
	_, err = pool.Exec(ctx, "DELETE FROM audit_log")
	if err != nil {
		t.Fatalf("failed to clean audit_log: %v", err)
	}

	// Используем уникальный action, чтобы не пересекаться с другими тестами
	action := fmt.Sprintf("TEST_ACTION_%d", time.Now().UnixNano())

	// 1. Пишем запись в аудит
	logEntry := &AuditLog{
		TenantID: "3bfb08a3-cc0e-451b-b8cb-51a5ba52a43b",
		Action:   action,
		Resource: "test-resource",
		Result:   "success",
	}

	err = repo.WriteAudit(ctx, logEntry)
	if err != nil {
		t.Fatalf("failed to write audit: %v", err)
	}

	// 2. Проверяем, что запись есть
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_log WHERE action = $1", action).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query audit: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 audit record, got %d", count)
	}

	// 3. Пытаемся изменить запись (UPDATE) — это не должно сработать
	_, err = pool.Exec(ctx, "UPDATE audit_log SET result = 'hacked' WHERE action = $1", action)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// Проверяем, что результат не изменился
	var result string
	err = pool.QueryRow(ctx, "SELECT result FROM audit_log WHERE action = $1", action).Scan(&result)
	if err != nil {
		t.Fatalf("failed to query audit: %v", err)
	}
	if result != "success" {
		t.Errorf("expected result 'success', got '%s' (UPDATE should be blocked)", result)
	}

	// 4. Пытаемся удалить запись (DELETE) — это не должно сработать
	_, err = pool.Exec(ctx, "DELETE FROM audit_log WHERE action = $1", action)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// Проверяем, что запись все еще есть
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_log WHERE action = $1", action).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query audit: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 audit record after delete attempt, got %d (DELETE should be blocked)", count)
	}
}
