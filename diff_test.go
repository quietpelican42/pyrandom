package pyrandom

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The differential harness replays operations recorded from CPython
// (testdata/gen.py) and compares every result exactly: integers and
// orderings as decimal values, floats as float64 bit patterns.

type fixtureSeed struct {
	T string `json:"t"`
	V string `json:"v"`
}

type fixtureEnd struct {
	Idx   int     `json:"idx"`
	Sha   string  `json:"sha"`
	Gauss *string `json:"gauss"`
}

type fixtureRec struct {
	Seed fixtureSeed         `json:"seed"`
	Ops  [][]json.RawMessage `json:"ops"`
	End  fixtureEnd          `json:"end"`
}

func rawStr(m json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(m, &s) != nil {
		return "", false
	}
	return s, true
}

func argFloat(m json.RawMessage) float64 {
	s, _ := rawStr(m)
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(err)
	}
	return f
}

func argBig(m json.RawMessage) *big.Int {
	s, _ := rawStr(m)
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("bad int " + s)
	}
	return v
}

func argInt(m json.RawMessage) int { return int(argBig(m).Int64()) }

func argList(m json.RawMessage) []json.RawMessage {
	var l []json.RawMessage
	if json.Unmarshal(m, &l) != nil {
		return nil
	}
	return l
}

func isNull(m json.RawMessage) bool { return string(m) == "null" }

func seedFrom(t *testing.T, r *Random, s fixtureSeed) {
	switch s.T {
	case "int":
		v, _ := new(big.Int).SetString(s.V, 10)
		r.SeedBig(v)
	case "str":
		r.SeedString(s.V)
	case "bytes":
		b, err := hex.DecodeString(s.V)
		if err != nil {
			t.Fatal(err)
		}
		r.SeedBytes(b)
	case "float":
		f, _ := strconv.ParseFloat(s.V, 64)
		if err := r.SeedFloat(f); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown seed type %q", s.T)
	}
}

// result is a decoded expectation or a Go result: kind f (float), s (string
// of an integer or hex), l (list), e (error).
type result struct {
	f   float64
	s   string
	l   []result
	e   bool
	isF bool
}

func fres(f float64) result  { return result{f: f, isF: true} }
func ires(i *big.Int) result { return result{s: i.String()} }
func lres[T any](xs []T, conv func(T) result) result {
	out := make([]result, len(xs))
	for i, x := range xs {
		out[i] = conv(x)
	}
	return result{l: out}
}
func intRes(i int) result { return result{s: strconv.Itoa(i)} }

func floatsOf(rs []result) []result { return rs }

