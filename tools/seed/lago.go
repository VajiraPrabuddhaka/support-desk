package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

type lagoSeeder struct {
	api *apiClient
}

func newLagoSeeder(lagoURL, apiKey string) *lagoSeeder {
	return &lagoSeeder{api: newAPIClient(lagoURL, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+apiKey)
	})}
}

type lagoInvoice struct {
	LagoID           string `json:"lago_id"`
	Number           string `json:"number"`
	InvoiceType      string `json:"invoice_type"`
	Status           string `json:"status"`
	PaymentStatus    string `json:"payment_status"`
	TotalAmountCents int    `json:"total_amount_cents"`
}

func (l *lagoSeeder) run(ctx context.Context) error {
	// Lago creates the organization and its API key on first start
	// (LAGO_CREATE_ORG). Until then the key is refused.
	if err := waitFor(ctx, "the Lago API and organization", 5*time.Minute, func() (bool, error) {
		err := l.api.do(ctx, http.MethodGet, "/api/v1/billable_metrics?per_page=1", nil, nil)
		return err == nil, err
	}); err != nil {
		return err
	}

	metricID, err := l.ensureMetric(ctx)
	if err != nil {
		return fmt.Errorf("lago billable metric: %w", err)
	}
	if err := l.ensurePlan(ctx, metricID); err != nil {
		return fmt.Errorf("lago plan: %w", err)
	}
	for _, a := range addOns {
		if err := l.ensureAddOn(ctx, a); err != nil {
			return fmt.Errorf("lago add-on %s: %w", a.Code, err)
		}
	}
	if err := l.ensureCoupon(ctx); err != nil {
		return fmt.Errorf("lago coupon: %w", err)
	}
	for _, c := range customers {
		if err := l.ensureCustomer(ctx, c); err != nil {
			return fmt.Errorf("lago customer %s: %w", c.ExternalID, err)
		}
	}
	return nil
}

func (l *lagoSeeder) ensureMetric(ctx context.Context) (string, error) {
	var resp struct {
		BillableMetric struct {
			LagoID string `json:"lago_id"`
		} `json:"billable_metric"`
	}
	err := l.api.do(ctx, http.MethodGet, "/api/v1/billable_metrics/"+metricCode, nil, &resp)
	if isStatus(err, http.StatusNotFound) {
		slog.Info("lago: creating billable metric", "code", metricCode)
		err = l.api.do(ctx, http.MethodPost, "/api/v1/billable_metrics", map[string]any{
			"billable_metric": map[string]any{
				"name":             "Geocode requests",
				"code":             metricCode,
				"description":      "Requests to the forward and reverse geocoding endpoints",
				"aggregation_type": "count_agg",
				"recurring":        false,
			},
		}, &resp)
	}
	return resp.BillableMetric.LagoID, err
}

func (l *lagoSeeder) ensurePlan(ctx context.Context, metricID string) error {
	err := l.api.do(ctx, http.MethodGet, "/api/v1/plans/"+planCode, nil, nil)
	if err == nil || !isStatus(err, http.StatusNotFound) {
		return err
	}
	slog.Info("lago: creating plan", "code", planCode)
	return l.api.do(ctx, http.MethodPost, "/api/v1/plans", map[string]any{
		"plan": map[string]any{
			"name":            "Growth",
			"code":            planCode,
			"description":     "199 USD a month, paid in advance, plus 0.40 USD per 1,000 geocode requests",
			"interval":        "monthly",
			"amount_cents":    19900,
			"amount_currency": currency,
			"pay_in_advance":  true,
			"charges": []map[string]any{{
				"billable_metric_id": metricID,
				"charge_model":       "package",
				"pay_in_advance":     false,
				"properties": map[string]any{
					"amount":       "0.40",
					"free_units":   0,
					"package_size": 1000,
				},
			}},
		},
	}, nil)
}

func (l *lagoSeeder) ensureAddOn(ctx context.Context, a addOn) error {
	err := l.api.do(ctx, http.MethodGet, "/api/v1/add_ons/"+a.Code, nil, nil)
	if err == nil || !isStatus(err, http.StatusNotFound) {
		return err
	}
	slog.Info("lago: creating add-on", "code", a.Code)
	return l.api.do(ctx, http.MethodPost, "/api/v1/add_ons", map[string]any{
		"add_on": map[string]any{
			"name":            a.Name,
			"code":            a.Code,
			"description":     a.Description,
			"amount_cents":    a.AmountCents,
			"amount_currency": currency,
		},
	}, nil)
}

