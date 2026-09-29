# pyrandom

A Go implementation of CPython's `random.Random` that produces the same streams for the same seed.

```go
r := pyrandom.New(42)             // random.seed(42)
r.Random()                        // 0.6394267984578837, as in Python
n, _ := r.RandInt(1, 6)           // random.randint(1, 6)
xs := []string{"a", "b", "c"}
pyrandom.Shuffle(r, xs)           // random.shuffle(xs)
picked, _ := pyrandom.Sample(r, xs, 2)
state, _ := r.MarshalState()      // JSON that Python's setstate accepts (see Reference)
```

## Why

Go programs that must agree with a Python program on a seeded sequence (test fixtures, reproducible simulations, ports, CTF and reverse-engineering work) need the same Mersenne Twister seeding, the same bit extraction and the same derived algorithms. The Go standard library does not provide them.

- `math/rand` and `math/rand/v2` use other generators and other algorithms.
- `github.com/seehuhn/mt19937` implements the 64-bit Mersenne Twister, and CPython uses the 32-bit one seeded through `init_by_array`, so its streams differ.
- `github.com/balpha/gopyrand` reproduces the Python 2 generator, and its readme says Python 2 and 3 streams differ. It has no `getrandbits`-based `randrange`, `choice`, `shuffle`, `sample`, distributions or state exchange.
- `github.com/goark/mt` and `github.com/bluele/randutil` were checked by their documented API and do not offer CPython-compatible streams (randutil is described as "inspired by" Python).
- A GitHub code search for `_randbelow`, `getrandbits` and `init_by_array` in Go finds hand-written copies inside unrelated applications, none published as an importable package.

Measured: the differential test against the local CPython 3.14.7 (200 seeds, 50 calls of each of 23 operations, plus 50 interleaved calls per seed):

```
go test -run TestLivePython -count=1 -v .
200 seeds, 247600 operations, 0 final-state mismatches, 4 cached-gauss mismatches
```

Every integer, ordering and float result matches exactly except the operations that call `sin`, `cos`, `acos` or other libm functions, where a few percent of results differ in the last bit. See Limitations for the counts.

## Installation

```sh
go get github.com/__GH_OWNER__/pyrandom
```

Go 1.24 or later. The package uses only the standard library.

## Usage

### Seeding

```go
r := pyrandom.New(12345)                 // int seed
r.SeedBig(bigInt)                        // any size; negative values use abs()
r.SeedString("hello")                    // random.seed("hello"), version 2
r.SeedBytes([]byte{0, 1, 2})             // random.seed(b"\x00\x01\x02")
err := r.SeedFloat(1.5)                  // random.seed(1.5), seeds from hash(1.5)
r, err = pyrandom.NewFromOS()            // random.Random(): OS entropy
```

### Integers and sequences

```go
bits, _ := r.GetRandBits(200)            // *big.Int
v, _ := r.RandRange(10, 100, 7)          // random.randrange(10, 100, 7)
i, _ := r.RandBelow(1000)
c, _ := pyrandom.Choice(r, items)
w, _ := pyrandom.Choices(r, items, 5, []float64{1, 2, 3})
cw, _ := pyrandom.ChoicesCum(r, items, 5, []float64{1, 3, 6})
s, _ := pyrandom.Sample(r, items, 3)
```

Sequences are generic functions because Go methods cannot have type parameters. The index-level methods (`ChoiceIndex`, `ChoicesIndices`, `SampleIndices`, `Shuffle(n, swap)`) work without a slice.

### Distributions

```go
r.Gauss(0, 1)
r.Uniform(1, 2)
x, err := r.GammaVariate(2.5, 1)
k, err := r.BinomialVariate(100, 0.3)
```

### Exchanging state with Python

```go
data, _ := r.MarshalState()
// {"version":3,"state":[... 624 words ..., 624],"gauss_next":null}
```

```python
import json, random
d = json.loads(data)
random.setstate((d["version"], tuple(d["state"]), d["gauss_next"]))
```

The reverse direction is `json.dumps({"version": v, "state": list(s), "gauss_next": g})` from `v, s, g = random.getstate()`, passed to `UnmarshalState`.

## Reference

Errors wrap sentinel values that mirror Python exceptions: `ErrValue` (ValueError), `ErrIndex` (IndexError), `ErrZeroDivision`, `ErrOverflow`, `ErrType`. A call that raises in Python consumes the same random numbers before failing as Python does.

