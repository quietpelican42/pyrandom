// Copyright (c) 2026 __AUTHOR__. Derived in part from CPython (Lib/random.py,
// Modules/_randommodule.c, Modules/mathmodule.c), Copyright (c) 2001-2026 Python
// Software Foundation; see LICENSE-python. The Mersenne Twister is covered by
// LICENSE-mt19937.

package pyrandom

import "math"

// lgamma follows CPython's Modules/mathmodule.c (m_lgamma), which uses a
// Lanczos approximation rather than the platform libm. Only x > 0 is needed
// here and only that domain is implemented; other arguments go to math.Lgamma.

const lanczosN = 13

const lanczosG = 6.024680040776729583740234375

var lanczosNumCoeffs = [lanczosN]float64{
	23531376880.410759688572007674451636754734846804940,
	42919803642.649098768957899047001988850926355848959,
	35711959237.355668049440185451547166705960488635843,
	17921034426.037209699919755754458931112671403265390,
	6039542586.3520280050642916443072979210699388420708,
	1439720407.3117216736632230727949123939715485786772,
	248874557.86205415651146038641322942321632125127801,
	31426415.585400194380614231628318205362874684987640,
	2876370.6289353724412254090516208496135991145378768,
	186056.26539522349504029498971604569928220784236328,
	8071.6720023658162106380029022722506138218516325024,
	210.82427775157934587250973392071336271166969580291,
	2.5066282746310002701649081771338373386264310793408,
}

var lanczosDenCoeffs = [lanczosN]float64{
	0.0, 39916800.0, 120543840.0, 150917976.0, 105258076.0, 45995730.0,
	13339535.0, 2637558.0, 357423.0, 32670.0, 1925.0, 66.0, 1.0,
}

func lanczosSum(x float64, fma bool) float64 {
	var num, den float64
	if x < 5.0 {
		for i := lanczosN - 1; i >= 0; i-- {
			if fma {
				num = math.FMA(num, x, lanczosNumCoeffs[i])
				den = math.FMA(den, x, lanczosDenCoeffs[i])
			} else {
				num = float64(num*x) + lanczosNumCoeffs[i]
				den = float64(den*x) + lanczosDenCoeffs[i]
			}
		}
	} else {
		for i := 0; i < lanczosN; i++ {
			num = num/x + lanczosNumCoeffs[i]
			den = den/x + lanczosDenCoeffs[i]
		}
	}
	return num / den
}

// lgamma returns log(|Gamma(x)|) for x > 0 as CPython built with fused
// multiply-add computes it (arm64 clang and gcc do this by default).
func lgamma(x float64) float64 { return lgammaImpl(x, true) }

// lgammaNoFMA is lgamma as CPython built without fused multiply-add
// computes it.
func lgammaNoFMA(x float64) float64 { return lgammaImpl(x, false) }

func lgammaImpl(x float64, fma bool) float64 {
	if !(x > 0) || math.IsInf(x, 0) {
		v, _ := math.Lgamma(x)
		return v
	}
	if x == math.Floor(x) && x <= 2.0 {
		return 0.0
	}
	if x < 1e-20 {
		return -log(x)
	}
	r := log(lanczosSum(x, fma)) - lanczosG
	if fma {
		r = math.FMA(x-0.5, log((x+lanczosG)-0.5)-1, r)
	} else {
		r += float64((x - 0.5) * (log((x+lanczosG)-0.5) - 1))
	}
	return r
}
