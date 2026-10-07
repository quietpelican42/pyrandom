// Copyright (c) 2026 kofiadeyemiq. Derived in part from CPython (Lib/random.py,
// Modules/_randommodule.c, Modules/mathmodule.c), Copyright (c) 2001-2026 Python
// Software Foundation; see LICENSE-python. The Mersenne Twister is covered by
// LICENSE-mt19937.

package pyrandom

import (
	"encoding/json"
	"fmt"
	"math"
)

// stateVersion is the version tag of Python's Random.getstate().
const stateVersion = 3

// State is the generator state in the shape of Python's getstate() tuple
// (version, internalstate, gauss_next). Its JSON form is
//
//	{"version": 3, "state": [624 words, index], "gauss_next": null}
//
// which converts to and from Python with
//
//	v, s, g = d["version"], tuple(d["state"]), d["gauss_next"]
//	random.setstate((v, s, g))
type State struct {
	Version   int      `json:"version"`
	State     []int64  `json:"state"`
	GaussNext *float64 `json:"gauss_next"`
}

// GetState returns the current state, like random.getstate().
func (r *Random) GetState() State {
	s := make([]int64, 0, mtN+1)
	for _, w := range r.mt {
		s = append(s, int64(w))
	}
	s = append(s, int64(r.idx))
	st := State{Version: stateVersion, State: s}
	if r.hasGauss {
		g := r.gaussVal
		st.GaussNext = &g
	}
	return st
}

// SetState restores a state, like random.setstate(state). Versions 2 and 3
// are accepted; in version 2 negative words are taken modulo 2**32. Unlike
// CPython, which truncates silently, words outside the 32-bit range in a
// version 3 state are rejected.
func (r *Random) SetState(st State) error {
	if st.Version != 2 && st.Version != 3 {
		return valueErr("state with version %d passed to SetState of version %d", st.Version, stateVersion)
	}
	if len(st.State) != mtN+1 {
		return valueErr("state vector is the wrong size")
	}
	idx := st.State[mtN]
	if idx < 0 || idx > mtN {
		return valueErr("invalid state")
	}
	var mt [mtN]uint32
	for i := 0; i < mtN; i++ {
		w := st.State[i]
		if st.Version == 2 {
			w = ((w % (1 << 32)) + (1 << 32)) % (1 << 32)
		}
		if w < 0 || w > math.MaxUint32 {
			return valueErr("state word %d out of range", i)
		}
		mt[i] = uint32(w)
	}
	if st.GaussNext != nil && (math.IsNaN(*st.GaussNext) || math.IsInf(*st.GaussNext, 0)) {
		return valueErr("gauss_next must be finite")
	}
	r.mt = mt
	r.idx = int(idx)
	r.hasGauss = st.GaussNext != nil
	r.gaussVal = 0
	if r.hasGauss {
		r.gaussVal = *st.GaussNext
	}
	return nil
}

// MarshalState returns the state as JSON, see State.
func (r *Random) MarshalState() ([]byte, error) { return json.Marshal(r.GetState()) }

// UnmarshalState restores the state from the JSON form produced by
// MarshalState or by Python (see State).
func (r *Random) UnmarshalState(data []byte) error {
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("pyrandom: decoding state: %w", err)
	}
	return r.SetState(st)
}
