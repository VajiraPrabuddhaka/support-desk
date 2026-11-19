---
name: triage-rubric
description: GeoGrid's rules for a support ticket's type and priority labels, when a ticket needs more information, and the format of the triage note.
---

# Triage rubric

## Type

Give every ticket exactly one type label:

- `type/billing`: invoices, charges, refunds, credits, coupons, discounts,
  plans and prices, payment problems
- `type/bug`: something in the product is broken or returns errors
- `type/question`: how to use the product
- `type/feature-request`: something the product does not do

## Priority

Give every ticket exactly one priority label:

- `priority/P1`: the customer's service is down or blocked, data loss,
  security, or money taken wrongly that blocks them
- `priority/P2`: a major feature is broken or a bill is wrong, and there is
  no workaround
- `priority/P3`: a problem with a workaround, or a routine billing request
- `priority/P4`: a question or a feature request

## Status

Add `status/triaged` to every ticket you triage. Use `status/needs-info` in
its place when something is missing:

- the ticket has no account ID
- billing_agent could not verify the person who wrote in
- a bug report has too little detail to reproduce

## Triage note

Write one note per ticket, in this Markdown format:

```
### Triage
**Type:** <type> · **Priority:** <priority>
**Summary:** <one or two sentences>
**Why this priority:** <one sentence>

### Billing
<only for billing tickets: billing_agent's findings, actions taken, and
what needs a person>

### Suggested reply
<the reply the support engineer can send; for billing tickets, start
from billing_agent's draft>
```
