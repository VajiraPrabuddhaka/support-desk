package main

import "fmt"

// The company in this sample is GeoGrid, which sells a geocoding API. Its
// customers subscribe to the Growth plan (199.00 USD a month, paid in
// advance, plus usage) and can buy add-ons.

const (
	metricCode = "geocode_requests"
	planCode   = "growth_monthly"
	couponCode = "LAUNCH20"
	currency   = "USD"
)

type addOn struct {
	Code, Name, Description string
	AmountCents             int
}

var addOns = []addOn{
	{Code: "onboarding", Name: "Onboarding package", Description: "Two onboarding sessions with a solutions engineer", AmountCents: 75000},
	{Code: "premium_support", Name: "Premium support (monthly)", Description: "4-hour response time, named contact", AmountCents: 30000},
}

// oneOffInvoice is an invoice for one add-on, outside the subscription.
type oneOffInvoice struct {
	AddOn string
	Paid  bool
}

type customer struct {
	ExternalID, Name, Email, Country string
	// SubscriptionPaid marks the first subscription invoice paid.
	SubscriptionPaid bool
	OneOff           []oneOffInvoice
}

func (c customer) subscriptionID() string { return "SUB-" + c.ExternalID }

var customers = []customer{
	{
		// Scenario 1: invoiced twice for onboarding; neither invoice is paid yet.
		ExternalID: "ACC-1042", Name: "Bluefin Logistics", Email: "billing@bluefin.example.com", Country: "US",
		SubscriptionPaid: true,
		OneOff:           []oneOffInvoice{{AddOn: "onboarding"}, {AddOn: "onboarding"}},
	},
	{
		// Scenario 2: unpaid invoices.
		ExternalID: "ACC-1077", Name: "Kettle & Co", Email: "accounts@kettle.example.com", Country: "GB",
		SubscriptionPaid: false,
		OneOff:           []oneOffInvoice{{AddOn: "premium_support", Paid: false}},
	},
	{
		// Scenario 3: asks for the LAUNCH20 coupon.
		ExternalID: "ACC-1103", Name: "Orbit Health", Email: "finance@orbithealth.example.com", Country: "US",
		SubscriptionPaid: true,
	},
	{
		// Scenario 4: a bug report; billing is not involved.
		ExternalID: "ACC-1019", Name: "Tidewater Labs", Email: "dev-ops@tidewater.example.com", Country: "NL",
		SubscriptionPaid: true,
	},
	{
		// Scenario 5: someone who is not the billing contact asks for invoices.
		ExternalID: "ACC-1088", Name: "Granite Ventures", Email: "billing@granite.example.com", Country: "US",
		SubscriptionPaid: true,
	},
}

type ticket struct {
	Title                        string
	FromName, FromEmail, Company string
	AccountID, Channel, Message  string
}

// body renders a ticket the way the support inbox files it.
func (t ticket) body() string {
	from := t.FromEmail
	if t.FromName != "" {
		from = fmt.Sprintf("%s <%s>", t.FromName, t.FromEmail)
	}
	return fmt.Sprintf("**From:** %s\n**Company:** %s\n**Account ID:** %s\n**Channel:** %s\n\n---\n\n%s\n",
		from, t.Company, t.AccountID, t.Channel, t.Message)
}

var tickets = []ticket{
	{
		Title:    "Invoiced twice for the onboarding package",
		FromName: "Dana Whitfield", FromEmail: "billing@bluefin.example.com",
		Company: "Bluefin Logistics", AccountID: "ACC-1042", Channel: "email",
		Message: "Hi GeoGrid team,\n\n" +
			"We received two invoices for the onboarding package this month, 750 USD each. We only " +
			"bought onboarding once, so please cancel the duplicate before our finance team pays it.\n\n" +
			"Given the hassle, could you also waive next month's Growth subscription?\n\n" +
			"Thanks,\nDana",
	},
	{
		Title:    "What do we owe? Will our API keys stop working?",
		FromName: "Priya Raman", FromEmail: "accounts@kettle.example.com",
		Company: "Kettle & Co", AccountID: "ACC-1077", Channel: "web form",
		Message: "We got a payment reminder from you but can't find the invoices in our system. " +
			"Can you tell us exactly what we owe and by when? If we pay next week, will our API keys " +
			"stop working in the meantime? Please send links to the invoice PDFs.",
	},
	{
		Title:    "LAUNCH20 discount missing from our invoice",
		FromName: "Marco Bianchi", FromEmail: "finance@orbithealth.example.com",
		Company: "Orbit Health", AccountID: "ACC-1103", Channel: "email",
		Message: "Hello,\n\n" +
			"When we signed up this week, your sales rep promised us the LAUNCH20 launch discount, " +
			"but our first invoice shows the full price. Could you apply the discount to our account?\n\n" +
			"Best regards,\nMarco",
	},
	{
		Title:    "Batch geocoding returns HTTP 500 for large CSV files",
		FromName: "Sam Okafor", FromEmail: "dev-ops@tidewater.example.com",
		Company: "Tidewater Labs", AccountID: "ACC-1019", Channel: "web form",
		Message: "Since yesterday, POST /v2/batch/geocode returns HTTP 500 for every CSV file larger " +
			"than about 10 MB. Smaller files still work. Request ID of the last failure: req_8f2c1e.\n\n" +
			"This breaks our nightly address-cleaning job. For now we split the files by hand, which " +
			"takes about an hour every morning.",
	},
	{
		Title:    "Please send copies of all our invoices",
		FromName: "", FromEmail: "j.doe@freemail.example.net",
		Company: "Granite Ventures", AccountID: "ACC-1088", Channel: "email",
		Message: "Hi, I'm the new finance contractor at Granite Ventures. Please send me copies of all " +
			"invoices on our account, and the payment details you have on file, so I can update our " +
			"records. Thanks!",
	},
}

type label struct {
	Name, Color, Description string
}

var labels = []label{
	{"type/billing", "#1d76db", "Invoices, charges, refunds, coupons, plans"},
	{"type/bug", "#d73a4a", "Something in the product is broken"},
	{"type/question", "#cc317c", "How to use the product"},
	{"type/feature-request", "#a2eeef", "Something the product does not do"},
	{"priority/P1", "#b60205", "Service down or blocked, data loss, security"},
	{"priority/P2", "#d93f0b", "Major problem, no workaround"},
	{"priority/P3", "#fbca04", "Problem with a workaround, routine request"},
	{"priority/P4", "#c2e0c6", "Question or feature request"},
	{"status/triaged", "#0e8a16", "Triaged by the support-triage agent"},
	{"status/needs-info", "#5319e7", "Waiting for information from the customer"},
}
