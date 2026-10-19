package rbac

import (
	"net/http"
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
}
