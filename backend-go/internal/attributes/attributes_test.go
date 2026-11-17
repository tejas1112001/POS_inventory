package attributes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func testDefs() Defs {
	return Defs{
		"size":        {Key: "size", Label: "Size", Level: LevelVariant, Kind: KindChoice, Choices: []string{"S", "M", "L", "8", "8.5"}},
		"colour":      {Key: "colour", Label: "Colour", Level: LevelVariant, Kind: KindText},
		"season":      {Key: "season", Label: "Season", Level: LevelProduct, Kind: KindChoice, Choices: []string{"Spring/Summer", "Autumn/Winter"}},
		"size_system": {Key: "size_system", Label: "Size system", Level: LevelProduct, Kind: KindChoice, Choices: []string{"UK", "EU"}},
	}
}

func TestCheckLeavesPlainProductsAlone(t *testing.T) {
	free := []map[string]any{{"Material": "Cotton"}, {"anything": 1}}
	c, f := testDefs().Check(nil, nil, free)
	require.Empty(t, f)
	require.False(t, c.Uses())
	require.Equal(t, free, c.Variants, "free attributes kept as sent")
}

func TestCheckValidatesDimensions(t *testing.T) {
	d := testDefs()
	c, f := d.Check(map[string]any{"season": "spring/summer"}, []string{"Size", "colour"},
		[]map[string]any{{"size": "m", "colour": " Blue "}, {"SIZE": 8.5, "colour": "Red"}})
	require.Empty(t, f)
	require.Equal(t, []string{"size", "colour"}, c.Dims)
	require.Equal(t, "Spring/Summer", c.Attributes["season"], "choice spelled as defined")
	require.Equal(t, map[string]any{"size": "M", "colour": "Blue"}, c.Variants[0])
	require.Equal(t, map[string]any{"size": "8.5", "colour": "Red"}, c.Variants[1], "numbers and key case accepted")

	cases := []struct {
		name     string
		attrs    map[string]any
		dims     []string
		variants []map[string]any
		field    string
	}{
		{"unknown dimension", nil, []string{"fit"}, []map[string]any{{}}, "variantDims"},
		{"product-level as dimension", nil, []string{"season"}, []map[string]any{{}}, "variantDims"},
		{"dimension twice", nil, []string{"size", "size"}, []map[string]any{{}}, "variantDims"},
		{"too many dimensions", nil, []string{"size", "colour", "a", "b", "c"}, nil, "variantDims"},
		{"variant attribute at product level", map[string]any{"size": "M"}, nil, nil, "attributes.size"},
		{"unknown product attribute", map[string]any{"fit": "slim"}, nil, nil, "attributes.fit"},
		{"value not a choice", map[string]any{"season": "Monsoon"}, nil, nil, "attributes.season"},
		{"missing dimension", nil, []string{"size", "colour"}, []map[string]any{{"size": "M"}}, "variants[0].attributes.colour"},
		{"value not in choices", nil, []string{"size"}, []map[string]any{{"size": "XXXL"}}, "variants[0].attributes.size"},
		{"extra key", nil, []string{"size"}, []map[string]any{{"size": "M", "fit": "slim"}}, "variants[0].attributes.fit"},
		{"duplicate combination", nil, []string{"size", "colour"}, []map[string]any{{"size": "M", "colour": "Blue"}, {"size": "m", "colour": "blue"}}, "variants[1].attributes"},
		{"empty value", nil, []string{"colour"}, []map[string]any{{"colour": " "}}, "variants[0].attributes.colour"},
		{"object value", nil, []string{"colour"}, []map[string]any{{"colour": map[string]any{"x": 1}}}, "variants[0].attributes.colour"},
	}
	for _, tc := range cases {
		_, f := d.Check(tc.attrs, tc.dims, tc.variants)
		require.Contains(t, f, tc.field, tc.name)
	}
	_, f = d.Check(map[string]any{"season": ""}, nil, nil)
	require.Empty(t, f, "an empty product value clears it")
}

func TestLabel(t *testing.T) {
	require.Equal(t, "M / Blue", Label([]string{"size", "colour"}, nil, map[string]any{"size": "M", "colour": "Blue"}))
	require.Equal(t, "UK 8 / Black", Label([]string{"size", "colour"}, map[string]string{"size_system": "UK"}, map[string]any{"size": "8", "colour": "Black"}))
	require.Equal(t, "", Label(nil, nil, map[string]any{"size": "M"}), "no dimensions: no label")
	require.Equal(t, "Blue", Label([]string{"size", "colour"}, nil, map[string]any{"colour": "Blue"}))
}

func TestSKUToken(t *testing.T) {
	require.Equal(t, "NAVYBLUE", SKUToken(" Navy blue "))
	require.Equal(t, "8.5", SKUToken("8.5"))
	require.Equal(t, "SPRING/SUMMER", SKUToken("Spring/Summer"))
	require.Equal(t, "", SKUToken("€ é"))
}

func TestCleanDef(t *testing.T) {
	d, err := cleanDef(defIn{Label: "Size System", Level: "product", Kind: "choice", Choices: []string{" UK ", "", "EU"}}, true)
	require.NoError(t, err)
	require.Equal(t, "size_system", d.Key, "key from the label")
	require.Equal(t, []string{"UK", "EU"}, d.Choices)
	_, err = cleanDef(defIn{Label: "Size", Key: "9size", Level: "variant", Kind: "choice"}, true)
	require.Error(t, err)
	_, err = cleanDef(defIn{Label: "Size", Level: "both", Kind: "list"}, true)
	require.Error(t, err)
	_, err = cleanDef(defIn{Label: "Size", Level: "variant", Kind: "choice", Choices: []string{"M", "m"}}, true)
	require.Error(t, err, "a choice listed twice")
}