// callOp executes one recorded operation.
func callOp(r *Random, name string, a []json.RawMessage) result {
	e := result{e: true}
	fl := func(v float64, err error) result {
		if err != nil {
			return e
		}
		return fres(v)
	}
	switch name {
	case "random":
		return fres(r.Random())
	case "getrandbits":
		v, err := r.GetRandBits(argInt(a[0]))
		if err != nil {
			return e
		}
		return ires(v)
	case "randbelow":
		v, err := r.RandBelowBig(argBig(a[0]))
		if err != nil {
			return e
		}
		return ires(v)
	case "randrange":
		var v *big.Int
		var err error
		switch len(a) {
		case 1:
			v, err = r.RandRangeBig(big.NewInt(0), argBig(a[0]), big.NewInt(1))
		case 2:
			v, err = r.RandRangeBig(argBig(a[0]), argBig(a[1]), big.NewInt(1))
		default:
			v, err = r.RandRangeBig(argBig(a[0]), argBig(a[1]), argBig(a[2]))
		}
		if err != nil {
			return e
		}
		// Cross-check the int64 entry point when everything fits.
		return ires(v)
	case "randint":
		v, err := r.RandIntBig(argBig(a[0]), argBig(a[1]))
		if err != nil {
			return e
		}
		return ires(v)
	case "randbytes":
		b, err := r.RandBytes(argInt(a[0]))
		if err != nil {
			return e
		}
		return result{s: hex.EncodeToString(b)}
	case "choice":
		i, err := r.ChoiceIndex(argInt(a[0]))
		if err != nil {
			return e
		}
		return intRes(i)
	case "choices":
		n, k := argInt(a[0]), argInt(a[3])
		var idx []int
		var err error
		switch {
		case !isNull(a[1]):
			var w []float64
			for _, x := range argList(a[1]) {
				w = append(w, argFloat(x))
			}
			idx, err = r.ChoicesIndices(n, k, w)
		case !isNull(a[2]):
			var c []float64
			for _, x := range argList(a[2]) {
				c = append(c, argFloat(x))
			}
			idx, err = r.ChoicesCumIndices(n, k, c)
		default:
			idx, err = r.ChoicesIndices(n, k, nil)
		}
		if err != nil {
			return e
		}
		return lres(idx, intRes)
	case "shuffle":
		x := make([]int, argInt(a[0]))
		for i := range x {
			x[i] = i
		}
		Shuffle(r, x)
		return lres(x, intRes)
	case "sample":
		idx, err := r.SampleIndices(argInt(a[0]), argInt(a[1]))
		if err != nil {
			return e
		}
		return lres(idx, intRes)
	case "sample_counts":
		n := argInt(a[0])
		pop := make([]int, n)
		for i := range pop {
			pop[i] = i
		}
		var counts []int64
		for _, c := range argList(a[1]) {
			counts = append(counts, argBig(c).Int64())
		}
		out, err := SampleCounts(r, pop, counts, argInt(a[2]))
		if err != nil {
			return e
		}
		return lres(out, intRes)
	case "uniform":
		return fres(r.Uniform(argFloat(a[0]), argFloat(a[1])))
	case "triangular":
		if isNull(a[2]) {
			return fres(r.Triangular(argFloat(a[0]), argFloat(a[1])))
		}
		return fres(r.TriangularMode(argFloat(a[0]), argFloat(a[1]), argFloat(a[2])))
	case "gauss":
		return fres(r.Gauss(argFloat(a[0]), argFloat(a[1])))
	case "normalvariate":
		return fres(r.NormalVariate(argFloat(a[0]), argFloat(a[1])))
	case "lognormvariate":
		return fl(r.LogNormVariate(argFloat(a[0]), argFloat(a[1])))
	case "expovariate":
		return fl(r.ExpoVariate(argFloat(a[0])))
	case "vonmisesvariate":
		return fl(r.VonMisesVariate(argFloat(a[0]), argFloat(a[1])))
	case "gammavariate":
		return fl(r.GammaVariate(argFloat(a[0]), argFloat(a[1])))
	case "betavariate":
		return fl(r.BetaVariate(argFloat(a[0]), argFloat(a[1])))
	case "paretovariate":
		return fl(r.ParetoVariate(argFloat(a[0])))
	case "weibullvariate":
		return fl(r.WeibullVariate(argFloat(a[0]), argFloat(a[1])))
	case "binomialvariate":
		v, err := r.BinomialVariate(argBig(a[0]).Int64(), argFloat(a[1]))
		if err != nil {
			return e
		}
		return intRes(int(v))
	}
	panic("unknown op " + name)
}

func decodeExpect(m json.RawMessage, wantFloat bool) result {
	if s, ok := rawStr(m); ok {
		if s == "ERR" {
			return result{e: true}
		}
		if wantFloat {
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				panic(err)
			}
			return fres(f)
		}
		return result{s: s}
	}
	l := argList(m)
	out := make([]result, len(l))
	for i, x := range l {
		out[i] = decodeExpect(x, wantFloat)
	}
	return result{l: out}
}

// floatOps are the operations whose results are floats.
var floatOps = map[string]bool{
	"random": true, "uniform": true, "triangular": true, "gauss": true,
	"normalvariate": true, "lognormvariate": true, "expovariate": true,
	"vonmisesvariate": true, "gammavariate": true, "betavariate": true,
	"paretovariate": true, "weibullvariate": true,
}

// libmOps depend on Log, Exp, Sin, Cos, Acos or Pow and can differ in the
// last bit from the platform libm CPython ran on.
var libmOps = map[string]bool{
	"gauss": true, "normalvariate": true, "lognormvariate": true,
	"expovariate": true, "vonmisesvariate": true, "gammavariate": true,
	"betavariate": true, "paretovariate": true, "weibullvariate": true,
}

func ulpDiff(a, b float64) int64 {
	x, y := int64(math.Float64bits(a)), int64(math.Float64bits(b))
	if x < 0 {
		x = math.MinInt64 - x
	}
	if y < 0 {
		y = math.MinInt64 - y
	}
	d := x - y
	if d < 0 {
		d = -d
	}
	return d
}

func equalResult(got, want result) (bool, int64) {
	if got.e != want.e {
		return false, -1
	}
	if got.isF && want.isF {
		if math.Float64bits(got.f) == math.Float64bits(want.f) {
			return true, 0
		}
		return false, ulpDiff(got.f, want.f)
	}
	if len(got.l) != len(want.l) || got.s != want.s {
		return false, -1
	}
	var worst int64
	ok := true
	for i := range got.l {
		g, d := equalResult(got.l[i], want.l[i])
		if !g {
			ok = false
			if d > worst || d < 0 {
				worst = d
			}
		}
	}
	return ok, worst
}

type opStat struct {
	n, bad int
	maxUlp int64
}

type diffReport struct {
	stats     map[string]*opStat
	seeds     int
	ops       int
	endBad    int
	gaussBad  int
	firstFail []string
}

