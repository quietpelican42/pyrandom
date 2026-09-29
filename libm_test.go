package pyrandom

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The reimplemented elementary functions are compared with the libm of the
// CPython that recorded testdata/libm.txt.gz, and against Go's math package
// for context.

type libmFn struct {
	ours, std func(x, y float64) float64
	maxRate   float64 // ceiling on the fraction of last-bit differences
}

var libmFns = map[string]libmFn{
	"log":    {func(x, _ float64) float64 { return log(x) }, func(x, _ float64) float64 { return math.Log(x) }, 0.005},
	"exp":    {func(x, _ float64) float64 { return exp(x) }, func(x, _ float64) float64 { return math.Exp(x) }, 0.01},
	"log2":   {func(x, _ float64) float64 { return log2(x) }, func(x, _ float64) float64 { return math.Log2(x) }, 0.005},
	"pow":    {func(x, y float64) float64 { return pow(x, y) }, func(x, y float64) float64 { return math.Pow(x, y) }, 0.01},
	"sin":    {func(x, _ float64) float64 { return sin(x) }, func(x, _ float64) float64 { return math.Sin(x) }, 0.08},
	"cos":    {func(x, _ float64) float64 { return cos(x) }, func(x, _ float64) float64 { return math.Cos(x) }, 0.08},
	"acos":   {func(x, _ float64) float64 { return acos(x) }, func(x, _ float64) float64 { return math.Acos(x) }, 0.25},
	"lgamma": {func(x, _ float64) float64 { return lgamma(x) }, func(x, _ float64) float64 { v, _ := math.Lgamma(x); return v }, 0.005},
}

func runLibm(t *testing.T, rd io.Reader) {
	t.Helper()
	type tally struct{ n, ours, std int }
	tal := map[string]*tally{}
	sc := bufio.NewScanner(rd)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		fn := libmFns[f[0]]
		var x, y, want float64
		var err error
		if x, err = strconv.ParseFloat(f[1], 64); err != nil {
			t.Fatal(err)
		}
		if f[0] == "pow" {
			y, _ = strconv.ParseFloat(f[2], 64)
			want, _ = strconv.ParseFloat(f[3], 64)
		} else {
			want, _ = strconv.ParseFloat(f[2], 64)
		}
		tl := tal[f[0]]
		if tl == nil {
			tl = &tally{}
			tal[f[0]] = tl
		}
		tl.n++
		if math.Float64bits(fn.ours(x, y)) != math.Float64bits(want) {
			tl.ours++
		}
		if math.Float64bits(fn.std(x, y)) != math.Float64bits(want) {
			tl.std++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(tal))
	for n := range tal {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		tl := tal[n]
		fmt.Fprintf(&b, "\n%-7s inputs %7d  pyrandom differs %6d (%.3f%%)  math.%s differs %6d (%.3f%%)",
			n, tl.n, tl.ours, 100*float64(tl.ours)/float64(tl.n), n, tl.std, 100*float64(tl.std)/float64(tl.n))
		if rate := float64(tl.ours) / float64(tl.n); rate > libmFns[n].maxRate {
			t.Errorf("%s: %.3f%% of results differ from CPython's libm, ceiling %.1f%%", n, 100*rate, 100*libmFns[n].maxRate)
		}
		if n != "lgamma" && tl.ours >= tl.std {
			t.Errorf("%s: pyrandom (%d) is not closer to libm than math (%d)", n, tl.ours, tl.std)
		}
	}
	t.Log(b.String())
}

func TestLibmFixture(t *testing.T) {
	f, err := os.Open("testdata/libm.txt.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	runLibm(t, gz)
}

func TestLibmLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped with -short")
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found on PATH; install CPython to run the live libm comparison")
	}
	cmd := exec.Command(py, "testdata/genlibm.py", "100000")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	runLibm(t, pipe)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}
