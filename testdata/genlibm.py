#!/usr/bin/env python3
"""Record libm results from the local CPython for the functions pyrandom
reimplements. Usage: genlibm.py N > libm.txt

Lines are "fn arg [arg2] result" with float.hex() values. Arguments are drawn
from the ranges the random module uses.
"""
import math
import random
import sys

n = int(sys.argv[1])
a = random.Random(99)
h = float.hex
for _ in range(n):
    u = a.random()
    lo = 1.0 - a.random()
    tw = a.random() * math.tau
    pw = a.uniform(-20, 20)
    big = a.uniform(0.001, 50)
    for line in (
        ("log", lo, math.log(lo)),
        ("log", a.uniform(1e-6, 1e6), None),
        ("exp", a.uniform(-30, 30), None),
        ("exp", u, None),
        ("sin", tw, None),
        ("cos", tw, None),
        ("cos", a.uniform(0, math.pi), None),
        ("acos", a.uniform(-1, 1), None),
        ("log2", lo, None),
        ("pow", lo, pw),
        ("pow", big, u + 0.5),
        ("lgamma", a.uniform(1, 3000), None),
        ("lgamma", big, None),
    ):
        fn, x = line[0], line[1]
        if fn == "pow":
            y = line[2]
            print(fn, h(x), h(y), h(x ** y))
        else:
            f = {"log": math.log, "exp": math.exp, "sin": math.sin, "cos": math.cos,
                 "acos": math.acos, "log2": math.log2, "lgamma": math.lgamma}[fn]
            print(fn, h(x), h(f(x)))
