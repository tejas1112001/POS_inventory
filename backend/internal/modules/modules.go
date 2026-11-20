// Package modules is the industry module registry (docs/INDUSTRY_PHASE1_DESIGN.md
// item 1). A shop has a business type (a preset that recommends modules and
// seeds starting data, never a block) and a set of switched-on modules.
// Screens and APIs check modules, never the type, so mixed shops work.
//
// Three separate checks, all on the server:
//  1. Entitled: the plan does not turn the module off and no override does
//     (plans.HasModule; on by default, E1).
//  2. Enabled: listed in settings.modules.
//  3. Permitted: RBAC, as for every route.
//
// Turning a module off hides its screens and refuses its writes; its data
// is never deleted.
package modules

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Business types (settings.business_type, platform.tenants.business_type).
const (
	Retail   = "retail"
	Clothing = "clothing"
	Footwear = "footwear"
	Pharmacy = "pharmacy"
)

// BusinessTypes lists the types in display order.
var BusinessTypes = []BusinessType{
	{Retail, "Retail", "General store: products with optional free-form variants."},
	{Clothing, "Clothing", "Sizes and colours, size-colour matrix, barcode labels, stock counts."},
	{Footwear, "Footwear", "Size systems (UK / US / EU / India), sizes and colours, labels, stock counts."},
	{Pharmacy, "Pharmacy", "Batches and expiry (earliest expiry first), barcode labels and stock counts."},
}

// BusinessType describes one preset.
type BusinessType struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// TypeChoices lists the type keys for an error message: "retail,
// clothing, footwear or pharmacy".
func TypeChoices() string {
	keys := make([]string, len(BusinessTypes))
	for i, b := range BusinessTypes {
		keys[i] = b.Key
	}
	if len(keys) < 2 {
		return strings.Join(keys, "")
	}
	return strings.Join(keys[:len(keys)-1], ", ") + " or " + keys[len(keys)-1]
}

// ValidType reports whether s is a known business type. The database only
// checks the format (ARCHITECTURE_REVIEW M1): a new type is added here.
func ValidType(s string) bool {
	for _, b := range BusinessTypes {
		if b.Key == s {
			return true
		}
	}
	return false
}

// Phase 1 module keys. The plan feature and override key is the same string.
const (
	ProductAttributes = "product_attributes"
	BarcodeLabels     = "barcode_labels"
	StockCounts       = "stock_counts"
	// BatchExpiry is industry Phase 3 (docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md
	// item 1). inventory.ModuleBatches repeats the key.
	BatchExpiry = "batch_expiry"
)

// Module is one switchable capability.
type Module struct {
	Key            string   `json:"key"`
	Label          string   `json:"label"`
	Description    string   `json:"description"`
	Depends        []string `json:"dependencies"`
	Feature        string   `json:"-"` // plan feature / override key
	Perms          []string `json:"permissions"`
	RecommendedFor []string `json:"recommendedFor"`
	// Routes are the API paths the module owns ("/api/attributes" covers
	// the path and everything below it). Every route under them must be
	// registered with InModule(Key) (CheckRoutes).
	Routes []string `json:"-"`
}

