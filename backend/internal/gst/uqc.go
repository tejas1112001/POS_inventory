package gst

// UQC is a GST Unique Quantity Code, the unit of the HSN summary in GSTR-1.
type UQC struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// UQCs lists the GST Unique Quantity Codes (the GST portal's list).
var UQCs = []UQC{
	{"BAG", "Bags"}, {"BAL", "Bale"}, {"BDL", "Bundles"}, {"BKL", "Buckles"}, {"BOU", "Billion of units"},
	{"BOX", "Box"}, {"BTL", "Bottles"}, {"BUN", "Bunches"}, {"CAN", "Cans"}, {"CBM", "Cubic metres"},
	{"CCM", "Cubic centimetres"}, {"CMS", "Centimetres"}, {"CTN", "Cartons"}, {"DOZ", "Dozens"}, {"DRM", "Drums"},
	{"GGK", "Great gross"}, {"GMS", "Grammes"}, {"GRS", "Gross"}, {"GYD", "Gross yards"}, {"KGS", "Kilograms"},
	{"KLR", "Kilolitre"}, {"KME", "Kilometre"}, {"LTR", "Litres"}, {"MLT", "Millilitre"}, {"MTR", "Metres"},
	{"MTS", "Metric ton"}, {"NOS", "Numbers"}, {"OTH", "Others"}, {"PAC", "Packs"}, {"PCS", "Pieces"},
	{"PRS", "Pairs"}, {"QTL", "Quintal"}, {"ROL", "Rolls"}, {"SET", "Sets"}, {"SQF", "Square feet"},
	{"SQM", "Square metres"}, {"SQY", "Square yards"}, {"TBS", "Tablets"}, {"TGM", "Ten gross"}, {"THD", "Thousands"},
	{"TON", "Tonnes"}, {"TUB", "Tubes"}, {"UGS", "US gallons"}, {"UNT", "Units"}, {"YDS", "Yards"},
}

var uqcByCode = func() map[string]bool {
	m := make(map[string]bool, len(UQCs))
	for _, u := range UQCs {
		m[u.Code] = true
	}
	return m
}()

// IsUQC reports whether code is a GST Unique Quantity Code.
func IsUQC(code string) bool { return uqcByCode[code] }
