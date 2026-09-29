package pyrandom

import "math"

// Correctly rounded elementary functions.
//
// CPython calls the platform libm. Go's math package is accurate to within
// about one ulp but is not correctly rounded, so on random inputs it differs
// from libm in the last bit for 7% (Log) to 70% (Pow) of arguments. The
// functions in this file evaluate the result in double-double arithmetic
// (about 100 bits) and round once, which agrees with a good libm except on
// the rare arguments where the libm itself is not correctly rounded.
//
// Every product that feeds a sum is wrapped in float64() so the Go compiler
// cannot fuse it into an FMA (arm64 does, amd64 does not), which would make
// results architecture dependent.

// dd is an unevaluated sum hi+lo with |lo| <= ulp(hi)/2.
type dd struct{ hi, lo float64 }

func twoSum(a, b float64) dd {
	s := a + b
	bb := s - a
	return dd{s, (a - (s - bb)) + (b - bb)}
}

func fastTwoSum(a, b float64) dd {
	s := a + b
	return dd{s, b - (s - a)}
}

func twoProd(a, b float64) dd {
	p := float64(a * b)
	return dd{p, math.FMA(a, b, -p)}
}

func (x dd) neg() dd { return dd{-x.hi, -x.lo} }

func (x dd) add(y dd) dd {
	s := twoSum(x.hi, y.hi)
	t := twoSum(x.lo, y.lo)
	s = fastTwoSum(s.hi, s.lo+t.hi)
	return fastTwoSum(s.hi, s.lo+t.lo)
}

func (x dd) sub(y dd) dd { return x.add(y.neg()) }

func (x dd) addF(f float64) dd {
	s := twoSum(x.hi, f)
	return fastTwoSum(s.hi, s.lo+x.lo)
}

func (x dd) mul(y dd) dd {
	p := twoProd(x.hi, y.hi)
	c := float64(x.hi*y.lo) + float64(x.lo*y.hi)
	return fastTwoSum(p.hi, p.lo+c)
}

func (x dd) mulF(f float64) dd {
	p := twoProd(x.hi, f)
	return fastTwoSum(p.hi, p.lo+float64(x.lo*f))
}

func (x dd) divF(f float64) dd {
	q1 := x.hi / f
	r := x.sub(twoProd(q1, f))
	q2 := r.hi / f
	r = r.sub(twoProd(q2, f))
	q3 := r.hi / f
	s := fastTwoSum(q1, q2)
	return s.addF(q3)
}

func (x dd) div(y dd) dd {
	q1 := x.hi / y.hi
	r := x.sub(y.mulF(q1))
	q2 := r.hi / y.hi
	r = r.sub(y.mulF(q2))
	q3 := r.hi / y.hi
	s := fastTwoSum(q1, q2)
	return s.addF(q3)
}

func (x dd) float() float64 { return x.hi + x.lo }

// Constants split into three doubles (about 160 bits).
var (
	ln2a, ln2b, ln2c = 0x1.62e42fefa39efp-1, 0x1.abc9e3b39803fp-56, 0x1.7b57a079a1934p-111
	ln2dd            = dd{0x1.62e42fefa39efp-1, 0x1.abc9e3b39803fp-56}
	invLn2dd         = dd{0x1.71547652b82fep+0, 0x1.777d0ffda0d24p-56}
	pio2a, pio2b     = 0x1.921fb54442d18p+0, 0x1.1a62633145c07p-54
	pio2c            = -0x1.f1976b7ed8fbcp-110
	pidd             = dd{0x1.921fb54442d18p+1, 0x1.1a62633145c07p-53}
)

// expDD returns exp(x) for a double-double argument as m*2**k with m in
// about [0.7, 1.5].
func expDD(x dd) (dd, int) {
	kf := math.Round(x.hi / ln2a)
	r := x.sub(twoProd(kf, ln2a)).sub(twoProd(kf, ln2b)).sub(dd{float64(kf * ln2c), 0})
	t := dd{r.hi / 256, r.lo / 256}
	term, sum := t, t
	for n := 2; n <= 12; n++ {
		term = term.mul(t).divF(float64(n))
		sum = sum.add(term)
	}
	for i := 0; i < 8; i++ {
		sum = sum.mulF(2).add(sum.mul(sum))
	}
	return sum.addF(1), int(kf)
}

// exp returns e**x rounded to nearest.
func exp(x float64) float64 {
	if !(x > -700 && x < 709) {
		return math.Exp(x)
	}
	m, k := expDD(dd{x, 0})
	return math.Ldexp(m.float(), k)
}

