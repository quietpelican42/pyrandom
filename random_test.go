package pyrandom

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// Reference values below were printed by CPython 3.14.7.

func TestKnownStreams(t *testing.T) {
	tests := []struct {
		name string
		seed func(*Random)
		want []float64
	}{
		{"int 42", func(r *Random) { r.Seed(42) }, []float64{0.6394267984578837, 0.025010755222666936, 0.27502931836911926}},
		{"negative uses abs", func(r *Random) { r.Seed(-42) }, []float64{0.6394267984578837}},
		{"str abc", func(r *Random) { r.SeedString("abc") }, []float64{0.7720246314157545}},
		{"bytes abc", func(r *Random) { r.SeedBytes([]byte("abc")) }, []float64{0.7720246314157545}},
		{"big int", func(r *Random) { r.SeedBig(new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 100), big.NewInt(5))) }, []float64{0.7033503239395905}},
		{"float 1.5", func(r *Random) {
			if err := r.SeedFloat(1.5); err != nil {
				t.Fatal(err)
			}
		}, []float64{0.551763726942059}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := new(Random)
			tc.seed(r)
			for i, w := range tc.want {
				if got := r.Random(); math.Float64bits(got) != math.Float64bits(w) {
					t.Fatalf("draw %d = %v, want %v", i, got, w)
				}
			}
		})
	}
}

func TestMixedMethods(t *testing.T) {
	r := New(42)
	bits, _ := r.GetRandBits(100)
	if want := "873491343714207852616756591005"; bits.String() != want {
		t.Errorf("GetRandBits(100) = %v, want %s", bits, want)
	}
	if v, _ := r.RandInt(1, 6); v != 3 {
		t.Errorf("RandInt(1,6) = %d, want 3", v)
	}
	if g := r.Gauss(0, 1); g != 0.017593105583573694 {
		t.Errorf("Gauss = %v, want 0.017593105583573694", g)
	}

	r = New(3)
	got, err := Choices(r, []string{"a", "b", "c"}, 5, []float64{1, 2, 3})
	if err != nil || !reflect.DeepEqual(got, []string{"b", "c", "b", "c", "c"}) {
		t.Errorf("Choices = %v, %v", got, err)
	}
	s, err := Sample(r, seqInts(100), 5)
	if err != nil || !reflect.DeepEqual(s, []int{8, 77, 1, 60, 33}) {
		t.Errorf("Sample = %v, %v", s, err)
	}
	if k, err := r.BinomialVariate(20, 0.3); err != nil || k != 7 {
		t.Errorf("BinomialVariate = %d, %v; want 7", k, err)
	}

	x := seqInts(10)
	Shuffle(New(5), x)
	if want := []int{2, 3, 1, 0, 8, 7, 6, 5, 4, 9}; !reflect.DeepEqual(x, want) {
		t.Errorf("Shuffle = %v, want %v", x, want)
	}
}

func seqInts(n int) []int {
	x := make([]int, n)
	for i := range x {
		x[i] = i
	}
	return x
}

func TestInt64MatchesBig(t *testing.T) {
	cases := [][3]int64{
		{0, 10, 1}, {-5, 5, 1}, {3, 100, 7}, {100, 3, -7}, {0, 1 << 62, 1},
		{math.MinInt64, math.MaxInt64, 1}, {math.MinInt64, math.MaxInt64, 3},
		{math.MaxInt64 - 3, math.MaxInt64, 1}, {-1 << 40, 1 << 40, 12345},
	}
	for _, c := range cases {
		a, b := New(7), New(7)
		for i := 0; i < 20; i++ {
			g, gerr := a.RandRange(c[0], c[1], c[2])
			w, werr := b.RandRangeBig(big.NewInt(c[0]), big.NewInt(c[1]), big.NewInt(c[2]))
			if (gerr != nil) != (werr != nil) || (gerr == nil && big.NewInt(g).Cmp(w) != 0) {
				t.Fatalf("RandRange%v draw %d: %d,%v vs %v,%v", c, i, g, gerr, w, werr)
			}
		}
	}
	a, b := New(9), New(9)
	for i := 0; i < 50; i++ {
		g, _ := a.RandInt(math.MinInt64, math.MaxInt64)
		w, _ := b.RandIntBig(big.NewInt(math.MinInt64), big.NewInt(math.MaxInt64))
		if big.NewInt(g).Cmp(w) != 0 {
			t.Fatalf("full-range RandInt draw %d: %d vs %v", i, g, w)
		}
	}
}

