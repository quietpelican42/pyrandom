#!/usr/bin/env python3
"""Generate differential fixtures for pyrandom from the local CPython.

Usage: gen.py [--seeds N] [--calls N] [--seed-of-args N] > fixture.jsonl

Each output line is one seed with a list of operations. Integers are decimal
strings, floats are float.hex() strings, lists are lists, and an operation
that raises in CPython has the result "ERR". The final "end" record holds the
generator state after all operations.
"""
import argparse
import hashlib
import json
import math
import random
import sys


def fx(x):
    return float(x).hex()


def enc(v):
    if isinstance(v, bool):
        return str(int(v))
    if isinstance(v, int):
        return str(v)
    if isinstance(v, float):
        return fx(v)
    if isinstance(v, (list, tuple)):
        return [enc(x) for x in v]
    if v is None:
        return None
    if isinstance(v, str):
        return v
    raise TypeError(type(v))


def make_seeds(n, a):
    seeds = [("int", 0), ("int", 1), ("int", 42), ("int", 2**31), ("int", 2**32 - 1),
             ("int", 2**32), ("int", 2**64 - 1), ("int", -1), ("int", -(2**63)),
             ("str", ""), ("str", "hello"), ("bytes", b""), ("bytes", b"\x00"),
             ("float", 1.5), ("float", -0.1), ("float", 3.0), ("float", 1e300),
             ("float", float("inf")), ("float", -2.5e-8), ("float", 0.0)]
    while len(seeds) < n:
        kind = len(seeds) % 5
        if kind == 0:
            seeds.append(("int", a.randrange(0, 2**a.randrange(1, 70))))
        elif kind == 1:
            seeds.append(("int", a.randrange(2**64, 2**a.randrange(65, 400))))
        elif kind == 2:
            seeds.append(("int", -a.randrange(1, 2**a.randrange(1, 200))))
        elif kind == 3:
            k = a.randrange(0, 40)
            seeds.append(("str", "".join(chr(a.choice([a.randrange(32, 127), a.randrange(0xA0, 0x2FF), a.randrange(0x4E00, 0x4F00), a.randrange(0x1F300, 0x1F400)])) for _ in range(k))))
        else:
            if a.random() < 0.5:
                seeds.append(("bytes", bytes(a.randrange(256) for _ in range(a.randrange(0, 50)))))
            else:
                seeds.append(("float", a.uniform(-1e6, 1e6) * 10 ** a.randrange(-10, 10)))
    return seeds


def seed_json(kind, v):
    if kind == "int":
        return {"t": "int", "v": str(v)}
    if kind == "str":
        return {"t": "str", "v": v}
    if kind == "bytes":
        return {"t": "bytes", "v": v.hex()}
    return {"t": "float", "v": fx(v)}


def rnd_float(a, lo, hi):
    return a.uniform(lo, hi)


def rnd_big(a):
    return a.randrange(-2**a.choice([8, 31, 40, 64, 100, 200]), 2**a.choice([8, 31, 40, 64, 100, 200]))


# Each op generator returns (args, call) where call(r) performs the call and
# returns the encodable result.
def op_random(a):
    return [], lambda r: r.random()


def op_getrandbits(a):
    k = a.choice([0, 1, 7, 8, 31, 32, 33, 63, 64, 65, 96, 127, 128, 129, 200, 257, a.randrange(0, 500)])
    return [k], lambda r: r.getrandbits(k)


def op_randbelow(a):
    n = a.choice([1, 2, 3, 5, 10, 2**31, 2**32, 2**32 + 1, 2**63, 2**64 - 1, 2**64, 2**64 + 1, 10**30, a.randrange(1, 2**a.randrange(1, 130))])
    return [n], lambda r: r._randbelow(n)


def op_randrange(a):
    form = a.randrange(4)
    if form == 0:
        n = a.randrange(1, 10**a.randrange(1, 25))
        return [n], lambda r: r.randrange(n)
    if form == 1:
        s = rnd_big(a); w = a.randrange(1, 10**a.randrange(1, 25)); e = s + w
        return [s, e], lambda r: r.randrange(s, e)
    s = a.randrange(-1000, 1000)
    st = a.choice([1, 2, 3, 7, -1, -2, -5, 10**a.randrange(1, 12), -(10**a.randrange(1, 12))])
    w = a.randrange(1, 10**a.randrange(1, 15))
    e = s + w if st > 0 else s - w
    return [s, e, st], lambda r: r.randrange(s, e, st)


def op_randint(a):
    lo = rnd_big(a); hi = lo + a.randrange(0, 10**a.randrange(1, 25))
    return [lo, hi], lambda r: r.randint(lo, hi)


