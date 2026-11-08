package auth

import (
	"context"
	"errors"
)

// Контекстный ключ для tenant_id (чтобы не конфликтовать с другими ключами).
type contextKey string

const tenantIDKey contextKey = "tenant_id"

// ErrNoTenant — ошибка, если tenant_id не найден в контексте.
var ErrNoTenant = errors.New("tenant_id not found in context")

// WithTenantID добавляет tenant_id в контекст.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

// TenantIDFromContext извлекает tenant_id из контекста.
// Возвращает ошибку, если tenant_id не найден.
func TenantIDFromContext(ctx context.Context) (string, error) {
	val := ctx.Value(tenantIDKey)
	if val == nil {
		return "", ErrNoTenant
	}
	tenantID, ok := val.(string)
	if !ok || tenantID == "" {
		return "", ErrNoTenant
	}
	return tenantID, nil
}
