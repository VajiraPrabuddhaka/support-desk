// Package tickets holds the format of a GeoGrid support ticket: the issue body
// that the support inbox writes, with the customer's details on top and their
// message below. The seed program writes tickets in it, and the console reads
// them.
package tickets

import (
	"fmt"
	"regexp"
	"strings"
)

// Ticket is a customer's support request.
type Ticket struct {
	Title     string
	FromName  string
	FromEmail string
	Company   string
	AccountID string
	Channel   string
	Message   string
}

// Body renders the issue body of the ticket, the way the support inbox files it.
func (t Ticket) Body() string {
	from := t.FromEmail
	if t.FromName != "" {
		from = fmt.Sprintf("%s <%s>", t.FromName, t.FromEmail)
	}
	return fmt.Sprintf("**From:** %s\n**Company:** %s\n**Account ID:** %s\n**Channel:** %s\n\n---\n\n%s\n",
		from, t.Company, t.AccountID, t.Channel, strings.TrimSpace(t.Message))
}

var (
	fieldLine = regexp.MustCompile(`(?m)^\*\*([A-Za-z ]+):\*\*[ \t]*(.*?)[ \t]*$`)
	nameEmail = regexp.MustCompile(`^(.*?)\s*<([^>]+)>$`)
)

// Parse reads a ticket back from an issue body. A body that is not in the
// inbox's format is all message.
func Parse(title, body string) Ticket {
	t := Ticket{Title: title}
	head, message, found := strings.Cut(strings.ReplaceAll(body, "\r\n", "\n"), "\n---\n")
	if !found {
		t.Message = strings.TrimSpace(body)
		return t
	}
	t.Message = strings.TrimSpace(message)
	for _, m := range fieldLine.FindAllStringSubmatch(head, -1) {
		value := m[2]
		switch m[1] {
		case "From":
			if nm := nameEmail.FindStringSubmatch(value); nm != nil {
				t.FromName, t.FromEmail = strings.TrimSpace(nm[1]), nm[2]
			} else {
				t.FromEmail = value
			}
		case "Company":
			t.Company = value
		case "Account ID":
			t.AccountID = value
		case "Channel":
			t.Channel = value
		}
	}
	return t
}
