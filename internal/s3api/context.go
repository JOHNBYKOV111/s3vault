// Package s3api provides S3-compatible HTTP handlers.
package s3api

// contextKey — приватный тип для ключей контекста.
// Используется вместо string, чтобы избежать коллизий между пакетами
// (staticcheck SA1029). Ключи контекста должны быть неэкспортируемыми
// типами, чтобы никто снаружи не мог их перезаписать.
type contextKey string

const (
	// tenantIDKey — ключ контекста для tenant_id.
	// В хендлерах: ctx.Value(tenantIDKey).(string)
	tenantIDKey contextKey = "tenant_id"
)
