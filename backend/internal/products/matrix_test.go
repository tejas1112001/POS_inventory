package products

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCartesian(t *testing.T) {
	require.Equal(t, [][]string{{"S", "Red"}, {"S", "Blue"}, {"M", "Red"}, {"M", "Blue"}},
		cartesian([][]string{{"S", "M"}, {"Red", "Blue"}}))
	require.Len(t, cartesian([][]string{{"a"}, {"b", "c"}, {"d", "e", "f"}}), 6)
}

func TestRenderSKU(t *testing.T) {
	art := "tsh-01"
	sku, msg := renderSKU("{article}-{size}-{colour}", 7, &art, map[string]string{"size": "M", "colour": "Navy blue"})
	require.Empty(t, msg)
	require.Equal(t, "TSH-01-M-NAVYBLUE", sku)
	sku, msg = renderSKU("P{id}-{size_system}{size}", 7, nil, map[string]string{"size": "8", "size_system": "UK"})
	require.Empty(t, msg)
	require.Equal(t, "P7-UK8", sku, "longer keys replaced first")
	_, msg = renderSKU("{article}-{fit}", 7, &art, map[string]string{"size": "M"})
	require.Contains(t, msg, "{fit}")
	_, msg = renderSKU("{size}", 7, nil, map[string]string{"size": "€"})
	require.Contains(t, msg, "empty SKU")
}
