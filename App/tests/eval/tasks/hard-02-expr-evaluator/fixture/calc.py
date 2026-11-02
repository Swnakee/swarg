"""Arithmetic expression evaluator. See SPEC.md."""


class ExprError(Exception):
    def __init__(self, message, pos):
        super().__init__(message)
        self.message = message
        self.pos = pos


def evaluate(text, variables=None):
    raise NotImplementedError
