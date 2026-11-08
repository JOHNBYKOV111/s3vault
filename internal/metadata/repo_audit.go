package metadata

import (
	"context"
	"fmt"
)

// WriteAudit записывает событие в журнал аудита.
// Журнал append-only: правила в БД (audit_log_no_update, audit_log_no_delete)
// запрещают изменение и удаление записей.
func (r *PostgresRepo) WriteAudit(ctx context.Context, log *AuditLog) error {
	query := `
		INSERT INTO audit_log (tenant_id, action, resource, result)
		VALUES ($1, $2, $3, $4)
	`

	_, err := r.pool.Exec(ctx, query, log.TenantID, log.Action, log.Resource, log.Result)
	if err != nil {
		return fmt.Errorf("write audit: %w", err)
	}

	return nil
}
