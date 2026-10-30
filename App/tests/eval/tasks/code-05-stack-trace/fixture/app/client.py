class Client:
    def __init__(self, timeout, retries):
        self.timeout = timeout
        self.retries = retries

    def describe(self):
        return "timeout=%d retries=%d" % (self.timeout, self.retries)