| Function | Python equivalent |
| --- | --- |
| `New(seed int64)`, `NewFromOS()` | `random.Random(seed)`, `random.Random()` |
| `Seed`, `SeedUint64`, `SeedBig`, `SeedBytes`, `SeedString`, `SeedFloat` | `seed(a)` (version 2) |
| `Random()` | `random()` |
| `GetRandBits(k) *big.Int`, `GetRandBits64(k)` | `getrandbits(k)` |
| `RandBytes(n)` | `randbytes(n)` |
| `RandBelow(n)`, `RandBelowBig(n)` | `_randbelow(n)` |
| `RandRange(start, stop, step)`, `RandRangeBig` | `randrange(start, stop, step)` |
| `RandInt(a, b)`, `RandIntBig` | `randint(a, b)` |
| `Choice(r, seq)`, `ChoiceIndex(n)` | `choice(seq)` |
| `Choices(r, pop, k, weights)`, `ChoicesCum(r, pop, k, cum)` | `choices(pop, weights, k=k)`, `choices(pop, cum_weights=cum, k=k)` |
| `Shuffle(r, x)`, `(*Random).Shuffle(n, swap)` | `shuffle(x)` |
| `Sample(r, pop, k)`, `SampleCounts(r, pop, counts, k)`, `SampleIndices(n, k)` | `sample(pop, k)`, `sample(pop, k, counts=counts)`, `sample(range(n), k)` |
| `Uniform`, `Triangular`, `TriangularMode` | `uniform`, `triangular(low, high)`, `triangular(low, high, mode)` |
| `Gauss`, `NormalVariate`, `LogNormVariate` | `gauss`, `normalvariate`, `lognormvariate` |
| `ExpoVariate`, `VonMisesVariate`, `GammaVariate`, `BetaVariate` | `expovariate`, `vonmisesvariate`, `gammavariate`, `betavariate` |
| `ParetoVariate`, `WeibullVariate`, `BinomialVariate` | `paretovariate`, `weibullvariate`, `binomialvariate` |
| `GetState()`, `SetState(State)`, `MarshalState()`, `UnmarshalState(data)` | `getstate()`, `setstate(state)` |

Field:

| Name | Type | Default | Description |
| --- | --- | --- | --- |
| `NoFMA` | `bool` | `false` | Compute the log-gamma inside `BinomialVariate` without fused multiply-add. Set it when the CPython being matched was built without FMA contraction. |

## How it works

- The generator is the 32-bit MT19937 of `_randommodule.c`. Integer seeds use `abs(n)` split into little-endian 32-bit words and fed to `init_by_array`. String and bytes seeds append their SHA-512 digest and read the result as a big-endian integer. Float seeds use `hash(f)` reinterpreted as an unsigned 64-bit integer, which is what CPython 3.14 still does.
- `Random()` combines `genrand>>5` and `genrand>>6`. `GetRandBits` takes the top bits of each 32-bit word, low word first. `RandBelow` is `_randbelow_with_getrandbits`.
- The derived methods follow `Lib/random.py` of Python 3.14, including the pool and set branches of `sample` (chosen from `21 + 4**ceil(log(3k, 4))`), the `bisect_right` selection of `choices`, and the two-value cache of `gauss`.
- Floating-point expressions are written in the same order as the Python source. Each product that feeds a sum is wrapped in `float64()`, because the Go compiler otherwise fuses `a*b + c` into an FMA on arm64 and the results would depend on the architecture. Running the test suite with `GOARCH=amd64` on an arm64 machine gives the same results.
- Go's `math.Log`, `Exp`, `Pow`, `Sin`, `Cos` and `Acos` are not correctly rounded and disagree with the platform libm on between 4% and 54% of random arguments (table below). `mathx.go` evaluates `log`, `log2`, `exp`, `pow`, `sin`, `cos` and `acos` in double-double arithmetic and rounds once, which lands on the same double as a good libm on almost all arguments.
- CPython computes `lgamma` itself (a Lanczos approximation in `mathmodule.c`) and does not call libm for it. The port reproduces that code, including the fused multiply-adds an arm64 build performs.

Measured against the libm of the CPython used for testing (`go test -run TestLibmLive -v .`, 100,000 to 200,000 random inputs per function in the ranges the random module uses):

