package rbac

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func noGuard(Requirement) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler { return h }
}

func TestSelfCheck(t *testing.T) {
	r := chi.NewRouter()
	rt := NewRouter(r, "/api", noGuard)
	ok := func(http.ResponseWriter, *http.Request) {}
	rt.Handle(http.MethodGet, "/users", Require("users", "view"), ok)
	rt.Handle(http.MethodPost, "/auth/login", Public().ReadOnlyOK(), ok)
	require.NoError(t, rt.SelfCheck())

	r.Get("/sneaky", ok) // registered without a requirement
	err := rt.SelfCheck()
	require.ErrorContains(t, err, "no requirement: GET /api/sneaky")

	r2 := chi.NewRouter()
	rt2 := NewRouter(r2, "/api", noGuard)
	rt2.Handle(http.MethodDelete, "/users/{id}", Require("users", "delete").ReadOnlyOK(), ok)
	require.ErrorContains(t, rt2.SelfCheck(), "read_only exemption not allow-listed: DELETE /api/users/{id}")

	// A module route needs the module guard, and the guard runs on it.
	r3 := chi.NewRouter()
	rt3 := NewRouter(r3, "/api", noGuard)
	rt3.Handle(http.MethodGet, "/attributes", Require("attributes", "view").InModule("product_attributes"), ok)
	require.ErrorContains(t, rt3.SelfCheck(), "module route without a module guard: GET /api/attributes")

	r4 := chi.NewRouter()
	var guarded []string
	rt4 := NewRouter(r4, "/api", noGuard).WithModuleGuard(func(m string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				guarded = append(guarded, m)
				w.WriteHeader(http.StatusForbidden)
			})
		}
	})
	rt4.Handle(http.MethodGet, "/attributes", Require("attributes", "view").InModule("product_attributes"), ok)
	rt4.Handle(http.MethodGet, "/users", Require("users", "view"), ok)
	require.NoError(t, rt4.SelfCheck())
	for _, path := range []string{"/attributes", "/users"} {
		w := httptest.NewRecorder()
		r4.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if path == "/attributes" {
			require.Equal(t, http.StatusForbidden, w.Code)
		} else {
			require.Equal(t, http.StatusOK, w.Code)
		}
	}
	require.Equal(t, []string{"product_attributes"}, guarded)
}

func TestOwnerOnlyKeys(t *testing.T) {
	for _, k := range []string{"sales:credit", "customers:credit_limit", "modules:manage"} {
		require.Contains(t, OwnerOnlyKeys(), k)
	}
	for _, p := range Catalog {
		if p.Key() == "modules:manage" {
			require.True(t, OwnerOnly(p))
			require.False(t, InSystemRole(RoleAdmin, p))
			require.False(t, InSystemRole(RoleBiller, p))
		}
	}
}
