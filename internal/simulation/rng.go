package simulation

import "math"

// DerivePathSeed derives a deterministic non-negative 63-bit seed for one path.
func DerivePathSeed(rootSeed int64, pathNo int) int64 {
	return int64(SplitMix64(seedAsUint64(rootSeed)+seedAsUint64(int64(pathNo))) & math.MaxInt64)
}

func seedAsUint64(v int64) uint64 {
	if v < 0 {
		return uint64(-v)
	}
	return uint64(v)
}

// SplitMix64 derives a deterministic non-negative 63-bit seed.
func SplitMix64(seed uint64) uint64 {
	z := seed + 0x9e3779b97f4a7c15
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// RNG is a deterministic pseudo-random generator for one simulation path.
type RNG struct {
	state uint64
}

// NewRNG creates a path-local RNG from a non-negative 63-bit seed.
func NewRNG(seed int64) *RNG {
	return &RNG{state: seedAsUint64(seed)}
}

func (r *RNG) Uint64() uint64 {
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (r *RNG) Float64() float64 {
	return float64(r.Uint64()>>11) * (1.0 / (1 << 53))
}

func (r *RNG) NormFloat64() float64 {
	for {
		u1 := r.Float64()
		if u1 == 0 {
			continue
		}
		u2 := r.Float64()
		radius := math.Sqrt(-2 * math.Log(u1))
		theta := 2 * math.Pi * u2
		return radius * math.Cos(theta)
	}
}
