# Parcelhub carrier integration spec

## 1. Purpose and scope

This document specifies the Parcelhub carrier integration platform, version 3.4.
It is the single reference for the web shop, the warehouse system and the customer service tools.
Not every section applies to every component. Each component implements the sections that name it.
The price quote service (module `shipping`, function `quote`) implements ONLY the Domestic Ground
service: sections 4, 5, 6, 7, 8 and 9, as changed by section 23 (Amendments).
All other sections are informational for that component and must not be implemented in it.
Prices are integers in cents. Weights are in kilograms, lengths in centimetres.
Where this document says 'round half up' it means rounding to the nearest integer, .5 going up.
If a rule in sections 1-22 conflicts with section 23, section 23 wins.

## 2. Glossary

Parcel: one physical package. A shipment may contain several parcels; quotes are per parcel.
Origin, destination: five-digit postcodes written as strings with leading zeros preserved.
Billable weight: the weight a price is calculated from (see section 5).
Zone: a pricing region derived from the two postcodes (see section 6).
Service: Ground, Express or Freight. Only Ground is quoted by the price quote service.
Surcharge: any amount added to the base price (section 8).
Label: the printable shipping label (section 10). Manifest: the daily list of labels (section 11).

## 3. Platform architecture

The platform consists of a gateway, a rating engine, a label printer, a tracking collector and a
billing exporter. Components talk to each other over an internal message bus.
The gateway terminates TLS and authenticates clients with signed API keys.
The rating engine is stateless; every quote is computed from the request alone.
The label printer renders labels from templates and caches fonts for 24 hours.
The tracking collector polls carrier endpoints and normalises events (section 12).
The billing exporter produces a CSV at 02:00 UTC every day for the finance team.
Components are deployed as containers; configuration comes from environment variables only.
A component that cannot reach the bus retries with exponential backoff, starting at 1 second
and doubling up to 64 seconds, then keeps retrying every 64 seconds and raises an alert.

## 4. Request format (all services)

A quote request describes one parcel and two postcodes.
Parcel fields: `weight_kg` (number, actual weight), `l`, `w`, `h` (numbers, centimetres),
`declared_value` (integer cents, 0 if none), `hazmat` (boolean).
Options: `saturday` (boolean, default false).
Dimensions and weight are always positive. The caller validates this; the rating engine may assume it.
Postcodes must be exactly five ASCII digits. Anything else is a ValueError.
The response of the quote function is a dict of integer cents with the keys
`base`, `oversize`, `remote`, `saturday`, `fuel`, `insurance` and `total`;
components that do not apply are 0. `total` is the sum of the other six.

## 5. Billable weight (Ground)

Dimensional weight = l * w * h / 5000 (kilograms).
Billable weight = the larger of the actual weight and the dimensional weight.
The billable weight is then rounded UP to the next multiple of 0.5 kg (2.0 stays 2.0, 2.01 becomes 2.5).
The minimum billable weight is 0.5 kg.
Use exact arithmetic where it matters: a parcel of exactly 2.0 kg must never be billed as 2.5 kg
because of floating point error.

## 6. Zones (Ground)

Zone = 1 + |first digit of origin - first digit of destination|, but never more than 6.
Examples: 10115 to 10999 is zone 1; 10115 to 40000 is zone 4; 10115 to 90000 is zone 6 (capped).

## 7. Base rates (Ground)

The base price depends on the zone and the billable weight band. Prices in cents:

| zone | up to 1.0 kg | up to 2.0 kg | up to 5.0 kg | up to 10.0 kg | up to 20.0 kg | each started kg above 20 |
|------|------|------|------|------|------|------|
| 1 | 500 | 650 | 900 | 1300 | 2000 | 90 |
| 2 | 600 | 780 | 1050 | 1500 | 2300 | 100 |
| 3 | 700 | 900 | 1200 | 1750 | 2700 | 120 |
| 4 | 850 | 1100 | 1500 | 2100 | 3300 | 150 |
| 5 | 1000 | 1300 | 1800 | 2600 | 4000 | 180 |
| 6 | 1200 | 1600 | 2200 | 3200 | 5000 | 220 |

A weight exactly on a band limit belongs to that band (2.0 kg is 'up to 2.0 kg').
Above 20 kg the price is the 'up to 20.0 kg' price plus the per-kg amount for every started kilogram
above 20 (20.5 kg = 1 started kg, 21.0 kg = 1, 21.5 kg = 2).

## 8. Surcharges (Ground)