func TestErrors(t *testing.T) {
	r := New(1)
	if _, err := r.RandRange(5, 5, 1); !errors.Is(err, ErrValue) {
		t.Errorf("empty range: %v", err)
	}
	if _, err := r.RandRange(0, 5, 0); !errors.Is(err, ErrValue) {
		t.Errorf("zero step: %v", err)
	}
	if _, err := r.RandInt(3, 2); !errors.Is(err, ErrValue) {
		t.Errorf("randint: %v", err)
	}
	if _, err := r.GetRandBits(-1); !errors.Is(err, ErrValue) {
		t.Errorf("getrandbits: %v", err)
	}
	if _, err := Choice(r, []int(nil)); !errors.Is(err, ErrIndex) {
		t.Errorf("choice: %v", err)
	}
	if _, err := Sample(r, seqInts(3), 4); !errors.Is(err, ErrValue) {
		t.Errorf("sample: %v", err)
	}
	if _, err := r.GammaVariate(0, 1); !errors.Is(err, ErrValue) {
		t.Errorf("gamma: %v", err)
	}
	if _, err := r.ExpoVariate(0); !errors.Is(err, ErrZeroDivision) {
		t.Errorf("expovariate: %v", err)
	}
	if _, err := r.LogNormVariate(1000, 0.001); !errors.Is(err, ErrOverflow) {
		t.Errorf("lognorm: %v", err)
	}
	if _, err := r.BinomialVariate(-1, 0.5); !errors.Is(err, ErrValue) {
		t.Errorf("binomial: %v", err)
	}
	if err := r.SeedFloat(math.NaN()); !errors.Is(err, ErrValue) {
		t.Errorf("SeedFloat(NaN): %v", err)
	}
	if _, err := Choices(r, seqInts(3), 2, []float64{0, 0, 0}); !errors.Is(err, ErrValue) {
		t.Errorf("zero weights: %v", err)
	}
}

func TestGaussCache(t *testing.T) {
	r := New(11)
	r.Gauss(0, 1)
	if st := r.GetState(); st.GaussNext == nil {
		t.Fatal("first Gauss did not cache a second value")
	}
	r.Gauss(0, 1)
	if st := r.GetState(); st.GaussNext != nil {
		t.Fatal("second Gauss did not consume the cached value")
	}
	r.Gauss(0, 1)
	r.Seed(11)
	if st := r.GetState(); st.GaussNext != nil {
		t.Fatal("Seed did not clear the cached value")
	}
}

func TestStateRoundTrip(t *testing.T) {
	r := New(5)
	r.Gauss(0, 1)
	for i := 0; i < 700; i++ {
		r.Random()
	}
	data, err := r.MarshalState()
	if err != nil {
		t.Fatal(err)
	}
	c := new(Random)
	if err := c.UnmarshalState(data); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if a, b := r.Random(), c.Random(); a != b {
			t.Fatalf("draw %d: %v != %v", i, a, b)
		}
	}
	if a, b := r.Gauss(0, 1), c.Gauss(0, 1); a != b {
		t.Fatalf("cached gauss differs: %v %v", a, b)
	}
}

func TestSetStateRejects(t *testing.T) {
	good := New(1).GetState()
	mut := func(f func(*State)) State {
		s := State{Version: good.Version, State: append([]int64(nil), good.State...)}
		f(&s)
		return s
	}
	bad := map[string]State{
		"version":   mut(func(s *State) { s.Version = 1 }),
		"short":     mut(func(s *State) { s.State = s.State[:10] }),
		"index":     mut(func(s *State) { s.State[624] = 625 }),
		"neg index": mut(func(s *State) { s.State[624] = -1 }),
		"word":      mut(func(s *State) { s.State[3] = 1 << 32 }),
	}
	for name, s := range bad {
		if err := new(Random).SetState(s); !errors.Is(err, ErrValue) {
			t.Errorf("%s: SetState error = %v, want ErrValue", name, err)
		}
	}
	v2 := mut(func(s *State) { s.Version = 2; s.State[0] = -1 })
	r := new(Random)
	if err := r.SetState(v2); err != nil || r.mt[0] != math.MaxUint32 {
		t.Errorf("version 2 negative word: %v, mt[0]=%d", err, r.mt[0])
	}
	if err := new(Random).UnmarshalState([]byte(`{"version":3,`)); err == nil {
		t.Error("truncated JSON accepted")
	}
}

