package main

// Sample data. Brand and company names are made up. Prices include GST
// (taxType Inclusive) and stay at or below the MRP.

func retailData() shopData {
	st, sn, bv, dy, pc, hh := "Staples", "Snacks", "Beverages", "Dairy & Bakery", "Personal Care", "Household"
	kg, pcs := "Kilogram", "Piece"
	p := func(name, cat, brand, hsn string, tax, price, mrp float64) product {
		return product{name: name, cat: cat, brand: brand, unit: pcs, hsn: hsn, tax: tax, price: price, mrp: mrp}
	}
	loose := func(name, cat, hsn string, tax, price float64) product {
		return product{name: name, cat: cat, brand: "", unit: kg, hsn: hsn, tax: tax, price: price, loose: true}
	}
	return shopData{
		categories: []string{st, sn, bv, dy, pc, hh},
		brands:     []string{"Annapurna Foods", "Sunrise", "FreshFarm", "CleanHome", "Glow Naturals"},
		units:      [][2]string{{kg, "kg"}, {pcs, "pc"}},
		suppliers: []party{
			{"Shree Ganesh Wholesale", "9822012345", "Shop 14, APMC Market", "Pune"},
			{"Metro FMCG Distributors", "9890123456", "Plot 7, MIDC Bhosari", "Pune"},
			{"Sai Dairy Supplies", "9765432109", "Near Bus Stand, Hadapsar", "Pune"},
		},
		customers: commonCustomers(),
		alert:     10,
		sales:     36,
		products: []product{
			loose("Basmati Rice (loose)", st, "1006", 0, 96),
			p("Sona Masoori Rice 5kg", st, "Annapurna Foods", "1006", 5, 420, 450),
			p("Wheat Atta 5kg", st, "Annapurna Foods", "1101", 5, 245, 260),
			p("Toor Dal 1kg", st, "Annapurna Foods", "0713", 5, 165, 175),
			p("Moong Dal 1kg", st, "Annapurna Foods", "0713", 5, 155, 165),
			loose("Chana Dal (loose)", st, "0713", 0, 92),
			loose("Sugar (loose)", st, "1701", 5, 46),
			p("Iodised Salt 1kg", st, "Sunrise", "2501", 0, 25, 28),
			p("Sunflower Oil 1L", st, "Sunrise", "1512", 5, 155, 170),
			p("Groundnut Oil 1L", st, "Sunrise", "1508", 5, 210, 230),
			p("Thick Poha 500g", st, "Annapurna Foods", "1904", 5, 40, 45),
			p("Potato Chips 52g", sn, "Sunrise", "2005", 12, 20, 20),
			p("Salted Peanuts 200g", sn, "Sunrise", "2008", 12, 55, 60),
			p("Glucose Biscuits 200g", sn, "Sunrise", "1905", 18, 25, 25),
			p("Cream Biscuits 150g", sn, "Sunrise", "1905", 18, 30, 30),
			p("Namkeen Mixture 400g", sn, "Annapurna Foods", "2106", 12, 95, 105),
			p("Instant Noodles 70g", sn, "Sunrise", "1902", 12, 14, 14),
			p("Dark Chocolate 80g", sn, "Sunrise", "1806", 18, 99, 110),
			p("Green Tea 25 bags", bv, "Glow Naturals", "0902", 5, 160, 175),
			p("Instant Coffee 100g", bv, "Sunrise", "2101", 18, 290, 310),
			p("Mango Drink 1L", bv, "FreshFarm", "2202", 12, 99, 110),
			p("Lemon Soda 750ml", bv, "FreshFarm", "2202", 28, 40, 45),
			p("Packaged Water 1L", bv, "FreshFarm", "2201", 18, 20, 20),
			p("Toned Milk 500ml", dy, "FreshFarm", "0401", 0, 27, 27),
			p("Fresh Curd 400g", dy, "FreshFarm", "0403", 5, 35, 38),
			p("Salted Butter 100g", dy, "FreshFarm", "0405", 12, 56, 58),
			p("Paneer 200g", dy, "FreshFarm", "0406", 5, 90, 95),
			p("Brown Bread 400g", dy, "FreshFarm", "1905", 0, 40, 45),
			{name: "Eggs (tray of 6)", cat: dy, unit: pcs, hsn: "0407", tax: 0, price: 42},
			p("Herbal Shampoo 180ml", pc, "Glow Naturals", "3305", 18, 145, 160),
			p("Coconut Hair Oil 200ml", pc, "Glow Naturals", "3305", 18, 110, 120),
			p("Neem Face Wash 100ml", pc, "Glow Naturals", "3304", 18, 120, 135),
			p("Liquid Hand Wash 200ml", pc, "Glow Naturals", "3401", 18, 89, 99),
			p("Toothbrush Soft", pc, "Glow Naturals", "9603", 18, 35, 40),
			p("Dishwash Bar 200g", hh, "CleanHome", "3402", 18, 30, 32),
			p("Detergent Powder 1kg", hh, "CleanHome", "3402", 18, 115, 125),
			p("Floor Cleaner 1L", hh, "CleanHome", "3402", 18, 185, 199),
			p("Toilet Cleaner 500ml", hh, "CleanHome", "3402", 18, 89, 95),
			{name: "Garbage Bags (30)", cat: hh, brand: "CleanHome", unit: pcs, hsn: "3923", tax: 18, price: 99, mrp: 110, buy: 8},
			{name: "Safety Matches (10 boxes)", cat: hh, unit: pcs, hsn: "3605", tax: 12, price: 20, mrp: 20, buy: 9},
			{name: "Agarbatti Sandal 100g", cat: hh, brand: "Glow Naturals", unit: pcs, hsn: "3307", tax: 5, price: 50, mrp: 55, buy: 7},
		},
	}
}

