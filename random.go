// Copyright (c) 2026 kofiadeyemiq. Derived in part from CPython (Lib/random.py,
// Modules/_randommodule.c, Modules/mathmodule.c), Copyright (c) 2001-2026 Python
// Software Foundation; see LICENSE-python. The Mersenne Twister is covered by
// LICENSE-mt19937.

// Package pyrandom reproduces the output streams of CPython's random.Random.
//
// A Random seeded with the same value as Python's random.seed produces the
// same integers, the same floats (bit for bit, subject to the libm caveats
// in the README) and the same choices, shuffles and samples, and its state
// can be exchanged with Python's getstate and setstate.
//
// The Mersenne Twister and the seeding follow CPython's
// Modules/_randommodule.c, and the derived methods follow Lib/random.py of
// Python 3.14. Both are covered by the Python Software Foundation license
// (LICENSE-python); the Mersenne Twister by the notice in LICENSE-mt19937.
package pyrandom

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/bits"
)

const (
	mtN = 624
	mtM = 397
)

// Errors returned for arguments on which Python raises an exception.
var (
	// ErrValue corresponds to Python's ValueError.
	ErrValue = errors.New("pyrandom: invalid value")
	// ErrIndex corresponds to Python's IndexError.
	ErrIndex = errors.New("pyrandom: index error")
	// ErrZeroDivision corresponds to Python's ZeroDivisionError.
	ErrZeroDivision = errors.New("pyrandom: division by zero")
	// ErrOverflow corresponds to Python's OverflowError.
	ErrOverflow = errors.New("pyrandom: numerical result out of range")
	// ErrType corresponds to Python's TypeError.
	ErrType = errors.New("pyrandom: unsupported type")
)

func valueErr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrValue, fmt.Sprintf(format, a...))
}

// Random is a Mersenne Twister generator equivalent to Python's
// random.Random. The zero value is not usable; create one with New or
// NewFromOS. A Random is not safe for concurrent use.
type Random struct {
	// NoFMA selects arithmetic without fused multiply-add in the one
	// place where CPython itself does floating point in C outside libm
	// (the log-gamma used by BinomialVariate). CPython built by clang or
	// gcc for arm64 fuses these operations, which is the default here.
	// Set NoFMA for a CPython built without fusing (typically x86-64).
	NoFMA bool

	mt       [mtN]uint32
	idx      int
	gaussVal float64
	hasGauss bool
}

// New returns a Random seeded like random.Random(seed) for an integer seed.
func New(seed int64) *Random {
	r := new(Random)
	r.Seed(seed)
	return r
}

// NewFromOS returns a Random seeded from the operating system's entropy
// source, like random.Random() with no argument.
func NewFromOS() (*Random, error) {
	var b [64]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, fmt.Errorf("pyrandom: reading OS entropy: %w", err)
	}
	r := new(Random)
	r.SeedBytes(b[:])
	return r, nil
}

func (r *Random) initGenrand(s uint32) {
	r.mt[0] = s
	for i := 1; i < mtN; i++ {
		r.mt[i] = 1812433253*(r.mt[i-1]^(r.mt[i-1]>>30)) + uint32(i)
	}
	r.idx = mtN
}

func (r *Random) initByArray(key []uint32) {
	r.initGenrand(19650218)
	i, j := 1, 0
	k := mtN
	if len(key) > k {
		k = len(key)
	}
	for ; k > 0; k-- {
		r.mt[i] = (r.mt[i] ^ ((r.mt[i-1] ^ (r.mt[i-1] >> 30)) * 1664525)) + key[j] + uint32(j)
		i++
		j++
		if i >= mtN {
			r.mt[0] = r.mt[mtN-1]
			i = 1
		}
		if j >= len(key) {
			j = 0
		}
	}
	for k = mtN - 1; k > 0; k-- {
		r.mt[i] = (r.mt[i] ^ ((r.mt[i-1] ^ (r.mt[i-1] >> 30)) * 1566083941)) - uint32(i)
		i++
		if i >= mtN {
			r.mt[0] = r.mt[mtN-1]
			i = 1
		}
	}
	r.mt[0] = 0x80000000
}

