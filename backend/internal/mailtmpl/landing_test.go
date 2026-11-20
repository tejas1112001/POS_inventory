package mailtmpl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindShop(t *testing.T) {
	r, err := RenderFindShop(FindShop{PlatformName: "POS Test", Shops: []ShopLink{
		{Name: "Demo Shop", URL: "https://demo-shop.pos.test/login"},
		{Name: "Second", URL: "https://second.pos.test/login"},
		{Name: "Bad link", URL: "javascript:alert(1)"},
	}})
	require.NoError(t, err)
	golden(t, "find_shop", r)
	require.NotContains(t, r.HTML, "javascript:")
	require.NotContains(t, r.HTML, "Bad link")

	one, err := RenderFindShop(FindShop{PlatformName: "POS Test", Shops: []ShopLink{{Name: hostile, URL: "https://a.pos.test/login"}}})
	require.NoError(t, err)
	checkSafe(t, one)
	require.Equal(t, "Your shop address on POS Test", one.Subject)
}

func TestContactRequest(t *testing.T) {
	r, err := RenderContactRequest(ContactRequest{PlatformName: "POS Test", ID: 7, Demo: true, Name: "Asha Rao",
		Email: "asha@example.test", Business: "Rao Stores", Phone: "+91 98765 43210",
		Message: "We have 2 stores.\r\nCan we see a demo?", Sent: "27 Oct 2026 10:00 UTC"})
	require.NoError(t, err)
	golden(t, "contact_request", r)
	require.Equal(t, "Demo request from Asha Rao (Rao Stores)", r.Subject)

	h, err := RenderContactRequest(ContactRequest{PlatformName: "POS Test", ID: 8, Name: hostile, Email: "a@b.test",
		Business: hostile, Message: hostile})
	require.NoError(t, err)
	checkSafe(t, h)
	require.Contains(t, h.Subject, "Question from")
}
