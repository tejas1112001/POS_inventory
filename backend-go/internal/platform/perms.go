// Package platform is the Super Admin side: platform users, MFA, sessions,
// step-up, tenant administration and the platform audit log
// (PHASE1_DESIGN §7). It is mounted only on admin.<BASE_DOMAIN>.
package platform

// Platform roles.
const (
	RoleSuperAdmin   = "super_admin"
	RoleBillingAdmin = "billing_admin"
	RoleSupport      = "support"
)

// Platform permissions (PHASE1_DESIGN §7.1).
const (
	PermTenantsView        = "tenants:view"
	PermTenantsCreate      = "tenants:create"
	PermTenantsEdit        = "tenants:edit"
	PermTenantsSuspend     = "tenants:suspend"
	PermTenantsDelete      = "tenants:delete"
	PermImpersonateRO      = "tenants:impersonate_ro"
	PermImpersonateRW      = "tenants:impersonate_rw"
	PermTenantsExport      = "tenants:export"
	PermPlansManage        = "plans:manage"
	PermBillingView        = "billing:view"
	PermBillingManage      = "billing:manage"
	PermNotificationsSend  = "notifications:send"
	PermMigrationsView     = "migrations:view"
	PermMigrationsRun      = "migrations:run"
	PermAuditViewAll       = "audit:view_all"
	PermAuditViewOwn       = "audit:view_own"
	PermPlatformUsersAdmin = "platform_users:manage"
)

var rolePerms = map[string]map[string]bool{
	RoleSuperAdmin: set(PermTenantsView, PermTenantsCreate, PermTenantsEdit, PermTenantsSuspend, PermTenantsDelete,
		PermImpersonateRO, PermImpersonateRW, PermTenantsExport, PermPlansManage, PermBillingView, PermBillingManage,
		PermNotificationsSend, PermMigrationsView, PermMigrationsRun, PermAuditViewAll, PermAuditViewOwn, PermPlatformUsersAdmin),
	RoleBillingAdmin: set(PermTenantsView, PermTenantsSuspend, PermPlansManage, PermBillingView, PermBillingManage,
		PermNotificationsSend, PermAuditViewOwn),
	RoleSupport: set(PermTenantsView, PermImpersonateRO, PermBillingView, PermMigrationsView, PermAuditViewOwn),
}

func set(ps ...string) map[string]bool {
	m := make(map[string]bool, len(ps))
	for _, p := range ps {
		m[p] = true
	}
	return m
}

// Can reports whether a platform role holds a permission.
func Can(role, perm string) bool { return rolePerms[role][perm] }

// Permissions lists a role's permissions (for /me).
func Permissions(role string) []string {
	out := make([]string, 0, len(rolePerms[role]))
	for p := range rolePerms[role] {
		out = append(out, p)
	}
	return out
}
