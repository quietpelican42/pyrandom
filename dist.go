// Copyright (c) 2026 kofiadeyemiq. Derived in part from CPython (Lib/random.py,
// Modules/_randommodule.c, Modules/mathmodule.c), Copyright (c) 2001-2026 Python
// Software Foundation; see LICENSE-python. The Mersenne Twister is covered by
// LICENSE-mt19937.

package pyrandom

import "math"

// Constants from Lib/random.py.
const (
	nvMagicConst = 1.7155277699214135 // 4*exp(-0.5)/sqrt(2.0)
	twoPi        = 6.283185307179586
	log4         = 1.3862943611198906
	sgMagicConst = 2.504077396776274 // 1.0 + log(4.5)
)

// pyMod is Python's float remainder: the result has the sign of b.
func pyMod(a, b float64) float64 {
	m := math.Mod(a, b)
	if m != 0 {
		if (b < 0) != (m < 0) {
			m += b
		}
	} else {
		m = math.Copysign(0, b)
	}
	return m
}

// powChecked is Python's float ** float for a finite positive base: it
// reports overflow and division by zero as errors.
func powChecked(x, y float64) (float64, error) {
	if x == 0 && y < 0 {
		return 0, ErrZeroDivision
	}
	v := pow(x, y)
	if math.IsInf(v, 0) && !math.IsInf(x, 0) && !math.IsInf(y, 0) {
		return 0, ErrOverflow
	}
	return v, nil
}

func expChecked(x float64) (float64, error) {
	v := exp(x)
	if math.IsInf(v, 0) && !math.IsInf(x, 0) {
		return 0, ErrOverflow
	}
	return v, nil
}

// Uniform returns a + (b-a)*Random(), like random.uniform(a, b).
func (r *Random) Uniform(a, b float64) float64 {
	return a + float64((b-a)*r.Random())
}

// Triangular returns a triangular variate on [low, high] with the midpoint
// as mode, like random.triangular(low, high).
func (r *Random) Triangular(low, high float64) float64 {
	return r.triangular(low, high, 0.5)
}

// TriangularMode is Triangular with an explicit mode, like
// random.triangular(low, high, mode).
func (r *Random) TriangularMode(low, high, mode float64) float64 {
	if high-low == 0 {
		r.Random()
		return low
	}
	return r.triangular(low, high, (mode-low)/(high-low))
}

func (r *Random) triangular(low, high, c float64) float64 {
	u := r.Random()
	if u > c {
		u = 1.0 - u
		c = 1.0 - c
		low, high = high, low
	}
	return low + float64((high-low)*math.Sqrt(float64(u*c)))
}

// NormalVariate returns a normal variate using Kinderman and Monahan's
// method, like random.normalvariate(mu, sigma).
func (r *Random) NormalVariate(mu, sigma float64) float64 {
	var z float64
	for {
		u1 := r.Random()
		u2 := 1.0 - r.Random()
		z = float64(nvMagicConst*(u1-0.5)) / u2
		zz := float64(z*z) / 4.0
		if zz <= -log(u2) {
			break
		}
	}
	return mu + float64(z*sigma)
}

// Gauss returns a normal variate, like random.gauss(mu, sigma). It caches
// the second value of each pair, so the stream depends on call order.
func (r *Random) Gauss(mu, sigma float64) float64 {
	var z float64
	if r.hasGauss {
		z = r.gaussVal
		r.hasGauss = false
	} else {
		x2pi := float64(r.Random() * twoPi)
		g2rad := math.Sqrt(float64(-2.0 * log(1.0-r.Random())))
		z = float64(cos(x2pi) * g2rad)
		r.gaussVal = float64(sin(x2pi) * g2rad)
		r.hasGauss = true
	}
	return mu + float64(z*sigma)
}

// LogNormVariate returns exp(NormalVariate(mu, sigma)), like
// random.lognormvariate. It returns ErrOverflow if the result overflows.
func (r *Random) LogNormVariate(mu, sigma float64) (float64, error) {
	return expChecked(r.NormalVariate(mu, sigma))
}

// ExpoVariate returns an exponential variate, like random.expovariate(lambd).
// It returns ErrZeroDivision for lambd == 0 (after consuming one draw, as
// Python does).
func (r *Random) ExpoVariate(lambd float64) (float64, error) {
	v := -log(1.0 - r.Random())
	if lambd == 0 {
		return 0, ErrZeroDivision
	}
	return v / lambd, nil
}

// VonMisesVariate returns a von Mises variate, like
// random.vonmisesvariate(mu, kappa).
func (r *Random) VonMisesVariate(mu, kappa float64) (float64, error) {
	if kappa <= 1e-6 {
		return float64(twoPi * r.Random()), nil
	}
	s := 0.5 / kappa
	rr := s + math.Sqrt(1.0+float64(s*s))
	var z float64
	for {
		u1 := r.Random()
		z = cos(math.Pi * u1)
		d := z / (rr + z)
		u2 := r.Random()
		if u2 < 1.0-float64(d*d) || u2 <= float64((1.0-d)*exp(d)) {
			break
		}
	}
	q := 1.0 / rr
	f := (q + z) / (1.0 + float64(q*z))
	u3 := r.Random()
	if f > 1 || f < -1 || math.IsNaN(f) {
		return 0, valueErr("expected a value in the range [-1, 1]")
	}
	if u3 > 0.5 {
		return pyMod(mu+acos(f), twoPi), nil
	}
	return pyMod(mu-acos(f), twoPi), nil
}

