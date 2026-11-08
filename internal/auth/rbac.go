package auth

import (
	"strings"
	"sync"
)

// Policy — правило доступа (действие + ресурс).
type Policy struct {
	TenantID string
	Action   string // например, "s3:GetObject" или "s3:*"
	Resource string // например, "arn:aws:s3:::bucket/*" или "*"
}

// RBAC — система разграничения прав (in-memory).
type RBAC struct {
	mu       sync.RWMutex
	policies []Policy
}

// NewRBAC создает пустой RBAC (deny-by-default).
func NewRBAC() *RBAC {
	return &RBAC{policies: make([]Policy, 0)}
}

// AddPolicy добавляет разрешение (allow) для арендатора.
func (r *RBAC) AddPolicy(tenantID, action, resource string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policies = append(r.policies, Policy{
		TenantID: tenantID,
		Action:   action,
		Resource: resource,
	})
}

// Allow проверяет, разрешено ли действие над ресурсом для арендатора.
// Возвращает true, если хотя бы одна политика разрешает (allow).
// Если ни одна не разрешает — false (deny-by-default).
func (r *RBAC) Allow(tenantID, action, resource string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.policies {
		if p.TenantID != tenantID {
			continue
		}
		if matchAction(p.Action, action) && matchResource(p.Resource, resource) {
			return true
		}
	}
	return false
}

// matchAction проверяет совпадение действия (с поддержкой wildcard "*").
func matchAction(policyAction, requestedAction string) bool {
	if policyAction == "*" || policyAction == requestedAction {
		return true
	}
	// Поддержка "s3:*" (все действия в сервисе s3)
	if strings.HasSuffix(policyAction, ":*") {
		prefix := strings.TrimSuffix(policyAction, "*")
		return strings.HasPrefix(requestedAction, prefix)
	}
	return false
}

// matchResource проверяет совпадение ресурса (с поддержкой wildcard "*").
func matchResource(policyResource, requestedResource string) bool {
	if policyResource == "*" || policyResource == requestedResource {
		return true
	}
	// Поддержка "arn:aws:s3:::bucket/*" (все объекты в бакете)
	if strings.HasSuffix(policyResource, "*") {
		prefix := strings.TrimSuffix(policyResource, "*")
		return strings.HasPrefix(requestedResource, prefix)
	}
	return false
}