func clothingData() shopData {
	men, women, kids, acc := "Men's Wear", "Women's Wear", "Kids' Wear", "Accessories"
	a := func(dept, season string) map[string]string {
		return map[string]string{"department": dept, "season": season, "collection": "Winter 2026"}
	}
	return shopData{
		categories: []string{men, women, kids, acc},
		brands:     []string{"Urban Thread", "Desi Loom", "Little Steps"},
		suppliers: []party{
			{"Tirupur Knit Exports", "9443012345", "12 Kumaran Road", "Tirupur"},
			{"Surat Textile House", "9825012345", "Ring Road Textile Market", "Surat"},
			{"Delhi Fashion Wholesale", "9811012345", "Gandhi Nagar Market", "Delhi"},
		},
		customers: commonCustomers(),
		alert:     2,
		sales:     28,
		products: []product{
			{name: "Men's Formal Shirt", cat: men, brand: "Urban Thread", hsn: "6205", tax: 5, price: 899, mrp: 1099, article: "MFS-01",
				sizes: []string{"S", "M", "L", "XL"}, colours: []string{"White", "Sky Blue"}, attrs: a("Men", "All season")},
			{name: "Men's Slim Jeans", cat: men, brand: "Urban Thread", hsn: "6203", tax: 5, price: 1499, mrp: 1799, article: "MSJ-01",
				sizes: []string{"M", "L", "XL"}, colours: []string{"Dark Blue", "Black"}, attrs: a("Men", "All season")},
			{name: "Men's Cotton Kurta", cat: men, brand: "Desi Loom", hsn: "6211", tax: 5, price: 1199, mrp: 1399, article: "MCK-01",
				sizes: []string{"M", "L", "XL"}, colours: []string{"Beige", "Maroon"}, attrs: a("Men", "All season")},
			{name: "Men's Chinos", cat: men, brand: "Urban Thread", hsn: "6203", tax: 5, price: 1299, mrp: 1499, article: "MCH-01",
				sizes: []string{"M", "L", "XL"}, colours: []string{"Khaki", "Navy"}, attrs: a("Men", "All season")},
			{name: "Men's Wool Sweater", cat: men, brand: "Urban Thread", hsn: "6110", tax: 18, price: 2799, mrp: 2999, article: "MWS-01",
				sizes: []string{"M", "L", "XL"}, colours: []string{"Grey"}, attrs: a("Men", "Autumn/Winter")},
			{name: "Women's Printed Kurti", cat: women, brand: "Desi Loom", hsn: "6211", tax: 5, price: 799, mrp: 999, article: "WPK-01",
				sizes: []string{"S", "M", "L", "XL"}, colours: []string{"Pink", "Green"}, attrs: a("Women", "All season")},
			{name: "Women's Palazzo", cat: women, brand: "Desi Loom", hsn: "6204", tax: 5, price: 599, mrp: 699, article: "WPZ-01",
				sizes: []string{"S", "M", "L"}, colours: []string{"Black", "White"}, attrs: a("Women", "Spring/Summer")},
			{name: "Women's Denim Jacket", cat: women, brand: "Urban Thread", hsn: "6202", tax: 18, price: 2699, mrp: 2999, article: "WDJ-01",
				sizes: []string{"S", "M", "L"}, colours: []string{"Blue"}, attrs: a("Women", "Autumn/Winter")},
			{name: "Cotton Saree", cat: women, brand: "Desi Loom", hsn: "5208", tax: 5, price: 1899, mrp: 2199, article: "CSR-01",
				colours: []string{"Red", "Yellow", "Teal"}, attrs: a("Women", "All season")},
			{name: "Women's Leggings", cat: women, brand: "Desi Loom", hsn: "6104", tax: 5, price: 349, mrp: 399, article: "WLG-01",
				sizes: []string{"S", "M", "L", "XL"}, colours: []string{"Black", "Navy"}, attrs: a("Women", "All season")},
			{name: "Kids' T-shirt", cat: kids, brand: "Little Steps", hsn: "6109", tax: 5, price: 299, mrp: 349, article: "KTS-01",
				sizes: []string{"XS", "S", "M"}, colours: []string{"Yellow", "Red"}, attrs: a("Boys", "Spring/Summer")},
			{name: "Girls' Party Frock", cat: kids, brand: "Little Steps", hsn: "6209", tax: 5, price: 649, mrp: 799, article: "GPF-01",
				sizes: []string{"XS", "S"}, colours: []string{"Pink", "Purple"}, attrs: a("Girls", "All season")},
			{name: "Boys' Shorts", cat: kids, brand: "Little Steps", hsn: "6203", tax: 5, price: 349, mrp: 399, article: "BSH-01",
				sizes: []string{"XS", "S", "M"}, colours: []string{"Grey"}, attrs: a("Boys", "Spring/Summer")},
			{name: "Leather Belt", cat: acc, brand: "Urban Thread", hsn: "4203", tax: 18, price: 499, mrp: 599, buy: 10},
			{name: "Cotton Socks (3 pairs)", cat: acc, brand: "Urban Thread", hsn: "6115", tax: 5, price: 199, mrp: 249, buy: 20},
			{name: "Woollen Cap", cat: acc, brand: "Little Steps", hsn: "6505", tax: 5, price: 299, mrp: 349, buy: 3},
			{name: "Silk Scarf", cat: acc, brand: "Desi Loom", hsn: "6214", tax: 5, price: 499, mrp: 599, article: "SSC-01",
				colours: []string{"Red", "Blue"}, attrs: map[string]string{"department": "Women", "season": "All season"}},
		},
	}
}

