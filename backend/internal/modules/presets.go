package modules

import "strconv"

// AttributePreset is a starting attribute definition a business type seeds
// (industry P1 item 4; docs/INDUSTRY_PHASE1_DESIGN.md §1 presets).
type AttributePreset struct {
	Key, Label, Level, Kind string
	Choices                 []string
	Sort                    int
}

var (
	department = AttributePreset{Key: "department", Label: "Department", Level: "product", Kind: "choice",
		Choices: []string{"Men", "Women", "Boys", "Girls", "Unisex"}, Sort: 10}
	colour = AttributePreset{Key: "colour", Label: "Colour", Level: "variant", Kind: "text", Choices: []string{}, Sort: 60}
)

// footwearSizes are whole and half sizes 1-13 (UK, US, India) and 33-48
// (EU); the product's size system says which.
func footwearSizes() []string {
	out := []string{}
	for n := 1; n <= 13; n++ {
		out = append(out, strconv.Itoa(n))
		if n < 13 {
			out = append(out, strconv.Itoa(n)+".5")
		}
	}
	for n := 33; n <= 48; n++ {
		out = append(out, strconv.Itoa(n))
	}
	return out
}

// AttributePresets are the definitions each business type seeds where
// none with the same key exists. Retail and pharmacy seed none.
var AttributePresets = map[string][]AttributePreset{
	Clothing: {
		department,
		{Key: "collection", Label: "Collection", Level: "product", Kind: "text", Choices: []string{}, Sort: 20},
		{Key: "season", Label: "Season", Level: "product", Kind: "choice", Choices: []string{"Spring/Summer", "Autumn/Winter", "All season"}, Sort: 30},
		{Key: "size", Label: "Size", Level: "variant", Kind: "choice", Choices: []string{"XS", "S", "M", "L", "XL", "XXL", "3XL"}, Sort: 50},
		colour,
	},
	Footwear: {
		department,
		{Key: "size_system", Label: "Size system", Level: "product", Kind: "choice", Choices: []string{"UK", "US", "EU", "India"}, Sort: 40},
		{Key: "size", Label: "Size", Level: "variant", Kind: "choice", Choices: footwearSizes(), Sort: 50},
		colour,
	},
}
