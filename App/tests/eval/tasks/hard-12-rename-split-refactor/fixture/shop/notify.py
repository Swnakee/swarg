from .utils import fmt_money


def welcome_email(customer):
    first = customer.name.split()[0] if customer.name.split() else customer.name
    return {
        "to": customer.email,
        "subject": "Welcome, %s" % first,
        "body": "Hello %s,\nYour customer number is #%d.\n" % (customer.name, customer.id),
    }


def low_balance_notice(customer, threshold_cents):
    if customer.balance_cents >= threshold_cents:
        return None
    return "Dear customer %s, your balance %s is below %s." % (
        customer.name, fmt_money(customer.balance_cents), fmt_money(threshold_cents))
