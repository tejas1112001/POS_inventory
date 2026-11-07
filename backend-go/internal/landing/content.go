package landing

// Page copy. Every statement here describes something the product does
// today; keep it that way (no customer counts, quotes or claims).

type feature struct {
	Icon, Title, Text string
}

// mainFeatures is the "What it does" grid.
var mainFeatures = []feature{
	{"cart", "POS checkout", "Find products by name or SKU at the counter, take payment and complete the sale."},
	{"boxes", "Stock across stores", "Stock is kept per store. Adjust counts with a reason and see the history of every change."},
	{"mail", "Receipts by email", "Send the receipt to the customer's email with a PDF copy attached, or resend it later."},
	{"undo", "Sales returns", "Take a return against the lines of the original sale. Returned items go back into stock."},
	{"truck", "Purchases", "Record purchases from suppliers, track their status and payment, and receive the stock."},
	{"chart", "Reports and exports", "Sales and purchase reports with filters, exported to Excel or PDF."},
	{"store", "An online shop per store", "Each store can publish a public product page on the web that customers can browse."},
	{"users", "Staff roles", "Use the built-in Owner, Admin and Biller roles or create your own, so each person can do only what they need."},
	{"layers", "Many businesses, one platform", "A Super Admin creates businesses, sets their plans and manages subscriptions. Each business keeps its data separate."},
}

// moreFeatures is the smaller "Also included" grid.
var moreFeatures = []feature{
	{"globe", "Your currency and time zone", "Each business sets its currency, number format and time zone."},
	{"upload", "Product import", "Bring in your product list from a CSV file."},
	{"key", "Invitations and password resets", "Staff get an email to set their own password. Nobody shares passwords."},
	{"shield", "Separate data per business", "Every business has its own database schema, so one shop never sees another's data."},
	{"link", "Your own shop address", "Each business gets its own address, like your-shop.{{domain}}, for staff and customers."},
	{"lock", "Two-step sign-in for Super Admins", "The platform console needs a password and an authenticator code."},
}

type step struct {
	Title, Text string
}

var steps = []step{
	{"Request a demo", "Tell us about your business and how many stores you run. We reply by email."},
	{"We set up your shop", "The platform team creates your business. You get an email with your shop address and a link to set your password."},
	{"Start selling", "Add your products and stores, invite your staff and open the POS."},
}

type faq struct {
	Q, A string
}

// faqs is shown on the page and in the FAQPage JSON-LD. {{domain}} is
// replaced by BASE_DOMAIN.
var faqs = []faq{
	{"Can I sign up on my own?", "Not yet. Every business is set up by the platform team. Send a demo request and we will get in touch."},
	{"Where do my staff sign in?", "At your shop's own address, for example your-shop.{{domain}}/login. Each person signs in with their own email and password."},
	{"I forgot my shop's address. What now?", "Use Find my shop on this page. If the email you enter is the owner or billing email of a shop, we send the shop's address to that email."},
	{"Can I run more than one store?", "Yes. Stock, sales and the online shop are kept per store, within the limits of your plan."},
	{"Do customers need an account to see my online shop?", "No. The online shop is public and shows only the products you choose to publish, without costs or stock counts."},
	{"How is pricing shown?", "Plans are priced in Indian rupees (INR). The pricing section on this page always shows the current public plans."},
}
