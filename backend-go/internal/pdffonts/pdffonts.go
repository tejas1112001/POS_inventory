// Package pdffonts embeds the fonts every generated PDF uses (receipts,
// report exports): Noto Sans Regular and Bold (SIL OFL 1.1, see
// fonts/OFL.txt), which cover ₹ and the other currency symbols.
package pdffonts

import (
	_ "embed" // fonts

	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/core/entity"
	"github.com/johnfercher/maroto/v2/pkg/fontrepository"
)

// Family is the font family name to use in maroto props.
const Family = "noto"

var (
	//go:embed fonts/NotoSans-Regular.ttf
	regular []byte
	//go:embed fonts/NotoSans-Bold.ttf
	bold []byte
)

// Load returns the fonts for maroto's config.WithCustomFonts.
func Load() ([]entity.CustomFont, error) {
	return fontrepository.New().
		AddUTF8FontFromBytes(Family, fontstyle.Normal, regular).
		AddUTF8FontFromBytes(Family, fontstyle.Bold, bold).
		Load()
}