```
acos   pyrandom differs 14.533%   math.Acos differs 22.564%
cos    pyrandom differs  4.524%   math.Cos  differs 28.692%
exp    pyrandom differs  0.164%   math.Exp  differs  9.280%
lgamma pyrandom differs  0.019%   math.Lgamma differs 40.926%
log    pyrandom differs  0.046%   math.Log  differs  3.903%
log2   pyrandom differs  0.074%   math.Log2 differs 17.876%
pow    pyrandom differs  0.122%   math.Pow  differs 53.786%
sin    pyrandom differs  3.720%   math.Sin  differs 23.261%
```

## Limitations

- Bit-identical floats hold only where the reference libm rounds correctly. That libm was macOS arm64 (Apple Silicon, CPython 3.14.7). Its `sin`, `cos` and `acos` are not correctly rounded (the exact value lies within 0.494 ulp of a tie in cases where it rounds the other way), so `Gauss` differs from CPython in the last bit for about 2% of calls, `VonMisesVariate` for about 2%, and `gammavariate`, `betavariate` and `weibullvariate` for under 0.5%. A CPython on glibc or on another libm will have a different set of last-bit differences. They were not measured.
- `Random`, `GetRandBits`, `RandBelow`, `RandRange`, `RandInt`, `RandBytes`, `Choice`, `Choices`, `Shuffle`, `Sample`, `SampleCounts`, `Uniform` and `Triangular` use no libm function and matched on every recorded call. `NormalVariate`, `BinomialVariate` and `ExpoVariate` use `log` and had 0, 0 and under 0.1% differing calls in the runs. A last-bit difference can in principle flip an accept/reject decision in a rejection loop and desynchronize the stream. None occurred in about 1.9 million randomized operations (all final generator states matched), but the probability is not zero.
- `NoFMA` is a guess for non-arm64 builds. Only an FMA-contracting CPython (arm64 macOS) was tested.
- Integer weights in `Choices` are `float64`. Python adds integer weights exactly, so sums above 2^53 can differ.
- `BinomialVariate` takes `int64` and converts `n` to `float64`. Python uses arbitrary-size integers there.
- `SeedFloat` returns an error for NaN, whose Python hash depends on the object's address. Seed version 1 (`random.seed(a, version=1)`) and the removed `hash()`-based `str` seeding are not implemented. Strings are seeded from their bytes, so a Python `str` containing lone surrogates has no equivalent.
- `SetState` rejects version 3 words outside 0 to 2^32-1, where CPython truncates silently.
- `Random` is not safe for concurrent use.
- Speed: the correctly rounded math costs about 240 ns per `log` (Go's `math.Log`: 2.6 ns). `Gauss` takes about 970 ns per call on an Apple M5 Pro, against 108 ns for CPython's `random.gauss` measured on the same machine. `Random()` takes 4.9 ns (CPython: 11 ns).
- Not covered: `random.SystemRandom`, Python's type errors for wrong argument types, and subclassing hooks.

## Compatibility

Run here: Go 1.27.1 on darwin/arm64 (Apple M5 Pro), CPython 3.14.7. `GOARCH=amd64` (Rosetta) was run for the fixture test. The fixtures hold CPython 3.14 results. Other CPython versions were not run; `BinomialVariate` does not exist before 3.12. The CI workflow runs `go build`, `go vet` and `go test -race` on Linux with the current Go release; that run is not part of the tested results above.

## Development

```sh
go build ./...
go vet ./...
go test -race -count=1 ./...
gofmt -l .
go test -run TestLivePython -v .      # needs python3 3.14 on PATH; skips otherwise
go test -run '^$' -fuzz '^FuzzMath$' -fuzztime 60s .
```

`testdata/gen.py` produces the operation fixtures and `testdata/genlibm.py` the libm samples. The committed `testdata/fixture.jsonl.gz` was made with `python3 testdata/gen.py --seeds 200 --calls 8 | gzip -9`, and `testdata/libm.txt.gz` with `python3 testdata/genlibm.py 2000 | gzip -9`. `PYRANDOM_LIVE_SEEDS` and `PYRANDOM_LIVE_ARGSEED` vary the live run.

## License

MIT, see [LICENSE](LICENSE). The Mersenne Twister and parts of the algorithms are derived from CPython (Python Software Foundation License, [LICENSE-python](LICENSE-python)) and from the Mersenne Twister reference implementation (BSD, [LICENSE-mt19937](LICENSE-mt19937)).