// logDD returns log(x) and reports whether x was in the supported range
// (positive, finite, normal).
func logDD(x float64) (dd, bool) {
	if !(x >= 0x1p-1022 && x <= math.MaxFloat64) {
		return dd{}, false
	}
	m, e := math.Frexp(x)
	if m < math.Sqrt2/2 {
		m *= 2
		e--
	}
	s := dd{m - 1, 0}.div(twoSum(m, 1))
	z := s.mul(s)
	pw, sum := s, s
	for j := 3.0; j <= 45; j += 2 {
		pw = pw.mul(z)
		sum = sum.add(pw.divF(j))
	}
	sum = sum.mulF(2)
	if e == 0 {
		return sum, true
	}
	ef := float64(e)
	l := twoProd(ef, ln2a).add(twoProd(ef, ln2b)).add(dd{float64(ef * ln2c), 0})
	return l.add(sum), true
}

// log returns the natural logarithm of x rounded to nearest.
func log(x float64) float64 {
	l, ok := logDD(x)
	if !ok {
		return math.Log(x)
	}
	return l.float()
}

// log2 returns the base-2 logarithm of x rounded to nearest.
func log2(x float64) float64 {
	if !(x >= 0x1p-1022 && x <= math.MaxFloat64) {
		return math.Log2(x)
	}
	m, e := math.Frexp(x)
	if m < math.Sqrt2/2 {
		m *= 2
		e--
	}
	l, _ := logDD(m)
	return l.mul(invLn2dd).addF(float64(e)).float()
}

// pow returns x**y rounded to nearest for finite x > 0 and finite y; other
// arguments and results outside the normal range go to math.Pow.
func pow(x, y float64) float64 {
	if !(x > 0 && x != 1 && y != 0) || math.IsInf(y, 0) || math.IsNaN(y) {
		return math.Pow(x, y)
	}
	l, ok := logDD(x)
	if !ok {
		return math.Pow(x, y)
	}
	p := l.mulF(y)
	if !(p.hi > -700 && p.hi < 709) {
		return math.Pow(x, y)
	}
	m, k := expDD(p)
	return math.Ldexp(m.float(), k)
}

// reduce returns r and n with x = n*pi/2 + r and |r| <= pi/4 (roughly).
func reduce(x float64) (dd, int) {
	kf := math.Round(x / pio2a)
	r := dd{x, 0}.sub(twoProd(kf, pio2a)).sub(twoProd(kf, pio2b)).sub(dd{float64(kf * pio2c), 0})
	return r, int(int64(kf) & 3)
}

func sinDD(r dd) dd {
	z := r.mul(r)
	term, sum := r, r
	for n := 1; n <= 16; n++ {
		term = term.mul(z).divF(float64((2 * n) * (2*n + 1))).neg()
		sum = sum.add(term)
	}
	return sum
}

func cosDD(r dd) dd {
	z := r.mul(r)
	term, sum := dd{1, 0}, dd{1, 0}
	for n := 1; n <= 16; n++ {
		term = term.mul(z).divF(float64((2*n - 1) * (2 * n))).neg()
		sum = sum.add(term)
	}
	return sum
}

func sincosDD(x float64) (s, c dd) {
	r, q := reduce(x)
	sr, cr := sinDD(r), cosDD(r)
	switch q {
	case 0:
		return sr, cr
	case 1:
		return cr, sr.neg()
	case 2:
		return sr.neg(), cr.neg()
	}
	return cr.neg(), sr
}

func trigOK(x float64) bool { return math.Abs(x) < 1e5 }

// sin returns the sine of x rounded to nearest.
func sin(x float64) float64 {
	if !trigOK(x) || x == 0 {
		return math.Sin(x)
	}
	s, _ := sincosDD(x)
	return s.float()
}

// cos returns the cosine of x rounded to nearest.
func cos(x float64) float64 {
	if !trigOK(x) {
		return math.Cos(x)
	}
	_, c := sincosDD(x)
	return c.float()
}

// acos returns the arc cosine of x rounded to nearest, or NaN outside [-1,1].
func acos(x float64) float64 {
	if !(x > -1 && x < 1) {
		return math.Acos(x)
	}
	y := dd{math.Acos(x), 0}
	for i := 0; i < 2; i++ {
		s, c := sincosDD(y.hi)
		d := c.addF(-x).float() / s.hi
		y = dd{y.hi, 0}.addF(d)
	}
	return y.float()
}