func (r *Random) genrand() uint32 {
	if r.idx >= mtN {
		var kk int
		for kk = 0; kk < mtN-mtM; kk++ {
			y := (r.mt[kk] & 0x80000000) | (r.mt[kk+1] & 0x7fffffff)
			r.mt[kk] = r.mt[kk+mtM] ^ (y >> 1) ^ (-(y & 1) & 0x9908b0df)
		}
		for ; kk < mtN-1; kk++ {
			y := (r.mt[kk] & 0x80000000) | (r.mt[kk+1] & 0x7fffffff)
			r.mt[kk] = r.mt[kk+(mtM-mtN)] ^ (y >> 1) ^ (-(y & 1) & 0x9908b0df)
		}
		y := (r.mt[mtN-1] & 0x80000000) | (r.mt[0] & 0x7fffffff)
		r.mt[mtN-1] = r.mt[mtM-1] ^ (y >> 1) ^ (-(y & 1) & 0x9908b0df)
		r.idx = 0
	}
	y := r.mt[r.idx]
	r.idx++
	y ^= y >> 11
	y ^= (y << 7) & 0x9d2c5680
	y ^= (y << 15) & 0xefc60000
	y ^= y >> 18
	return y
}

// Uint32 returns the next raw 32-bit output of the Mersenne Twister.
func (r *Random) Uint32() uint32 { return r.genrand() }

// seedWords seeds from a magnitude given as little-endian 32-bit words, as
// CPython does with abs(int).
func (r *Random) seedWords(w []uint32) {
	for len(w) > 1 && w[len(w)-1] == 0 {
		w = w[:len(w)-1]
	}
	if len(w) == 0 {
		w = []uint32{0}
	}
	r.initByArray(w)
	r.hasGauss = false
	r.gaussVal = 0
}

// Seed seeds the generator like random.seed(n) for an integer n. Negative
// values use their absolute value, as in Python.
func (r *Random) Seed(n int64) {
	var m uint64
	if n < 0 {
		m = -uint64(n)
	} else {
		m = uint64(n)
	}
	r.SeedUint64(m)
}

// SeedUint64 seeds the generator like random.seed(n) for a non-negative n
// that may not fit in an int64.
func (r *Random) SeedUint64(n uint64) {
	r.seedWords([]uint32{uint32(n), uint32(n >> 32)})
}

// SeedBig seeds the generator like random.seed(n) for an integer of any
// size. Negative values use their absolute value.
func (r *Random) SeedBig(n *big.Int) {
	var abs big.Int
	abs.Abs(n)
	r.seedWords(bigWords(&abs))
}

func bigWords(n *big.Int) []uint32 {
	b := n.Bytes() // big-endian magnitude
	w := make([]uint32, (len(b)+3)/4)
	for i := range b {
		pos := len(b) - 1 - i
		w[pos/4] |= uint32(b[i]) << (8 * (pos % 4))
	}
	return w
}

// SeedBytes seeds the generator like random.seed(b) for bytes with seed
// version 2: the SHA-512 digest of b is appended to b and the result is
// read as a big-endian integer.
func (r *Random) SeedBytes(b []byte) {
	sum := sha512.Sum512(b)
	buf := make([]byte, 0, len(b)+len(sum))
	buf = append(buf, b...)
	buf = append(buf, sum[:]...)
	r.SeedBig(new(big.Int).SetBytes(buf))
}

// SeedString seeds the generator like random.seed(s) for a str: the UTF-8
// bytes are seeded as by SeedBytes. Python strings containing lone
// surrogates cannot be encoded and are not representable.
func (r *Random) SeedString(s string) { r.SeedBytes([]byte(s)) }

