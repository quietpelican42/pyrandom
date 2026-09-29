package pyrandom

import (
	"math"
	"math/big"
	"testing"
)

func FuzzRandBelowAgreesWithBig(f *testing.F) {
	f.Add(int64(1), uint64(10))
	f.Add(int64(-7), uint64(1<<63))
	f.Add(int64(0), uint64(math.MaxUint64))
	f.Fuzz(func(t *testing.T, seed int64, n uint64) {
		if n == 0 {
			return
		}
		a, b := New(seed), New(seed)
		for i := 0; i < 8; i++ {
			g, err := a.RandBelow(n)
			if err != nil || g >= n {
				t.Fatalf("RandBelow(%d) = %d, %v", n, g, err)
			}
			w, err := b.RandBelowBig(new(big.Int).SetUint64(n))
			if err != nil || w.Uint64() != g {
				t.Fatalf("RandBelow(%d) = %d but RandBelowBig = %v", n, g, w)
			}
		}
	})
}

func FuzzRandRange(f *testing.F) {
	f.Add(int64(1), int64(0), int64(100), int64(3))
	f.Add(int64(2), int64(100), int64(-100), int64(-7))
	f.Add(int64(3), int64(math.MinInt64), int64(math.MaxInt64), int64(1))
	f.Fuzz(func(t *testing.T, seed, start, stop, step int64) {
		a, b := New(seed), New(seed)
		g, gerr := a.RandRange(start, stop, step)
		w, werr := b.RandRangeBig(big.NewInt(start), big.NewInt(stop), big.NewInt(step))
		if (gerr != nil) != (werr != nil) {
			t.Fatalf("errors differ: %v vs %v", gerr, werr)
		}
		if gerr != nil {
			return
		}
		if big.NewInt(g).Cmp(w) != 0 {
			t.Fatalf("RandRange(%d,%d,%d) = %d, big says %v", start, stop, step, g, w)
		}
		// The result must lie in the range.
		diff := new(big.Int).Sub(big.NewInt(g), big.NewInt(start))
		if new(big.Int).Rem(diff, big.NewInt(step)).Sign() != 0 {
			t.Fatalf("%d is not on the range(%d,%d,%d) grid", g, start, stop, step)
		}
		if (step > 0 && (g < start || g >= stop)) || (step < 0 && (g > start || g <= stop)) {
			t.Fatalf("%d outside range(%d,%d,%d)", g, start, stop, step)
		}
	})
}

// FuzzGetRandBits checks that k-bit results are below 2**k and that the
// 64-bit and big-integer paths agree.
func FuzzGetRandBits(f *testing.F) {
	f.Add(int64(5), 0)
	f.Add(int64(5), 33)
	f.Add(int64(5), 64)
	f.Add(int64(5), 65)
	f.Fuzz(func(t *testing.T, seed int64, k int) {
		if k < 0 || k > 4096 {
			return
		}
		v, err := New(seed).GetRandBits(k)
		if err != nil || v.BitLen() > k {
			t.Fatalf("GetRandBits(%d) = %v, %v", k, v, err)
		}
		if k <= 64 {
			w, _ := New(seed).GetRandBits64(k)
			if v.Uint64() != w {
				t.Fatalf("GetRandBits(%d) %v != GetRandBits64 %d", k, v, w)
			}
		}
	})
}

func FuzzUnmarshalState(f *testing.F) {
	good, _ := New(1).MarshalState()
	f.Add(good)
	f.Add([]byte(`{"version":3,"state":[],"gauss_next":null}`))
	f.Add([]byte(`{"version":2,"state":[1,2,3],"gauss_next":1.5}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		r := new(Random)
		if err := r.UnmarshalState(data); err != nil {
			return
		}
		// An accepted state must round-trip and keep producing values.
		out, err := r.MarshalState()
		if err != nil {
			t.Fatal(err)
		}
		c := new(Random)
		if err := c.UnmarshalState(out); err != nil {
			t.Fatalf("re-reading own output: %v", err)
		}
		if r.Random() != c.Random() {
			t.Fatal("round-tripped state diverged")
		}
	})
}

// FuzzMath checks the reimplemented functions against Go's math package:
// they must stay within a few ulp of it, which catches gross errors such as a
// wrong argument reduction on inputs no fixture contains.
func FuzzMath(f *testing.F) {
	f.Add(0.5, 2.0)
	f.Add(1e-300, 700.0)
	f.Add(3.14159, -0.999)
	f.Fuzz(func(t *testing.T, x, y float64) {
		near := func(name string, got, want float64, tol ...int64) {
			max := int64(2)
			if len(tol) > 0 {
				max = tol[0]
			}
			if math.IsNaN(want) || math.IsInf(want, 0) || want == 0 {
				return
			}
			if ulpDiff(got, want) > max {
				t.Fatalf("%s(%v, %v) = %v, math says %v", name, x, y, got, want)
			}
		}
		if x > 0 && x < math.MaxFloat64 {
			near("log", log(x), math.Log(x))
			// math.Log2 loses many ulps near 1 (it adds an exponent to a
			// log of the mantissa), so compare it away from 1 only.
			if math.Abs(x-1) > 0.5 {
				near("log2", log2(x), math.Log2(x))
			}
			if math.Abs(y) < 50 {
				// math.Pow is off by up to |y*log(x)| ulp.
				near("pow", pow(x, y), math.Pow(x, y), 4096)
			}
		}
		if math.Abs(x) < 700 {
			near("exp", exp(x), math.Exp(x))
		}
		if math.Abs(x) < 1e4 {
			// Skip results that are tiny by cancellation: ulps are meaningless there.
			if math.Abs(math.Sin(x)) > 1e-3 {
				near("sin", sin(x), math.Sin(x))
			}
			if math.Abs(math.Cos(x)) > 1e-3 {
				near("cos", cos(x), math.Cos(x))
			}
		}
		if y > -1 && y < 1 && math.Abs(y) < 0.999999 {
			// math.Acos loses hundreds of ulp near +-1, so check the
			// inverse relation instead.
			if a := acos(y); math.Abs(math.Cos(a)-y) > 4e-16 || a < 0 || a > math.Pi {
				t.Fatalf("acos(%v) = %v does not invert cos", y, a)
			}
		}
	})
}
