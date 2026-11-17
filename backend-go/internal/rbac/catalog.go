// Package rbac holds the permission catalog, the RBAC middleware and the
// anti-escalation rules. The catalog in code is the source of truth; it is
// synced to platform.permission_catalog and to every tenant's permissions.
package rbac

// Permission is one module:action pair.
type Permission struct {
	Module string
	Action string
	Label  string
}

// Key returns "module:action".
func (p Permission) Key() string { return p.Module + ":" + p.Action }

// System role keys (tenant roles.system_key).
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleBiller = "biller"
)

// Catalog is the full permission list (API_CONTRACT §3). Order is stable;
// new permissions are appended.
var Catalog = []Permission{
	{"dashboard", "view", "View dashboard"},
	{"pos", "view", "Open POS"},
	{"pos", "create", "Sell at POS"},
	{"products", "view", "View products"},
	{"products", "create", "Create products"},
	{"products", "edit", "Edit products"},
	{"products", "delete", "Delete products"},
	{"products", "import", "Import products"},
	{"categories", "view", "View categories"},
	{"categories", "create", "Create categories"},
	{"categories", "edit", "Edit categories"},
	{"categories", "delete", "Delete categories"},
	{"brands", "view", "View brands"},
	{"brands", "create", "Create brands"},
	{"brands", "edit", "Edit brands"},
	{"brands", "delete", "Delete brands"},
	{"units", "view", "View units"},
	{"units", "create", "Create units"},
	{"units", "edit", "Edit units"},
	{"units", "delete", "Delete units"},
	{"suppliers", "view", "View suppliers"},
	{"suppliers", "create", "Create suppliers"},
	{"suppliers", "edit", "Edit suppliers"},
	{"suppliers", "delete", "Delete suppliers"},
	{"customers", "view", "View customers"},
	{"customers", "create", "Create customers"},
	{"customers", "edit", "Edit customers"},
	{"customers", "delete", "Delete customers"},
	{"inventory", "view", "View inventory"},
	{"inventory", "edit", "Edit inventory"},
	{"stock_adjustments", "view", "View stock adjustments"},
	{"stock_adjustments", "create", "Create stock adjustments"},
	{"sales", "view", "View own sales"},
	{"sales", "view_all", "View all sales"},
	{"sales", "create", "Create sales"},
	{"sales", "edit", "Edit sales"},
	{"sales", "delete", "Void sales"},
	{"sales", "price_override", "Override prices"},
	{"sales", "discount", "Give discounts"},
	{"sales_returns", "view", "View returns"},
	{"sales_returns", "create", "Create returns"},
	{"purchases", "view", "View purchases"},
	{"purchases", "create", "Create purchases"},
	{"purchases", "edit", "Edit purchases"},
	{"purchases", "approve", "Approve and receive purchases"},
	{"invoices", "view", "View invoices"},
	{"reports", "view", "View reports"},
	{"reports", "export", "Export reports"},
	{"users", "view", "View users"},
	{"users", "create", "Create users"},
	{"users", "edit", "Edit users"},
	{"users", "delete", "Delete users"},
	{"roles", "view", "View roles"},
	{"roles", "create", "Create roles"},
	{"roles", "edit", "Edit roles"},
	{"roles", "delete", "Delete roles"},
	{"stores", "view", "View stores and warehouses"},
	{"stores", "create", "Create stores and warehouses"},
	{"stores", "edit", "Edit stores and warehouses"},
	{"stores", "delete", "Delete stores and warehouses"},
	{"settings", "view", "View settings"},
	{"settings", "edit", "Edit settings"},
	{"email_settings", "view", "View email settings"},
	{"email_settings", "edit", "Edit email settings"},
	{"receipts", "view", "View receipts"},
	{"receipts", "resend", "Resend receipts"},
	{"audit", "view", "View audit log"},
	{"billing", "view", "View billing"},
	{"billing", "manage", "Manage billing"},
	// S10 item 5 (P1).
	{"sales", "credit", "Sell on credit or part payment"},
	{"customers", "credit_limit", "Set customer credit limits"},
	{"customers", "collect", "Collect customer payments"},
	// Industry Phase 1 (docs/INDUSTRY_PHASE1_DESIGN.md).
	{"modules", "manage", "Change business type and modules"}, // P1, owner only
	{"attributes", "view", "View product attributes"},         // P2
	{"attributes", "create", "Create product attributes"},
	{"attributes", "edit", "Edit product attributes"},
	{"attributes", "delete", "Delete product attributes"},
	{"labels", "print", "Print barcode labels"}, // P3 (admin; not billers)
}

// ownerOnlyKeys are single permissions the admin system role does not get
// (besides the whole billing module).
var ownerOnlyKeys = []string{
	"sales:credit", "customers:credit_limit", // S10 P1
	"modules:manage", // industry P1
}

// OwnerOnly are permissions the admin system role does not get: billing,
// selling on credit and setting credit limits (S10 P1), and changing the
// business type and modules (industry P1). The owner can give them to
// custom roles.
func OwnerOnly(p Permission) bool {
	if p.Module == "billing" {
		return true
	}
	for _, k := range ownerOnlyKeys {
		if p.Key() == k {
			return true
		}
	}
	return false
}

// OwnerOnlyKeys returns the single owner-only permissions (the billing
// module is owner-only as a whole). The grant queries take this list.
func OwnerOnlyKeys() []string { return append([]string(nil), ownerOnlyKeys...) }

// BillerDefaults are the biller role's seeded permissions (API_CONTRACT §3).
// The role is editable afterwards; it is never reset.
var BillerDefaults = map[string]bool{
	"dashboard:view": true, "pos:view": true, "pos:create": true, "products:view": true,
	"customers:view": true, "customers:create": true, "inventory:view": true,
	"sales:view": true, "sales:create": true, "sales:discount": true,
	"invoices:view": true, "reports:view": true, "stores:view": true, "settings:view": true,
	"receipts:view": true, "receipts:resend": true,
	"attributes:view": true, // industry P2: checkout shows attribute labels (new shops only)
}

// InSystemRole reports whether a permission belongs to a system role's fixed
// set: owner has everything, admin everything except billing.
func InSystemRole(roleKey string, p Permission) bool {
	switch roleKey {
	case RoleOwner:
		return true
	case RoleAdmin:
		return !OwnerOnly(p)
	case RoleBiller:
		return BillerDefaults[p.Key()]
	}
	return false
}
