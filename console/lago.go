package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// lagoClient reads a customer's account in Lago: who they are, their invoices
// and their active coupons. It never changes anything.
type lagoClient struct {
	baseURL string // includes /api/v1
	apiKey  string
	http    *http.Client
}

func newLagoClient(baseURL, apiKey string) *lagoClient {
	return &lagoClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

type account struct {
	ID        string    `json:"id"`
	LagoID    string    `json:"lagoId"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
	Coupons   []string  `json:"coupons"`
	Invoices  []invoice `json:"invoices"`
	LagoURL   string    `json:"lagoUrl,omitempty"`
}

type invoice struct {
	Number        string `json:"number"`
	Type          string `json:"type"`
	Status        string `json:"status"`
	PaymentStatus string `json:"paymentStatus"`
	IssuingDate   string `json:"issuingDate"`
	TotalCents    int64  `json:"totalCents"`
	Currency      string `json:"currency"`
}

var errAccountNotFound = errors.New("account not found")

func (l *lagoClient) account(ctx context.Context, externalID string) (account, error) {
	var c struct {
		Customer struct {
			LagoID     string    `json:"lago_id"`
			ExternalID string    `json:"external_id"`
			Name       string    `json:"name"`
			Email      string    `json:"email"`
			CreatedAt  time.Time `json:"created_at"`
		} `json:"customer"`
	}
	if err := l.get(ctx, "/customers/"+url.PathEscape(externalID), &c); err != nil {
		var ue *upstreamError
		if errors.As(err, &ue) && ue.Status == http.StatusNotFound {
			return account{}, errAccountNotFound
		}
		return account{}, err
	}
	a := account{
		ID:        c.Customer.ExternalID,
		LagoID:    c.Customer.LagoID,
		Name:      c.Customer.Name,
		Email:     c.Customer.Email,
		CreatedAt: c.Customer.CreatedAt,
		Coupons:   []string{},
		Invoices:  []invoice{},
	}

	var inv struct {
		Invoices []struct {
			Number           string `json:"number"`
			InvoiceType      string `json:"invoice_type"`
			Status           string `json:"status"`
			PaymentStatus    string `json:"payment_status"`
			IssuingDate      string `json:"issuing_date"`
			TotalAmountCents int64  `json:"total_amount_cents"`
			Currency         string `json:"currency"`
		} `json:"invoices"`
	}
	if err := l.get(ctx, "/invoices?per_page=50&external_customer_id="+url.QueryEscape(externalID), &inv); err != nil {
		return account{}, err
	}
	for _, i := range inv.Invoices {
		a.Invoices = append(a.Invoices, invoice{
			Number: i.Number, Type: i.InvoiceType, Status: i.Status, PaymentStatus: i.PaymentStatus,
			IssuingDate: i.IssuingDate, TotalCents: i.TotalAmountCents, Currency: i.Currency,
		})
	}
	sort.Slice(a.Invoices, func(i, j int) bool { return a.Invoices[i].Number < a.Invoices[j].Number })

	var ac struct {
		AppliedCoupons []struct {
			CouponCode string `json:"coupon_code"`
		} `json:"applied_coupons"`
	}
	if err := l.get(ctx, "/applied_coupons?status=active&external_customer_id="+url.QueryEscape(externalID), &ac); err != nil {
		return account{}, err
	}
	for _, c := range ac.AppliedCoupons {
		a.Coupons = append(a.Coupons, c.CouponCode)
	}
	return a, nil
}

func (l *lagoClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+l.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := l.http.Do(req)
	if err != nil {
		return fmt.Errorf("lago: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &upstreamError{Service: "Lago", Status: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	return json.Unmarshal(data, out)
}