func (l *lagoSeeder) ensureCoupon(ctx context.Context) error {
	err := l.api.do(ctx, http.MethodGet, "/api/v1/coupons/"+couponCode, nil, nil)
	if err == nil || !isStatus(err, http.StatusNotFound) {
		return err
	}
	slog.Info("lago: creating coupon", "code", couponCode)
	return l.api.do(ctx, http.MethodPost, "/api/v1/coupons", map[string]any{
		"coupon": map[string]any{
			"name":               "Launch discount, 20% for 3 months",
			"code":               couponCode,
			"description":        "For new customers, within 30 days of signing up",
			"coupon_type":        "percentage",
			"percentage_rate":    "20.0",
			"frequency":          "recurring",
			"frequency_duration": 3,
			"expiration":         "no_expiration",
			"reusable":           true,
		},
	}, nil)
}

// ensureCustomer creates the customer, its Growth subscription and its
// one-off invoices, then marks the invoices paid that the scenario says were
// paid. Nothing is created twice when the seed runs again. A voided invoice
// does not count, so after billing-agent voids a duplicate, running the seed
// again creates a new one and the scenario can be played again.
func (l *lagoSeeder) ensureCustomer(ctx context.Context, c customer) error {
	// POST /customers creates or updates.
	if err := l.api.do(ctx, http.MethodPost, "/api/v1/customers", map[string]any{
		"customer": map[string]any{
			"external_id":      c.ExternalID,
			"name":             c.Name,
			"email":            c.Email,
			"country":          c.Country,
			"currency":         currency,
			"net_payment_term": 30,
		},
	}, nil); err != nil {
		return err
	}

	err := l.api.do(ctx, http.MethodGet, "/api/v1/subscriptions/"+url.PathEscape(c.subscriptionID()), nil, nil)
	if isStatus(err, http.StatusNotFound) {
		slog.Info("lago: creating subscription", "customer", c.ExternalID, "plan", planCode)
		err = l.api.do(ctx, http.MethodPost, "/api/v1/subscriptions", map[string]any{
			"subscription": map[string]any{
				"external_customer_id": c.ExternalID,
				"external_id":          c.subscriptionID(),
				"plan_code":            planCode,
				"name":                 "Growth",
				"billing_time":         "anniversary",
			},
		}, nil)
	}
	if err != nil {
		return fmt.Errorf("subscription: %w", err)
	}

	invoices, err := l.invoices(ctx, c.ExternalID)
	if err != nil {
		return err
	}
	oneOffs := filterInvoices(invoices, "one_off")
	for i := len(oneOffs); i < len(c.OneOff); i++ {
		slog.Info("lago: creating one-off invoice", "customer", c.ExternalID, "add_on", c.OneOff[i].AddOn)
		if err := l.api.do(ctx, http.MethodPost, "/api/v1/invoices", map[string]any{
			"invoice": map[string]any{
				"external_customer_id": c.ExternalID,
				"currency":             currency,
				"fees":                 []map[string]any{{"add_on_code": c.OneOff[i].AddOn, "units": 1}},
			},
		}, nil); err != nil {
			return fmt.Errorf("one-off invoice: %w", err)
		}
	}

	// The subscription invoice is created by a background job; wait for it
	// and for all one-off invoices to be finalized.
	var subs []lagoInvoice
	if err := waitFor(ctx, "invoices of "+c.ExternalID, 2*time.Minute, func() (bool, error) {
		all, err := l.invoices(ctx, c.ExternalID)
		if err != nil {
			return false, err
		}
		subs, oneOffs = filterInvoices(all, "subscription"), filterInvoices(all, "one_off")
		return len(subs) > 0 && len(oneOffs) >= len(c.OneOff), nil
	}); err != nil {
		return err
	}

	if c.SubscriptionPaid {
		if err := l.markPaid(ctx, subs[len(subs)-1]); err != nil {
			return err
		}
	}
	// invoices() lists newest first; pair scenario entries with the oldest first.
	for i, want := range c.OneOff {
		if want.Paid {
			if err := l.markPaid(ctx, oneOffs[len(oneOffs)-1-i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *lagoSeeder) invoices(ctx context.Context, customerID string) ([]lagoInvoice, error) {
	var resp struct {
		Invoices []lagoInvoice `json:"invoices"`
	}
	path := "/api/v1/invoices?per_page=100&external_customer_id=" + url.QueryEscape(customerID)
	if err := l.api.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Invoices, nil
}

// markPaid records a payment made outside Lago. Without a payment provider
// this is how a finance team marks an invoice paid.
func (l *lagoSeeder) markPaid(ctx context.Context, inv lagoInvoice) error {
	if inv.PaymentStatus == "succeeded" {
		return nil
	}
	slog.Info("lago: marking invoice paid", "number", inv.Number, "type", inv.InvoiceType)
	return l.api.do(ctx, http.MethodPut, "/api/v1/invoices/"+inv.LagoID, map[string]any{
		"invoice": map[string]any{"payment_status": "succeeded"},
	}, nil)
}

func filterInvoices(all []lagoInvoice, invoiceType string) []lagoInvoice {
	var out []lagoInvoice
	for _, inv := range all {
		if inv.InvoiceType == invoiceType && inv.Status == "finalized" {
			out = append(out, inv)
		}
	}
	return out
}