def op_randbytes(a):
    n = a.choice([0, 1, 3, 4, 5, 16, 33, a.randrange(0, 100)])
    return [n], lambda r: r.randbytes(n).hex()


def op_choice(a):
    n = a.choice([1, 2, 3, 10, 100, 1000, a.randrange(1, 10**6)])
    return [n], lambda r: r.choice(range(n))


def op_choices(a):
    n = a.randrange(1, 40); k = a.randrange(0, 12)
    form = a.randrange(4)
    if form == 0:
        return [n, None, None, k], lambda r: r.choices(range(n), k=k)
    if form == 1:
        w = [a.uniform(0, 10) for _ in range(n)]
        w[a.randrange(n)] += 0.5
        return [n, w, None, k], lambda r: r.choices(range(n), w, k=k)
    if form == 2:
        w = [a.randrange(0, 20) for _ in range(n)]; w[a.randrange(n)] += 1
        return [n, w, None, k], lambda r: r.choices(range(n), w, k=k)
    w = [a.uniform(0, 5) for _ in range(n)]; c = []; t = 0.0
    for x in w:
        t += x; c.append(t)
    return [n, None, c, k], lambda r: r.choices(range(n), cum_weights=c, k=k)


def op_shuffle(a):
    n = a.choice([0, 1, 2, 3, 10, 52, a.randrange(0, 300)])
    def call(r):
        x = list(range(n)); r.shuffle(x); return x
    return [n], call


