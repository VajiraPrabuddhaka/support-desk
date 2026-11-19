package tickets

import "testing"

func TestBodyRoundTrip(t *testing.T) {
	in := Ticket{
		Title:    "Invoiced twice",
		FromName: "Dana Whitfield", FromEmail: "billing@bluefin.example.com",
		Company: "Bluefin Logistics", AccountID: "ACC-1042", Channel: "email",
		Message: "Hi,\n\nWe were invoiced twice.\n\n---\n\nThanks",
	}
	got := Parse(in.Title, in.Body())
	if got != in {
		t.Fatalf("round trip changed the ticket:\n got  %+v\n want %+v", got, in)
	}
}

func TestParseWithoutName(t *testing.T) {
	got := Parse("x", "**From:** j.doe@freemail.example.net\n**Company:** Granite\n**Account ID:** ACC-1088\n**Channel:** email\n\n---\n\nHello")
	if got.FromName != "" || got.FromEmail != "j.doe@freemail.example.net" || got.AccountID != "ACC-1088" || got.Message != "Hello" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseFreeText(t *testing.T) {
	got := Parse("x", "  Just a message.  ")
	if got.Message != "Just a message." || got.AccountID != "" {
		t.Fatalf("got %+v", got)
	}
}
