// Copyright (c) 2026 __AUTHOR__. Derived in part from CPython (Lib/random.py,
// Modules/_randommodule.c, Modules/mathmodule.c), Copyright (c) 2001-2026 Python
// Software Foundation; see LICENSE-python. The Mersenne Twister is covered by
// LICENSE-mt19937.

package pyrandom

import (
	"math"
	"sort"
)

// ChoiceIndex returns a random index in [0, n), like the index chosen by
// random.choice on a sequence of length n. It returns ErrIndex if n is 0.
func (r *Random) ChoiceIndex(n int) (int, error) {
	if n <= 0 {
		return 0, ErrIndex
	}
	return r.randbelowInt(n), nil
}

// Choice returns a random element of seq, like random.choice(seq).
func Choice[T any](r *Random, seq []T) (T, error) {
	i, err := r.ChoiceIndex(len(seq))
	if err != nil {
		var zero T
		return zero, err
	}
	return seq[i], nil
}

// Shuffle shuffles n elements in place using swap, like random.shuffle on a
// list of length n (Fisher-Yates from the end).
func (r *Random) Shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i >= 1; i-- {
		j := r.randbelowInt(i + 1)
		swap(i, j)
	}
}

// Shuffle shuffles x in place, like random.shuffle(x).
func Shuffle[T any](r *Random, x []T) {
	r.Shuffle(len(x), func(i, j int) { x[i], x[j] = x[j], x[i] })
}

// SampleIndices returns k distinct integers from range(n) in selection
// order, like random.sample(range(n), k). It uses the same pool or set
// strategy as CPython, chosen by n and k.
func (r *Random) SampleIndices(n, k int) ([]int, error) {
	if k < 0 || k > n {
		return nil, valueErr("sample larger than population or is negative")
	}
	result := make([]int, k)
	setsize := 21
	if k > 5 {
		setsize += pow4Ceil(k * 3)
	}
	if n <= setsize {
		pool := make([]int, n)
		for i := range pool {
			pool[i] = i
		}
		for i := 0; i < k; i++ {
			j := r.randbelowInt(n - i)
			result[i] = pool[j]
			pool[j] = pool[n-i-1]
		}
		return result, nil
	}
	selected := make(map[int]struct{}, k)
	for i := 0; i < k; i++ {
		j := r.randbelowInt(n)
		for {
			if _, dup := selected[j]; !dup {
				break
			}
			j = r.randbelowInt(n)
		}
		selected[j] = struct{}{}
		result[i] = j
	}
	return result, nil
}

// pow4Ceil returns 4 ** ceil(log(x, 4)) as CPython computes it, where
// log(x, 4) is log(x)/log(4) in floating point.
func pow4Ceil(x int) int {
	e := int(math.Ceil(log(float64(x)) / log(4)))
	return 1 << (2 * uint(e))
}

// Sample returns k distinct elements of pop in selection order, like
// random.sample(pop, k).
func Sample[T any](r *Random, pop []T, k int) ([]T, error) {
	idx, err := r.SampleIndices(len(pop), k)
	if err != nil {
		return nil, err
	}
	out := make([]T, k)
	for i, j := range idx {
		out[i] = pop[j]
	}
	return out, nil
}

// SampleCounts is Sample with repeated elements, like
// random.sample(pop, k, counts=counts).
func SampleCounts[T any](r *Random, pop []T, counts []int64, k int) ([]T, error) {
	if len(counts) != len(pop) {
		return nil, valueErr("the number of counts does not match the population")
	}
	cum := make([]int64, 0, len(counts))
	var total int64
	for _, c := range counts {
		total += c
		cum = append(cum, total)
	}
	if total < 0 {
		return nil, valueErr("counts must be non-negative")
	}
	if len(cum) > 0 {
		cum = cum[:len(cum)-1]
	}
	if total > math.MaxInt {
		return nil, ErrOverflow
	}
	sel, err := r.SampleIndices(int(total), k)
	if err != nil {
		return nil, err
	}
	out := make([]T, k)
	for i, s := range sel {
		// bisect_right(cum, s)
		p := sort.Search(len(cum), func(j int) bool { return int64(s) < cum[j] })
		out[i] = pop[p]
	}
	return out, nil
}

// ChoicesIndices returns k indices in [0, n) drawn with replacement. With
// nil weights the draws are uniform, like random.choices(pop, k=k) on a
// population of length n; otherwise weights are the relative weights, like
// random.choices(pop, weights, k=k).
func (r *Random) ChoicesIndices(n, k int, weights []float64) ([]int, error) {
	if weights == nil {
		return r.choicesUniform(n, k)
	}
	cum := make([]float64, len(weights))
	var acc float64
	for i, w := range weights {
		if i == 0 {
			acc = w
		} else {
			acc += w
		}
		cum[i] = acc
	}
	return r.ChoicesCumIndices(n, k, cum)
}

func (r *Random) choicesUniform(n, k int) ([]int, error) {
	if k < 0 {
		k = 0
	}
	out := make([]int, k)
	nf := float64(n)
	for i := range out {
		v := int(math.Floor(float64(r.Random() * nf)))
		if v >= n || v < 0 {
			return nil, ErrIndex
		}
		out[i] = v
	}
	return out, nil
}

// ChoicesCumIndices is ChoicesIndices with cumulative weights, like
// random.choices(pop, cum_weights=cum, k=k).
func (r *Random) ChoicesCumIndices(n, k int, cum []float64) ([]int, error) {
	if len(cum) != n {
		return nil, valueErr("the number of weights does not match the population")
	}
	if n == 0 {
		return nil, ErrIndex
	}
	total := cum[n-1] + 0.0
	if total <= 0.0 {
		return nil, valueErr("total of weights must be greater than zero")
	}
	if math.IsInf(total, 0) || math.IsNaN(total) {
		return nil, valueErr("total of weights must be finite")
	}
	if k < 0 {
		k = 0
	}
	hi := n - 1
	out := make([]int, k)
	for i := range out {
		x := float64(r.Random() * total)
		// bisect_right(cum, x, 0, hi)
		out[i] = sort.Search(hi, func(j int) bool { return x < cum[j] })
	}
	return out, nil
}

// Choices returns k elements of pop drawn with replacement, like
// random.choices(pop, weights, k=k). Nil weights select uniformly.
func Choices[T any](r *Random, pop []T, k int, weights []float64) ([]T, error) {
	idx, err := r.ChoicesIndices(len(pop), k, weights)
	return pick(pop, idx), err
}

// ChoicesCum is Choices with cumulative weights, like
// random.choices(pop, cum_weights=cum, k=k).
func ChoicesCum[T any](r *Random, pop []T, k int, cum []float64) ([]T, error) {
	idx, err := r.ChoicesCumIndices(len(pop), k, cum)
	return pick(pop, idx), err
}

func pick[T any](pop []T, idx []int) []T {
	if idx == nil {
		return nil
	}
	out := make([]T, len(idx))
	for i, j := range idx {
		out[i] = pop[j]
	}
	return out
}