// Registry is the module list, in display order. A new module is added
// here only: the database checks the key's format, not the list
// (ARCHITECTURE_REVIEW M1), and the console override key follows.
var Registry = []Module{
	{
		Key: ProductAttributes, Label: "Sizes, colours and attributes",
		Description:    "Define attributes such as size, colour and season once, and build every size-colour variant of a product in one step.",
		Feature:        ProductAttributes,
		Perms:          []string{"attributes:view", "attributes:create", "attributes:edit", "attributes:delete"},
		RecommendedFor: []string{Clothing, Footwear},
		Routes:         []string{"/api/attributes", "/api/products/{id}/variants/matrix"},
	},
	{
		Key: BarcodeLabels, Label: "Barcode labels",
		Description:    "Generate in-store barcodes for variants without one and print price labels on a label printer or A4 sheets.",
		Feature:        BarcodeLabels,
		Perms:          []string{"labels:print"},
		RecommendedFor: []string{Clothing, Footwear, Pharmacy},
		Routes:         []string{"/api/barcodes", "/api/labels"},
	},
	{
		Key: StockCounts, Label: "Stock counts",
		Description:    "Count a store's stock by scanning, typing or uploading a sheet, review the differences and post them.",
		Feature:        StockCounts,
		Perms:          []string{"stock_counts:view", "stock_counts:create", "stock_counts:complete"},
		RecommendedFor: []string{Clothing, Footwear, Pharmacy},
		Routes:         []string{"/api/stock-counts"},
	},
	{
		Key: BatchExpiry, Label: "Batches and expiry",
		Description:    "Keep stock per batch with its expiry date, sell the earliest expiry first and stop expired stock being sold.",
		Feature:        BatchExpiry,
		Perms:          []string{"batches:view", "batches:manage"},
		RecommendedFor: []string{Pharmacy},
		Routes:         []string{"/api/batches"},
	},
}

// Get returns a module by key.
func Get(key string) (Module, bool) {
	for _, m := range Registry {
		if m.Key == key {
			return m, true
		}
	}
	return Module{}, false
}

// Keys returns every module key in registry order.
func Keys() []string {
	out := make([]string, 0, len(Registry))
	for _, m := range Registry {
		out = append(out, m.Key)
	}
	return out
}

// Recommended returns the modules a business type switches on, in
// registry order (retail: none).
func Recommended(businessType string) []string {
	out := []string{}
	for _, m := range Registry {
		if slices.Contains(m.RecommendedFor, businessType) {
			out = append(out, m.Key)
		}
	}
	return out
}

// Normalize checks a module list against a registry: every key known, no
// duplicates, every dependency present. It returns the list in registry
// order.
func Normalize(reg []Module, keys []string) ([]string, error) {
	set := map[string]bool{}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		found := false
		for _, m := range reg {
			if m.Key == k {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown module %q", k)
		}
		set[k] = true
	}
	out := []string{}
	for _, m := range reg {
		if !set[m.Key] {
			continue
		}
		for _, d := range m.Depends {
			if !set[d] {
				return nil, fmt.Errorf("module %q needs %q", m.Key, d)
			}
		}
		out = append(out, m.Key)
	}
	return out, nil
}

// RouteOwner returns the module that owns an API path (a route pattern as
// registered, e.g. "/api/attributes/{id}"), or "" when none does.
func RouteOwner(reg []Module, path string) string {
	for _, m := range reg {
		for _, p := range m.Routes {
			if path == p || strings.HasPrefix(path, p+"/") {
				return m.Key
			}
		}
	}
	return ""
}

// Route is a declared tenant route and the module it is registered in.
type Route struct {
	Key    string // "METHOD /api/path"
	Module string
}

// CheckRoutes fails when a route under a module's paths is not registered
// in that module, or a route names a module that does not own it. Run at
// startup on every declared route (a missed module check is the main risk).
func CheckRoutes(reg []Module, routes []Route) error {
	var problems []string
	for _, r := range routes {
		_, path, _ := strings.Cut(r.Key, " ")
		owner := RouteOwner(reg, path)
		switch {
		case owner != "" && r.Module != owner:
			problems = append(problems, fmt.Sprintf("%s is under module %s but registered with %q", r.Key, owner, r.Module))
		case r.Module != "" && owner == "":
			if _, ok := getIn(reg, r.Module); !ok {
				problems = append(problems, fmt.Sprintf("%s names unknown module %q", r.Key, r.Module))
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("modules: route check failed:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

func getIn(reg []Module, key string) (Module, bool) {
	for _, m := range reg {
		if m.Key == key {
			return m, true
		}
	}
	return Module{}, false
}