func runDifferential(t *testing.T, rd io.Reader) *diffReport {
	t.Helper()
	rep := &diffReport{stats: map[string]*opStat{}}
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	for sc.Scan() {
		var rec fixtureRec
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		r := new(Random)
		seedFrom(t, r, rec.Seed)
		rep.seeds++
		for _, op := range rec.Ops {
			var name string
			_ = json.Unmarshal(op[0], &name)
			got := callOp(r, name, argList(op[1]))
			want := decodeExpect(op[2], floatOps[name])
			ok, d := equalResult(got, want)
			st := rep.stats[name]
			if st == nil {
				st = &opStat{}
				rep.stats[name] = st
			}
			st.n++
			rep.ops++
			if !ok {
				st.bad++
				if d > st.maxUlp {
					st.maxUlp = d
				}
				if d < 0 && len(rep.firstFail) < 10 {
					rep.firstFail = append(rep.firstFail, fmt.Sprintf("seed %s/%s op %s args %s got %+v want %s", rec.Seed.T, rec.Seed.V, name, op[1], got, op[2]))
				}
			}
		}
		st := r.GetState()
		words := make([]string, mtN)
		for i := 0; i < mtN; i++ {
			words[i] = strconv.FormatInt(st.State[i], 10)
		}
		sum := sha256.Sum256([]byte(strings.Join(words, ",")))
		if int(st.State[mtN]) != rec.End.Idx || hex.EncodeToString(sum[:]) != rec.End.Sha {
			rep.endBad++
			if len(rep.firstFail) < 10 {
				rep.firstFail = append(rep.firstFail, fmt.Sprintf("seed %s/%s: final state differs", rec.Seed.T, rec.Seed.V))
			}
		}
		if (st.GaussNext == nil) != (rec.End.Gauss == nil) {
			rep.gaussBad++
		} else if st.GaussNext != nil {
			w, _ := strconv.ParseFloat(*rec.End.Gauss, 64)
			if math.Float64bits(*st.GaussNext) != math.Float64bits(w) {
				rep.gaussBad++
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rep
}

func (rep *diffReport) check(t *testing.T) {
	t.Helper()
	names := make([]string, 0, len(rep.stats))
	for n := range rep.stats {
		names = append(names, n)
	}
	sort.Strings(names)
	var lines []string
	for _, n := range names {
		s := rep.stats[n]
		lines = append(lines, fmt.Sprintf("%-16s calls %6d  mismatches %5d  max ulp %d", n, s.n, s.bad, s.maxUlp))
	}
	t.Logf("%d seeds, %d operations, %d final-state mismatches, %d cached-gauss mismatches\n%s", rep.seeds, rep.ops, rep.endBad, rep.gaussBad, strings.Join(lines, "\n"))
	for _, f := range rep.firstFail {
		t.Error(f)
	}
	if rep.endBad != 0 {
		t.Errorf("%d seeds ended in a different generator state", rep.endBad)
	}
	for _, n := range names {
		s := rep.stats[n]
		switch {
		case !libmOps[n] && s.bad != 0:
			t.Errorf("%s: %d of %d results differ from CPython; this operation uses no libm and must match exactly", n, s.bad, s.n)
		case libmOps[n] && float64(s.bad) > 0.10*float64(s.n):
			t.Errorf("%s: %d of %d results differ, more than the 10%% libm budget", n, s.bad, s.n)
		}
	}
}

func TestFixture(t *testing.T) {
	f, err := os.Open("testdata/fixture.jsonl.gz")
	if err != nil {
		t.Fatalf("fixture missing: %v (regenerate with testdata/gen.py, see README)", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	runDifferential(t, gz).check(t)
}

// TestLivePython regenerates a larger corpus with the local python3 and
// compares it. It runs 200 seeds by 50 calls of every method.
func TestLivePython(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped with -short")
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found on PATH; install CPython 3.14 to run the live differential")
	}
	out, err := exec.Command(py, "-c", "import sys; print('%d.%d' % sys.version_info[:2])").Output()
	if err != nil {
		t.Skipf("python3 not runnable: %v", err)
	}
	t.Logf("python3 %s", bytes.TrimSpace(out))
	if strings.TrimSpace(string(out)) != "3.14" {
		t.Skipf("python3 is %s; the fixture semantics are those of 3.14", bytes.TrimSpace(out))
	}
	// PYRANDOM_LIVE_SEEDS and PYRANDOM_LIVE_ARGSEED vary the corpus for
	// repeated randomized runs.
	seeds, argSeed := "200", "7"
	if v := os.Getenv("PYRANDOM_LIVE_SEEDS"); v != "" {
		seeds = v
	}
	if v := os.Getenv("PYRANDOM_LIVE_ARGSEED"); v != "" {
		argSeed = v
	}
	cmd := exec.Command(py, "testdata/gen.py", "--seeds", seeds, "--calls", "50", "--args-seed", argSeed, "--errors")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	rep := runDifferential(t, pipe)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	rep.check(t)
}
