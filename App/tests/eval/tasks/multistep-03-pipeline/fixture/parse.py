def read_pairs(path):
    pairs = []
    for line in open(path):
        line = line.strip()
        if line:
            k, v = line.split(";")
            pairs.append((k, int(v)))
    return pairs
