package modules

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryIsConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Registry {
		require.NotEmpty(t, m.Key)
		require.NotEmpty(t, m.Label)
		require.Equal(t, m.Key, m.Feature, "the plan feature and override key is the module key")
		require.False(t, seen[m.Key], "duplicate %s", m.Key)
		seen[m.Key] = true
		for _, d := range m.Depends {
			_, ok := Get(d)
			require.True(t, ok, "%s depends on unknown %s", m.Key, d)
		}
		for _, bt := range m.RecommendedFor {
			require.True(t, ValidType(bt), "%s recommended for unknown type %s", m.Key, bt)
		}
		require.NotEmpty(t, m.Routes, "%s owns no routes", m.Key)
	}
}

func TestRecommendedPresets(t *testing.T) {
	require.Empty(t, Recommended(Retail))
	require.Equal(t, []string{ProductAttributes, BarcodeLabels, StockCounts}, Recommended(Clothing))
	require.Equal(t, []string{ProductAttributes, BarcodeLabels, StockCounts}, Recommended(Footwear))
	require.Equal(t, []string{BarcodeLabels, StockCounts, BatchExpiry}, Recommended(Pharmacy)) // industry P3
	require.Empty(t, Recommended("bakery"))
	require.False(t, ValidType("bakery"))
	require.False(t, ValidType(""))
}

func TestNormalize(t *testing.T) {
	reg := []Module{{Key: "a"}, {Key: "b", Depends: []string{"a"}}, {Key: "c"}}

	got, err := Normalize(reg, []string{"c", "a", "a"})
	require.NoError(t, err)
	require.Equal(t, []string{"a", "c"}, got, "registry order, no duplicates")

	got, err = Normalize(reg, nil)
	require.NoError(t, err)
	require.Equal(t, []string{}, got)

	_, err = Normalize(reg, []string{"b"})
	require.ErrorContains(t, err, `module "b" needs "a"`)

	_, err = Normalize(reg, []string{"a", "zzz"})
	require.ErrorContains(t, err, `unknown module "zzz"`)

	got, err = Normalize(reg, []string{"b", "a"})
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, got)
}

func TestCheckRoutes(t *testing.T) {
	reg := []Module{{Key: "m1", Routes: []string{"/api/things", "/api/products/{id}/matrix"}}}
	ok := []Route{
		{Key: "GET /api/things", Module: "m1"},
		{Key: "PUT /api/things/{id}", Module: "m1"},
		{Key: "POST /api/products/{id}/matrix", Module: "m1"},
		{Key: "GET /api/thingsx"},       // a different path that only shares a prefix
		{Key: "GET /api/products/{id}"}, // the parent is not owned
	}
	require.NoError(t, CheckRoutes(reg, ok))

	err := CheckRoutes(reg, append(ok, Route{Key: "DELETE /api/things/{id}"}))
	require.ErrorContains(t, err, `DELETE /api/things/{id} is under module m1 but registered with ""`)

	err = CheckRoutes(reg, []Route{{Key: "GET /api/other", Module: "nope"}})
	require.ErrorContains(t, err, `names unknown module "nope"`)

	// The real registry against the real owner paths.
	require.Equal(t, ProductAttributes, RouteOwner(Registry, "/api/attributes/{id}"))
	require.Equal(t, ProductAttributes, RouteOwner(Registry, "/api/products/{id}/variants/matrix"))
	require.Equal(t, BarcodeLabels, RouteOwner(Registry, "/api/labels/pdf"))
	require.Equal(t, StockCounts, RouteOwner(Registry, "/api/stock-counts/{id}/complete"))
	require.Equal(t, "", RouteOwner(Registry, "/api/settings/labels"), "label layouts are settings, not the module (A11)")
}

func TestSummaryKeepsOnlyEntitledModulesUsable(t *testing.T) {
	s := SummaryOf(Shop{BusinessType: Clothing, Modules: []string{ProductAttributes, StockCounts}}, []string{ProductAttributes, BarcodeLabels})
	require.Equal(t, []string{ProductAttributes}, s.Modules)
	require.Equal(t, []string{ProductAttributes, StockCounts}, s.EnabledModules)
	require.Equal(t, Clothing, s.BusinessType)
}
