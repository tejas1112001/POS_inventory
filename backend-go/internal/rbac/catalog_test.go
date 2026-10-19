package rbac

import "testing"

func TestCatalogUniqueAndBillerSubset(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Catalog {
		if p.Module == "" || p.Action == "" || p.Label == "" {
			t.Fatalf("incomplete permission %+v", p)
		}
		if seen[p.Key()] {
			t.Fatalf("duplicate %s", p.Key())
		}
		seen[p.Key()] = true
	}
	for k := range BillerDefaults {
		if !seen[k] {
			t.Fatalf("biller default %s not in catalog", k)
		}
	}
	for _, p := range Catalog {
		if !InSystemRole(RoleOwner, p) {
			t.Fatal("owner must have every permission")
		}
		if InSystemRole(RoleAdmin, p) == (p.Module == "billing") {
			t.Fatalf("admin billing rule wrong for %s", p.Key())
		}
	}
	if InSystemRole("custom", Catalog[0]) {
		t.Fatal("unknown role key has no fixed set")
	}
}