Oversize: 1500 cents when the longest side is more than 120 cm, or when
longest side + 2 * (sum of the other two sides) is more than 300 cm.
Remote area: 800 cents when the destination postcode starts with 99 or 00.
Saturday delivery: 500 cents when option `saturday` is true. Saturday delivery is not available
to remote-area destinations: that is a ValueError.
Fuel surcharge: 7.5% of (base + oversize + remote), rounded half up. The Saturday fee carries no fuel.
Insurance: only when declared_value is more than 10000 cents. It is 1% of the declared value, rounded half up,
but at least 200 and at most 5000 cents. Insurance carries no fuel surcharge.

## 9. Rejections (Ground)

Hazmat parcels are refused: raise `Prohibited`.
An actual weight above 70 kg is refused: raise `ParcelTooHeavy` (the dimensional weight is not considered).
Rejections are checked after the postcodes are validated (ValueError first) and before any pricing.
A request with hazmat=true AND a weight above 70 kg raises `Prohibited`.

## 10. Labels

Labels are rendered as PDF at 10 x 15 cm. The layout has five zones: sender, recipient, barcode,
service icon and routing code. Fonts: the sender block uses 8 pt, the recipient block 12 pt bold.
The barcode is Code 128 and encodes the 18-digit tracking number. Quiet zones are 3 mm.
Service icons: Ground is a square, Express a lightning bolt, Freight a pallet.
Labels for remote destinations carry the text REMOTE in the routing code line.
A label must be reprintable for 30 days after creation with identical content.
Languages: labels are always printed in English, address lines are left as entered.
Maximum address line length is 40 characters; longer lines are wrapped, never truncated.

## 11. Manifests

A manifest lists all labels handed to a carrier on one day. It is closed at 17:00 local time.
After closing, new labels go to the next day's manifest. A closed manifest is immutable.
Each manifest has a number of the form MF-YYYYMMDD-NNN where NNN counts manifests of that day.
The carrier receives the manifest as CSV and as a signed PDF summary.

## 12. Tracking events

Carrier events are normalised to: CREATED, PICKED_UP, IN_TRANSIT, OUT_FOR_DELIVERY, DELIVERED,
EXCEPTION, RETURNED. Unknown carrier events are stored as EXCEPTION with the raw text attached.
Events are idempotent: the pair (tracking number, carrier event id) is unique.
The collector polls every 15 minutes for parcels not yet DELIVERED or RETURNED.
A parcel with no event for 10 days is flagged LOST and reported to customer service.
Timestamps are stored in UTC and shown in the recipient's local time.

## 13. Returns

A customer may request a return within 30 days of delivery. The return label is generated on demand.
Return shipments use the Ground service with origin and destination swapped.
The first return per order is free of shipping charge; later ones are charged as normal Ground quotes.
Returns from remote areas are accepted but flagged for manual review.
Refund timing is handled by the finance component and is out of scope here.

## 14. Express service

Express is next-business-day delivery within zones 1-3 and two days for zones 4-6.
Express base rates are 1.8 times the Ground rates of the same zone and weight band, rounded half up.
The Express fuel surcharge is 12%. Express has no oversize parcels: oversize parcels must use Freight.
Express cut-off is 15:00 local time at the origin warehouse.
Express insurance is 2% of the declared value with a minimum of 300 cents and no maximum.

## 15. Freight service

Freight is for parcels heavier than 70 kg or oversize parcels. It is priced per pallet position.
A pallet position is 120 x 80 cm. Stacking is allowed up to 160 cm total height.
Freight quotes are manual: the rating engine returns a 'quote on request' response.
Delivery appointments are booked by the carrier with the recipient 24 hours in advance.

## 16. International shipments

International shipments need a customs declaration (CN22 up to 2 kg and 400 EUR, CN23 above).
The declaration lists item descriptions, tariff numbers, origin country and value.
Zones for international are letters A-F, unrelated to the domestic numeric zones.
Restricted goods by country are kept in the compliance table, updated monthly.
Duties and taxes are paid by the recipient unless the shipper selects DDP.
International shipments are not quoted by the price quote service.

## 17. Packaging

The warehouse offers three box sizes: S (20 x 15 x 10), M (35 x 25 x 15), L (50 x 40 x 30) cm.
A packaging fee of 150 cents applies to M and 250 cents to L; it is added by the web shop, not by
the rating engine. Fragile items are padded with paper; the fee for that is 100 cents.
Boxes are chosen by the warehouse system using a first-fit algorithm over item volume.
The chosen box dimensions are what the price quote service receives as l, w and h.

## 18. Pickup and handover

Carriers collect parcels between 14:00 and 16:00 on working days.
If the warehouse is not ready, the carrier waits at most 10 minutes, then the pickup is rescheduled.
Handover is confirmed by a scan; the scan produces the PICKED_UP event.
A missed pickup is reported by the warehouse system within one hour.

## 19. Service levels and uptime