// SeedFloat seeds the generator like random.seed(f) for a float. CPython
// seeds from hash(f) reinterpreted as an unsigned 64-bit integer (this is
// deprecated in Python but still works in 3.14). A NaN has an address-based
// hash in Python and returns ErrValue.
func (r *Random) SeedFloat(f float64) error {
	if math.IsNaN(f) {
		return valueErr("cannot seed from NaN: its Python hash is not deterministic")
	}
	r.SeedUint64(uint64(hashFloat(f)))
	return nil
}

// hashFloat is CPython's hash(float) (Python/pyhash.c) for 64-bit builds.
func hashFloat(v float64) int64 {
	const modulus = uint64(1)<<61 - 1
	if math.IsInf(v, 0) {
		if v > 0 {
			return 314159
		}
		return -314159
	}
	m, e := math.Frexp(v)
	sign := int64(1)
	if m < 0 {
		sign = -1
		m = -m
	}
	var x uint64
	for m != 0 {
		x = ((x << 28) & modulus) | x>>(61-28)
		m *= 268435456.0
		e -= 28
		y := uint64(m)
		m -= float64(y)
		x += y
		if x >= modulus {
			x -= modulus
		}
	}
	if e >= 0 {
		e %= 61
	} else {
		e = 61 - 1 - ((-1 - e) % 61)
	}
	x = ((x << uint(e)) & modulus) | x>>(61-uint(e))
	h := int64(x) * sign
	if h == -1 {
		h = -2
	}
	return h
}

// Random returns the next float in [0.0, 1.0) with 53 bits of resolution,
// like random.random().
func (r *Random) Random() float64 {
	a := r.genrand() >> 5
	b := r.genrand() >> 6
	return float64(float64(a)*67108864.0+float64(b)) * (1.0 / 9007199254740992.0)
}

// GetRandBits64 returns an integer with k random bits, 0 <= k <= 64, like
// random.getrandbits(k).
func (r *Random) GetRandBits64(k int) (uint64, error) {
	switch {
	case k < 0:
		return 0, valueErr("number of bits must be non-negative")
	case k > 64:
		return 0, valueErr("GetRandBits64: k must be at most 64")
	case k == 0:
		return 0, nil
	case k <= 32:
		return uint64(r.genrand() >> uint(32-k)), nil
	}
	lo := uint64(r.genrand())
	hi := uint64(r.genrand() >> uint(64-k))
	return lo | hi<<32, nil
}

func (r *Random) bits64(k int) uint64 {
	v, _ := r.GetRandBits64(k)
	return v
}

