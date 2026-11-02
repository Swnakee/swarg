# Billing rules

All amounts are Decimal and in the same currency. "Round" always means rounding to cents, half up
(0.005 -> 0.01, 2.505 -> 2.51).

1. Line total = quantity * unit price, rounded.
2. Subtotal = sum of the line totals.
3. Volume discount percent from the subtotal: 5% when subtotal >= 100.00, 10% when >= 500.00,
   15% when >= 2000.00, otherwise 0%.
4. Loyalty customers get 5 more percentage points. The total discount percent never exceeds 18.
   Discount = subtotal * percent / 100, rounded.
5. Tax applies only to lines marked taxable. The discount is shared out pro rata: the discount
   attributed to taxable lines = discount * taxable_subtotal / subtotal, rounded. Tax base =
   taxable_subtotal - that attributed discount. Tax = base * region rate, rounded ONCE per invoice
   (not per line). An unknown region is an error (ValueError).
6. Shipping is 7.50, waived (0.00) when subtotal - discount >= 100.00. Shipping is not taxed.
7. Total = subtotal - discount + tax + shipping.
8. An order with no lines (or subtotal 0) has all amounts 0.00, including shipping.
9. The customer report sums invoice totals per customer_id (names can differ in case/spelling
   between orders) and lists customers by total descending, then customer_id ascending.