// TestStateAgainstPython exchanges a state through JSON with the local
// CPython in both directions.
func TestStateAgainstPython(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found on PATH; install CPython to run the state exchange test")
	}
	const script = `
import json, random, sys
r = random.Random(7)
r.gauss(0, 1)
for _ in range(300): r.random()
v, s, g = r.getstate()
sys.stdout.write(json.dumps({"state": {"version": v, "state": list(s), "gauss_next": g},
    "next": [r.random() for _ in range(5)], "gauss": r.gauss(0, 1)}))
`
	out, err := exec.Command(py, "-c", script).Output()
	if err != nil {
		t.Skipf("python3 failed: %v", err)
	}
	var got struct {
		State json.RawMessage `json:"state"`
		Next  []float64       `json:"next"`
		Gauss float64         `json:"gauss"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	r := new(Random)
	if err := r.UnmarshalState(got.State); err != nil {
		t.Fatal(err)
	}
	for i, w := range got.Next {
		if v := r.Random(); v != w {
			t.Fatalf("draw %d after Python state: %v, want %v", i, v, w)
		}
	}
	if g := r.Gauss(0, 1); g != got.Gauss {
		t.Errorf("cached gauss = %v, want %v", g, got.Gauss)
	}

	// Go to Python.
	ours := New(7)
	ours.Gauss(0, 1)
	data, _ := ours.MarshalState()
	cmd := exec.Command(py, "-c", `
import json, random, sys
d = json.load(sys.stdin)
r = random.Random(); r.setstate((d["version"], tuple(d["state"]), d["gauss_next"]))
sys.stdout.write(json.dumps([r.random(), r.gauss(0, 1)]))`)
	cmd.Stdin = strings.NewReader(string(data))
	res, err := cmd.Output()
	if err != nil {
		t.Fatalf("Python rejected our state: %v", err)
	}
	var pv []float64
	if err := json.Unmarshal(res, &pv); err != nil {
		t.Fatal(err)
	}
	if a, b := ours.Random(), ours.Gauss(0, 1); a != pv[0] || b != pv[1] {
		t.Errorf("after handing state to Python: %v %v vs %v", a, b, pv)
	}
}

func TestSampleStrategies(t *testing.T) {
	// n <= setsize uses the pool, larger n uses the set; both must return
	// distinct values and agree with the recorded fixture (see TestFixture).
	r := New(3)
	for _, c := range [][2]int{{5, 5}, {21, 3}, {22, 3}, {100, 30}, {100000, 50}} {
		s, err := r.SampleIndices(c[0], c[1])
		if err != nil {
			t.Fatal(err)
		}
		seen := map[int]bool{}
		for _, v := range s {
			if v < 0 || v >= c[0] || seen[v] {
				t.Fatalf("SampleIndices(%d,%d) = %v", c[0], c[1], s)
			}
			seen[v] = true
		}
	}
}

func TestPow4Ceil(t *testing.T) {
	// 4 ** ceil(log(x, 4)) as printed by CPython, including exact powers.
	for x, want := range map[int]int{3: 4, 6: 16, 12: 16, 18: 64, 48: 64, 63: 64, 64: 64, 65: 256, 192: 256, 193: 256, 3000: 4096, 262144: 262144} {
		if got := pow4Ceil(x); got != want {
			t.Errorf("pow4Ceil(%d) = %d, want %d", x, got, want)
		}
	}
}

func BenchmarkRandom(b *testing.B) {
	r := New(1)
	for i := 0; i < b.N; i++ {
		r.Random()
	}
}

func BenchmarkGauss(b *testing.B) {
	r := New(1)
	for i := 0; i < b.N; i++ {
		r.Gauss(0, 1)
	}
}

func BenchmarkGauss_GoMath(b *testing.B) {
	r := New(1)
	for i := 0; i < b.N; i++ {
		x := r.Random() * twoPi
		_ = math.Cos(x) * math.Sqrt(-2*math.Log(1-r.Random()))
	}
}

func BenchmarkRandBelow(b *testing.B) {
	r := New(1)
	for i := 0; i < b.N; i++ {
		r.RandBelow(1000)
	}
}

func BenchmarkGammaVariate(b *testing.B) {
	r := New(1)
	for i := 0; i < b.N; i++ {
		r.GammaVariate(2.5, 1)
	}
}

func BenchmarkLog(b *testing.B) {
	for i := 0; i < b.N; i++ {
		log(1.2345 + float64(i&1023))
	}
}

func BenchmarkMathLog(b *testing.B) {
	for i := 0; i < b.N; i++ {
		math.Log(1.2345 + float64(i&1023))
	}
}