The rating engine targets 99.9% monthly availability and a p95 latency of 200 ms.
The label printer targets p95 of 2 s per label. The tracking collector may lag by up to 30 minutes.
Maintenance windows are announced 48 hours ahead and happen between 01:00 and 03:00 UTC on Sundays.
Incidents are classified P1 (platform down), P2 (feature down), P3 (degraded) and P4 (cosmetic).

Logs are JSON lines with the fields ts, level, component, request_id and message.
Personal data (names, addresses, phone numbers) is never written to logs; postcodes are allowed.
API keys rotate every 90 days; the old key stays valid for 7 days after rotation.
Label PDFs are kept 90 days, manifests 10 years, quote requests 30 days.
Backups run nightly and are restored in a test environment every quarter.

## 20. Address validation

Addresses are validated by the gateway before a quote or a label request reaches the other components.
The validator applies these checks in order and stops at the first failure:
1. The country code is a two-letter ISO code from the supported list.
2. The postcode matches the national pattern (five digits for the domestic market).
3. The city is not empty and not longer than 40 characters.
4. The street line contains at least one digit unless the address is a PO box.
5. PO boxes are refused for the Freight service and for hazmat shipments.
Unknown abbreviations such as `Str.` are expanded using the abbreviation table, which has about 300 rows
and is maintained by customer service. The expansion never changes the postcode.
A validation failure returns HTTP 422 with a list of `{field, code, message}` objects.
Error codes: ADDR_COUNTRY, ADDR_POSTCODE, ADDR_CITY, ADDR_STREET, ADDR_POBOX.
Warnings (not failures): ADDR_SUSPECT_DUPLICATE when the same address was used for another customer
within 24 hours, ADDR_LONG_LINE when a line was wrapped by the label printer.
The validator caches positive results for 12 hours keyed by the normalised address string.
Normalisation lower-cases the text, collapses whitespace, strips punctuation except hyphens and
replaces umlauts by their base letters for the cache key only.

## 21. Notification templates

Customers receive e-mails and SMS messages at defined points of the delivery:
| event | channel | template id | sent when |
|-------|---------|-------------|-----------|
| CREATED | e-mail | N-01 | the label has been created |
| PICKED_UP | e-mail | N-02 | the carrier scanned the parcel |
| OUT_FOR_DELIVERY | SMS | N-03 | the parcel is on the delivery vehicle |
| DELIVERED | e-mail | N-04 | the delivery was confirmed |
| EXCEPTION | e-mail | N-05 | the carrier reported a problem |
| LOST | e-mail | N-06 | no events for 10 days |
Templates are plain text with `{{placeholders}}`: `{{first_name}}`, `{{tracking_number}}`, `{{eta}}`.
SMS messages are limited to 160 characters and never contain links to third-party sites.
A customer can opt out of SMS in the account page; e-mails about problems cannot be switched off.
Quiet hours: SMS are not sent between 21:00 and 08:00 recipient local time; they are queued.
Templates exist in English, German and Polish; the language follows the order locale.
Missing translations fall back to English and raise a P4 incident.

## 22. Test data and sandbox

The sandbox environment mirrors production but never contacts real carriers.
It accepts these fixed test postcodes: 10115 (Berlin), 20095 (Hamburg), 80331 (Munich), 99999 (remote test).
Sandbox tracking numbers start with `TEST` and move through the events on a fixed schedule:
CREATED at once, PICKED_UP after 1 minute, IN_TRANSIT after 5 minutes, DELIVERED after 15 minutes.
Sandbox quotes use the same rating engine as production, so rates and rules are identical.
The sandbox accepts any API key that starts with `demo-` (rate limited to 60 requests per minute).
Contract tests live in the `contracts/` folder of the repository. They are run on every deployment.
A failing contract test blocks the deployment of the rating engine and notifies the on-call engineer.
Load tests are run before each major release at 200 requests per second for ten minutes.
The acceptance criteria are: error rate below 0.1%, p95 latency below 250 ms, no memory growth.

## 23. Amendments (supersede sections 1-22)

A1 (section 8, fuel). The Ground fuel surcharge is 8.25% (not 7.5%) and its base is
base + oversize + remote + saturday (the Saturday fee now carries fuel). Rounded half up.
A2 (section 8, oversize). The thresholds are tightened: oversize when the longest side is more than
100 cm (not 120), or when longest side + 2 * (sum of the other two sides) is more than 250 cm (not 300).
A3 (section 8, remote). Destination postcodes starting with 98 are also remote areas, in addition to
99 and 00. Everything said about remote areas (surcharge, Saturday refusal) applies to them.
A4 (section 5). The minimum billable weight is 1.0 kg (not 0.5 kg). The 0.5 kg rounding step stays.
No other rule changes.

## 24. Change log

3.4: amendments A1-A4 collected in section 23. 3.3: Express fuel raised to 12%.
3.2: tracking LOST rule added. 3.1: manifest numbering changed. 3.0: first public version.