func pharmacyData() shopData {
	pain, cold, vit, dia, stom, aid, care := "Pain & Fever", "Cold & Cough", "Vitamins & Supplements", "Diabetes Care",
		"Stomach Care", "First Aid", "Personal & Baby Care"
	strip, btl, tube, box, pcs := "Strip of 10", "Bottle", "Tube", "Box", "Piece"
	m := func(name, cat, brand, unit, hsn string, tax, price, mrp float64) product {
		return product{name: name, cat: cat, brand: brand, unit: unit, hsn: hsn, tax: tax, price: price, mrp: mrp}
	}
	sundry := func(name, cat, brand, unit, hsn string, tax, price, mrp float64) product {
		p := m(name, cat, brand, unit, hsn, tax, price, mrp)
		p.untracked = true
		return p
	}
	return shopData{
		categories: []string{pain, cold, vit, dia, stom, aid, care},
		brands:     []string{"Medline Labs", "Curewell Pharma", "Healthaid", "VitaPlus"},
		units:      [][2]string{{strip, "st10"}, {btl, "btl"}, {tube, "tube"}, {box, "box"}, {pcs, "pc"}},
		suppliers: []party{
			{"City Pharma Distributors", "9850012345", "Shop 3, Medical Market, Budhwar Peth", "Pune"},
			{"Lifeline Medical Agencies", "9881012345", "22 Station Road", "Pune"},
			{"Wellness Surgicals", "9762012345", "Plot 5, Kondhwa Industrial Area", "Pune"},
		},
		customers: commonCustomers(),
		alert:     5,
		sales:     34,
		products: []product{
			m("Paracetamol 650mg Tablet", pain, "Medline Labs", strip, "3004", 5, 30, 32),
			m("Ibuprofen 400mg Tablet", pain, "Curewell Pharma", strip, "3004", 5, 25, 28),
			m("Aceclofenac + Paracetamol Tablet", pain, "Curewell Pharma", strip, "3004", 5, 65, 72),
			m("Diclofenac Gel 30g", pain, "Medline Labs", tube, "3004", 5, 110, 125),
			m("Pain Relief Spray 55g", pain, "Healthaid", btl, "3004", 5, 180, 199),
			m("Cetirizine 10mg Tablet", cold, "Medline Labs", strip, "3004", 5, 18, 20),
			m("Levocetirizine 5mg Tablet", cold, "Curewell Pharma", strip, "3004", 5, 45, 52),
			m("Dry Cough Syrup 100ml", cold, "Medline Labs", btl, "3004", 5, 98, 110),
			m("Ambroxol Syrup 100ml", cold, "Curewell Pharma", btl, "3004", 5, 85, 95),
			m("Steam Inhalation Capsules", cold, "Healthaid", strip, "3004", 5, 60, 68),
			m("Throat Lozenges (8)", cold, "Healthaid", strip, "3004", 5, 35, 40),
			m("Vitamin D3 60000 IU Capsule (4)", vit, "VitaPlus", strip, "3004", 5, 110, 125),
			m("B-Complex Capsule", vit, "VitaPlus", strip, "3004", 5, 35, 40),
			m("Calcium + Vitamin D3 Tablet (15)", vit, "VitaPlus", strip, "3004", 5, 120, 135),
			m("Multivitamin Tablet (30)", vit, "VitaPlus", btl, "3004", 5, 299, 330),
			m("Zinc 50mg Tablet", vit, "VitaPlus", strip, "3004", 5, 85, 95),
			m("Iron + Folic Acid Tablet", vit, "Medline Labs", strip, "3004", 5, 40, 45),
			m("Metformin 500mg Tablet", dia, "Curewell Pharma", strip, "3004", 5, 22, 25),
			m("Glimepiride 1mg Tablet", dia, "Curewell Pharma", strip, "3004", 5, 55, 62),
			m("Glucose Test Strips (50)", dia, "Healthaid", box, "3822", 5, 899, 999),
			sundry("Lancets (100)", dia, "Healthaid", box, "9018", 5, 199, 220),
			m("Pantoprazole 40mg Tablet", stom, "Medline Labs", strip, "3004", 5, 95, 108),
			m("ORS Orange Sachet 21g", stom, "Healthaid", pcs, "3004", 5, 20, 22),
			m("Antacid Syrup 170ml", stom, "Medline Labs", btl, "3004", 5, 115, 128),
			m("Loperamide 2mg Tablet", stom, "Curewell Pharma", strip, "3004", 5, 25, 28),
			m("Antiseptic Liquid 100ml", aid, "Healthaid", btl, "3004", 5, 65, 72),
			sundry("Adhesive Bandages (20)", aid, "Healthaid", box, "3005", 5, 45, 50),
			sundry("Absorbent Cotton Roll 100g", aid, "Healthaid", pcs, "3005", 5, 55, 60),
			sundry("Crepe Bandage 10cm", aid, "Healthaid", pcs, "3005", 5, 120, 135),
			sundry("Digital Thermometer", aid, "Healthaid", pcs, "9025", 18, 199, 250),
			sundry("Hand Sanitizer 200ml", care, "Healthaid", btl, "3808", 18, 99, 110),
			m("Diaper Rash Cream 50g", care, "Medline Labs", tube, "3004", 5, 145, 160),
			sundry("Surgical Masks (10)", care, "Healthaid", box, "6307", 5, 50, 60),
		},
	}
}

// commonCustomers are regulars; phone numbers are made up.
func commonCustomers() []party {
	return []party{
		{"Aarti Deshpande", "9800000101", "Flat 4, Shanti Apartments, Kothrud", "Pune"},
		{"Rahul Patil", "9800000102", "12 Lane 3, Aundh", "Pune"},
		{"Sneha Kulkarni", "9800000103", "B-203 Green Park, Baner", "Pune"},
		{"Imran Shaikh", "9800000104", "45 Camp Road", "Pune"},
		{"Priya Nair", "9800000105", "7 Koregaon Park", "Pune"},
		{"Vikram Joshi", "9800000106", "21 Sadashiv Peth", "Pune"},
		{"Meena Iyer", "9800000107", "C-11 Viman Nagar", "Pune"},
		{"Arjun Mehta", "9800000108", "3 Model Colony", "Pune"},
		{"Fatima Khan", "9800000109", "18 Wanowrie", "Pune"},
		{"Sanjay Gupta", "9800000110", "9 Deccan Gymkhana", "Pune"},
	}
}
