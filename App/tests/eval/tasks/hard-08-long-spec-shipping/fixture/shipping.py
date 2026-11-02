"""Domestic Ground price quote. See SPEC.md (the sections that name this service)."""


class Prohibited(Exception):
    """The parcel may not be shipped (hazmat)."""


class ParcelTooHeavy(Exception):
    """The parcel is over the weight limit."""


def quote(parcel, origin, dest, saturday=False):
    """Return a dict of integer cents: base, oversize, remote, saturday, fuel, insurance, total.

    parcel: {"weight_kg": number, "l": number, "w": number, "h": number,
             "declared_value": int cents, "hazmat": bool}
    origin, dest: postcode strings.
    """
    raise NotImplementedError