// GammaVariate returns a gamma variate, like random.gammavariate(alpha,
// beta). It returns ErrValue unless alpha > 0 and beta > 0.
func (r *Random) GammaVariate(alpha, beta float64) (float64, error) {
	if alpha <= 0.0 || beta <= 0.0 {
		return 0, valueErr("gammavariate: alpha and beta must be > 0.0")
	}
	switch {
	case alpha > 1.0:
		ainv := math.Sqrt(float64(2.0*alpha) - 1.0)
		bbb := alpha - log4
		ccc := alpha + ainv
		for {
			u1 := r.Random()
			if !(1e-7 < u1 && u1 < 0.9999999) {
				continue
			}
			u2 := 1.0 - r.Random()
			v := log(u1/(1.0-u1)) / ainv
			ev, err := expChecked(v)
			if err != nil {
				return 0, err
			}
			x := float64(alpha * ev)
			z := float64(u1*u1) * u2
			rr := bbb + float64(ccc*v) - x
			if rr+sgMagicConst-float64(4.5*z) >= 0.0 || rr >= log(z) {
				return x * beta, nil
			}
		}
	case alpha == 1.0:
		return float64(-log(1.0-r.Random())) * beta, nil
	}
	for {
		u := r.Random()
		b := (math.E + alpha) / math.E
		p := float64(b * u)
		var x float64
		if p <= 1.0 {
			var err error
			if x, err = powChecked(p, 1.0/alpha); err != nil {
				return 0, err
			}
		} else {
			x = -log((b - p) / alpha)
		}
		u1 := r.Random()
		if p > 1.0 {
			pv, err := powChecked(x, alpha-1.0)
			if err != nil {
				return 0, err
			}
			if u1 <= pv {
				return x * beta, nil
			}
		} else if u1 <= exp(-x) {
			return x * beta, nil
		}
	}
}

// BetaVariate returns a beta variate, like random.betavariate(alpha, beta).
func (r *Random) BetaVariate(alpha, beta float64) (float64, error) {
	y, err := r.GammaVariate(alpha, 1.0)
	if err != nil {
		return 0, err
	}
	if y != 0 {
		z, err := r.GammaVariate(beta, 1.0)
		if err != nil {
			return 0, err
		}
		return y / (y + z), nil
	}
	return 0.0, nil
}

// ParetoVariate returns a Pareto variate, like random.paretovariate(alpha).
func (r *Random) ParetoVariate(alpha float64) (float64, error) {
	u := 1.0 - r.Random()
	if alpha == 0 {
		return 0, ErrZeroDivision
	}
	return powChecked(u, -1.0/alpha)
}

// WeibullVariate returns a Weibull variate, like
// random.weibullvariate(alpha, beta).
func (r *Random) WeibullVariate(alpha, beta float64) (float64, error) {
	u := 1.0 - r.Random()
	if beta == 0 {
		return 0, ErrZeroDivision
	}
	v, err := powChecked(-log(u), 1.0/beta)
	if err != nil {
		return 0, err
	}
	return alpha * v, nil
}

func (r *Random) lgamma(x float64) float64 {
	if r.NoFMA {
		return lgammaNoFMA(x)
	}
	return lgamma(x)
}

// BinomialVariate returns the number of successes in n independent trials
// of probability p, like random.binomialvariate(n, p).
func (r *Random) BinomialVariate(n int64, p float64) (int64, error) {
	if n < 0 {
		return 0, valueErr("n must be non-negative")
	}
	if p <= 0.0 || p >= 1.0 {
		if p == 0.0 {
			return 0, nil
		}
		if p == 1.0 {
			return n, nil
		}
		return 0, valueErr("p must be in the range 0.0 <= p <= 1.0")
	}
	if n == 1 {
		if r.Random() < p {
			return 1, nil
		}
		return 0, nil
	}
	if p > 0.5 {
		k, err := r.BinomialVariate(n, 1.0-p)
		return n - k, err
	}
	nf := float64(n)
	if float64(nf*p) < 10.0 {
		// Devroye's geometric method.
		var x int64
		y := 0.0
		c := log2(1.0 - p)
		if c == 0 {
			return x, nil
		}
		for {
			u := r.Random()
			if u == 0 {
				continue
			}
			y += math.Floor(log2(u)/c) + 1
			if y > nf {
				return x, nil
			}
			x++
		}
	}
	// BTRS: transformed rejection with squeeze (Hormann).
	setup := false
	spq := math.Sqrt(float64(nf*p) * (1.0 - p))
	b := 1.15 + float64(2.53*spq)
	a := -0.0873 + float64(0.0248*b) + float64(0.01*p)
	c := float64(nf*p) + 0.5
	vr := 0.92 - 4.2/b
	var alpha, lpq, h float64
	var m int64
	for {
		u := r.Random()
		u -= 0.5
		us := 0.5 - math.Abs(u)
		if us == 0 {
			continue
		}
		kf := math.Floor(float64((2.0*a/us+b)*u) + c)
		if kf < 0 || kf > nf {
			continue
		}
		k := int64(kf)
		v := r.Random()
		if us >= 0.07 && v <= vr {
			return k, nil
		}
		if !setup {
			alpha = (2.83 + 5.1/b) * spq
			lpq = log(p / (1.0 - p))
			m = int64(math.Floor(float64(nf+1) * p))
			h = r.lgamma(float64(m+1)) + r.lgamma(float64(n-m+1))
			setup = true
		}
		v *= alpha / (a/float64(us*us) + b)
		rhs := h - r.lgamma(float64(k+1)) - r.lgamma(float64(n-k+1)) + float64(float64(k-m)*lpq)
		if log(v) <= rhs {
			return k, nil
		}
	}
}
