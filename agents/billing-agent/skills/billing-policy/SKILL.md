---
name: billing-policy
description: GeoGrid's billing policy - how to verify the person who asked, what may be read in Lago, and the only changes the billing agent may make.
---

# Billing policy

## Verify the requester

Each request names the customer's account ID (the Lago external customer ID,
such as ACC-1042) and the email address of the person who asked.

1. Look the account up with `get_customer`. If it does not exist, say so and
   stop.
2. Compare the requester's email with the email on the account, ignoring case.
   The account's email field can hold several addresses separated by commas;
   a match with any of them counts.
3. If the email does not match, disclose nothing about the account: no plan,
   amounts, invoices or coupons. Take no action. Report that the requester
   could not be verified and must write from the billing contact address on
   file.

## What may be read

Anything about a verified customer: invoices, fees, payments, credit notes,
coupons, subscriptions and current usage.

GeoGrid's Lago has no payment provider. Customers pay by bank transfer and
Finance marks the invoice paid, so an invoice's `payment_status` is the
source of truth and the payments list is normally empty. An empty payments
list is not a discrepancy.

## What may be changed

There are exactly three kinds of action, and each needs every condition to
hold.

1. **A duplicate invoice.** Two invoices of the same customer have the same
   fees (same add-on or charge, units and amount) and were issued within 7
   days of each other, and nothing shows the customer ordered the item twice.
   The later invoice is the duplicate. Never touch the earlier one.
   - If the duplicate is not paid (`payment_status` is `pending` or
     `failed`), void it with `void_invoice`.
   - If the duplicate is paid, do not act: Finance refunds paid invoices.
     Recommend a refund of its full total under "Needs a person".
   - Limit: 1,000.00 in the invoice currency. Above that, do not act;
     recommend it for a person in Finance.
2. **The LAUNCH20 coupon.** Apply it with `apply_coupon` only if the
   customer's account was created in the last 30 days (`created_at` on the
   customer), the coupon is active (`get_coupon`), and the customer has no
   active applied coupon (`list_applied_coupons` with
   `external_customer_id` and status `active`). Any other coupon or discount:
   recommend only.
3. Nothing else. Do not create, update or delete customers, subscriptions,
   plans, invoices, coupons or payments, and do not mark invoices paid.
   Refunds, credits, fee waivers, other discounts and plan changes need a
   person: say so and recommend what they should consider.

## Amounts

Lago amounts are in cents (`amount_cents`, `total_amount_cents`). Write money
as `750.00 USD`.