def op_sample(a):
    n = a.choice([1, 5, 21, 22, 25, 50, 100, 1000, 10000, a.randrange(1, 5000)])
    k = a.choice([0, 1, 5, 6, 10, 20, 50, 200, n // 2]); k = min(k, n)
    return [n, k], lambda r: r.sample(range(n), k)


def op_sample_counts(a):
    n = a.randrange(1, 15); c = [a.randrange(0, 6) for _ in range(n)]; c[a.randrange(n)] += 1
    k = a.randrange(0, sum(c) + 1)
    return [n, c, k], lambda r: r.sample(list(range(n)), k, counts=c)


def op_uniform(a):
    lo = a.uniform(-1e3, 1e3); hi = lo + a.uniform(0, 1e3)
    return [lo, hi], lambda r: r.uniform(lo, hi)


def op_triangular(a):
    lo = a.uniform(-10, 10); hi = lo + a.uniform(0, 20)
    f = a.randrange(3)
    if f == 0:
        return [lo, hi, None], lambda r: r.triangular(lo, hi)
    m = a.uniform(lo, hi)
    if f == 1:
        return [lo, hi, m], lambda r: r.triangular(lo, hi, m)
    return [lo, lo, lo], lambda r: r.triangular(lo, lo, lo)


def op_gauss(a):
    mu = a.uniform(-5, 5); s = a.uniform(0.1, 5)
    return [mu, s], lambda r: r.gauss(mu, s)


def op_normalvariate(a):
    mu = a.uniform(-5, 5); s = a.uniform(0.1, 5)
    return [mu, s], lambda r: r.normalvariate(mu, s)


def op_lognormvariate(a):
    mu = a.uniform(-2, 2); s = a.uniform(0.1, 1.5)
    return [mu, s], lambda r: r.lognormvariate(mu, s)


def op_expovariate(a):
    l = a.choice([1.0, a.uniform(0.01, 20), -a.uniform(0.01, 20)])
    return [l], lambda r: r.expovariate(l)


def op_vonmises(a):
    mu = a.uniform(0, 2 * math.pi); k = a.choice([0.0, 1e-7, a.uniform(0, 1), a.uniform(0, 20), a.uniform(0, 500)])
    return [mu, k], lambda r: r.vonmisesvariate(mu, k)


def op_gamma(a):
    al = a.choice([a.uniform(0.01, 1), 1.0, a.uniform(1, 5), a.uniform(1, 200), 0.5, 2.0])
    be = a.uniform(0.1, 10)
    return [al, be], lambda r: r.gammavariate(al, be)


def op_beta(a):
    al = a.uniform(0.05, 20); be = a.uniform(0.05, 20)
    return [al, be], lambda r: r.betavariate(al, be)


def op_pareto(a):
    al = a.uniform(0.1, 10)
    return [al], lambda r: r.paretovariate(al)


def op_weibull(a):
    al = a.uniform(0.1, 10); be = a.uniform(0.1, 10)
    return [al, be], lambda r: r.weibullvariate(al, be)


def op_binomial(a):
    n = a.choice([0, 1, 2, 5, 20, 100, 1000, 5000, a.randrange(0, 10**5)])
    p = a.choice([0.0, 1.0, 0.5, a.random(), a.random(), 0.01, 0.99, a.uniform(0.3, 0.7)])
    return [n, p], lambda r: r.binomialvariate(n, p)


OPS = {
    "random": op_random, "getrandbits": op_getrandbits, "randbelow": op_randbelow,
    "randrange": op_randrange, "randint": op_randint, "randbytes": op_randbytes,
    "choice": op_choice, "choices": op_choices, "shuffle": op_shuffle,
    "sample": op_sample, "sample_counts": op_sample_counts,
    "uniform": op_uniform, "triangular": op_triangular, "gauss": op_gauss,
    "normalvariate": op_normalvariate, "lognormvariate": op_lognormvariate,
    "expovariate": op_expovariate, "vonmisesvariate": op_vonmises,
    "gammavariate": op_gamma, "betavariate": op_beta, "paretovariate": op_pareto,
    "weibullvariate": op_weibull, "binomialvariate": op_binomial,
}

# Calls that must raise in CPython.
ERR_CASES = [
    ("randrange", [5, 5], lambda r: r.randrange(5, 5)),
    ("randrange", [0], lambda r: r.randrange(0)),
    ("randrange", [0, 10, 0], lambda r: r.randrange(0, 10, 0)),
    ("randrange", [10, 0, 1], lambda r: r.randrange(10, 0, 1)),
    ("randint", [3, 2], lambda r: r.randint(3, 2)),
    ("getrandbits", [-1], lambda r: r.getrandbits(-1)),
    ("choice", [0], lambda r: r.choice([])),
    ("sample", [3, 4], lambda r: r.sample(range(3), 4)),
    ("sample", [3, -1], lambda r: r.sample(range(3), -1)),
    ("gammavariate", [0.0, 1.0], lambda r: r.gammavariate(0.0, 1.0)),
    ("gammavariate", [1.0, -1.0], lambda r: r.gammavariate(1.0, -1.0)),
    ("binomialvariate", [-1, 0.5], lambda r: r.binomialvariate(-1, 0.5)),
    ("binomialvariate", [5, 1.5], lambda r: r.binomialvariate(5, 1.5)),
    ("expovariate", [0.0], lambda r: r.expovariate(0.0)),
    ("paretovariate", [0.0], lambda r: r.paretovariate(0.0)),
    ("weibullvariate", [1.0, 0.0], lambda r: r.weibullvariate(1.0, 0.0)),
    ("choices", [3, [0, 0, 0], None, 2], lambda r: r.choices(range(3), [0, 0, 0], k=2)),
    ("choices", [3, [1, 1], None, 2], lambda r: r.choices(range(3), [1, 1], k=2)),
    ("lognormvariate", [1000.0, 0.001], lambda r: r.lognormvariate(1000.0, 0.001)),
]


def make_seeded(kind, v):
    r = random.Random()
    r.seed(v)
    return r


def state_end(r):
    ver, st, g = r.getstate()
    return {"idx": st[-1], "sha": hashlib.sha256(",".join(map(str, st[:-1])).encode()).hexdigest(),
            "gauss": None if g is None else fx(g)}


def run(r, name, args, call):
    try:
        return call(r)
    except (ValueError, IndexError, ZeroDivisionError, OverflowError, TypeError):
        return "ERR"


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--seeds", type=int, default=200)
    p.add_argument("--calls", type=int, default=50)
    p.add_argument("--args-seed", type=int, default=20260929)
    p.add_argument("--errors", action="store_true", help="add error cases to seed 0")
    o = p.parse_args()
    a = random.Random(o.args_seed)
    for si, (kind, v) in enumerate(make_seeds(o.seeds, a)):
        r = make_seeded(kind, v)
        names = list(OPS)
        a.shuffle(names)
        ops = []

        def do(name):
            args, call = OPS[name](a)
            ops.append([name, enc(args), enc(run(r, name, args, call))])

        for name in names:
            for _ in range(o.calls):
                do(name)
        for _ in range(o.calls):
            do(a.choice(names))
        if si == 0 or o.errors:
            for name, args, call in ERR_CASES:
                res = run(r, name, args, call)
                assert res == "ERR", (name, args, res)
                ops.append([name, enc(args), "ERR"])
                ops.append(["random", [], enc(r.random())])
        rec = {"seed": seed_json(kind, v), "ops": ops, "end": state_end(r)}
        sys.stdout.write(json.dumps(rec, separators=(",", ":")) + "\n")


if __name__ == "__main__":
    main()