// GetRandBits returns a non-negative integer with k random bits, like
// random.getrandbits(k). It accepts any k >= 0.
func (r *Random) GetRandBits(k int) (*big.Int, error) {
	if k < 0 {
		return nil, valueErr("number of bits must be non-negative")
	}
	if k <= 64 {
		v, _ := r.GetRandBits64(k)
		return new(big.Int).SetUint64(v), nil
	}
	words := (k-1)/32 + 1
	buf := make([]byte, words*4) // little-endian words
	for i := 0; i < words; i++ {
		w := r.genrand()
		if k < 32 {
			w >>= uint(32 - k)
		}
		binary.LittleEndian.PutUint32(buf[i*4:], w)
		k -= 32
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return new(big.Int).SetBytes(buf), nil
}

// RandBytes returns n random bytes, like random.randbytes(n).
func (r *Random) RandBytes(n int) ([]byte, error) {
	if n < 0 {
		return nil, valueErr("number of bytes must be non-negative")
	}
	v, err := r.GetRandBits(n * 8)
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	b := v.Bytes()
	for i := range b {
		out[i] = b[len(b)-1-i]
	}
	return out, nil
}

// RandBelow returns a random integer in [0, n) using CPython's
// _randbelow_with_getrandbits. It returns ErrValue if n is zero.
func (r *Random) RandBelow(n uint64) (uint64, error) {
	if n == 0 {
		return 0, valueErr("RandBelow: n must be positive")
	}
	k := bits.Len64(n)
	v := r.bits64(k)
	for v >= n {
		v = r.bits64(k)
	}
	return v, nil
}

// RandBelowBig is RandBelow for an arbitrarily large n.
func (r *Random) RandBelowBig(n *big.Int) (*big.Int, error) {
	if n.Sign() <= 0 {
		return nil, valueErr("RandBelowBig: n must be positive")
	}
	k := n.BitLen()
	v, _ := r.GetRandBits(k)
	for v.Cmp(n) >= 0 {
		v, _ = r.GetRandBits(k)
	}
	return v, nil
}

// randbelowInt is RandBelow for a positive int that cannot be zero.
func (r *Random) randbelowInt(n int) int {
	v, _ := r.RandBelow(uint64(n))
	return int(v)
}

// RandRange returns a random integer from range(start, stop, step), like
// random.randrange(start, stop, step). Use step 1 for a plain range and
// start 0 for random.randrange(stop).
func (r *Random) RandRange(start, stop, step int64) (int64, error) {
	if step == 1 {
		if stop <= start {
			return 0, valueErr("empty range in randrange(%d, %d)", start, stop)
		}
		width := uint64(stop) - uint64(start) // exact modulo 2**64
		v, _ := r.RandBelow(width)
		return int64(uint64(start) + v), nil
	}
	res, err := r.RandRangeBig(big.NewInt(start), big.NewInt(stop), big.NewInt(step))
	if err != nil {
		return 0, err
	}
	if !res.IsInt64() {
		return 0, fmt.Errorf("%w: result does not fit in int64", ErrOverflow)
	}
	return res.Int64(), nil
}

// RandRangeBig is RandRange for arbitrarily large integers.
func (r *Random) RandRangeBig(start, stop, step *big.Int) (*big.Int, error) {
	width := new(big.Int).Sub(stop, start)
	var n *big.Int
	switch step.Sign() {
	case 0:
		return nil, valueErr("zero step for randrange()")
	case 1:
		// n = (width + step - 1) // step
		n = new(big.Int).Add(width, step)
		n.Sub(n, big.NewInt(1))
		n = floorDiv(n, step)
	default:
		// n = (width + step + 1) // step
		n = new(big.Int).Add(width, step)
		n.Add(n, big.NewInt(1))
		n = floorDiv(n, step)
	}
	if n.Sign() <= 0 {
		return nil, valueErr("empty range in randrange(%v, %v, %v)", start, stop, step)
	}
	v, err := r.RandBelowBig(n)
	if err != nil {
		return nil, err
	}
	v.Mul(v, step)
	return v.Add(v, start), nil
}

func floorDiv(a, b *big.Int) *big.Int {
	q, m := new(big.Int).DivMod(a, b, new(big.Int)) // Euclidean: m >= 0
	if m.Sign() != 0 && b.Sign() < 0 {
		q.Sub(q, big.NewInt(1)) // Euclidean remainder is >= 0; Python's has the sign of b
	}
	return q
}

// RandInt returns a random integer in [a, b], like random.randint(a, b).
func (r *Random) RandInt(a, b int64) (int64, error) {
	if b < a {
		return 0, valueErr("empty range in randint(%d, %d)", a, b)
	}
	width := uint64(b) - uint64(a) + 1
	if width == 0 { // the full 64-bit range: width is 2**64
		res, err := r.RandIntBig(big.NewInt(a), big.NewInt(b))
		if err != nil {
			return 0, err
		}
		return res.Int64(), nil
	}
	v, _ := r.RandBelow(width)
	return int64(uint64(a) + v), nil
}

// RandIntBig is RandInt for arbitrarily large integers.
func (r *Random) RandIntBig(a, b *big.Int) (*big.Int, error) {
	if b.Cmp(a) < 0 {
		return nil, valueErr("empty range in randint(%v, %v)", a, b)
	}
	n := new(big.Int).Sub(b, a)
	n.Add(n, big.NewInt(1))
	v, err := r.RandBelowBig(n)
	if err != nil {
		return nil, err
	}
	return v.Add(v, a), nil
}
