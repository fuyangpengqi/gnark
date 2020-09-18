package algorithms

// FFT ...
const FFT = `

import (
	"math/bits"
	"runtime"
	"sync"

	{{ template "import_curve" . }}
)

type FFTType uint8

const (
	DIT FFTType = iota
	DIF 
)

const fftParallelThreshold = 64

var numCpus = uint(runtime.NumCPU())

// FFT computes (recursively) the discrete Fourier transform of a and stores the result in a.
// if fType == DIT (decimation in time), the input must be in bit-reversed order
// if fType == DIF (decimation in frequency), the output will be in bit-reversed order
// len(a) must be a power of 2, and w must be a len(a)th root of unity in field F.
func FFT(a []fr.Element, w fr.Element, fType FFTType) {
	switch fType {
	case DIF:
		var wg sync.WaitGroup
		difFFT(a, w, 1, nil)
		wg.Wait()
	case DIT:
		ditFFT(a, w, 1, nil)
	default:
		panic("not implemented")
	}
}

func difFFT(a []fr.Element, w fr.Element, splits uint, chDone chan struct{}) {
	if chDone != nil {
		defer func() {
			chDone <- struct{}{}
		}()
	}
	n := len(a)
	if n == 1 {
		return
	}
	m := n >> 1

	// i == 0
	t := a[0]
	a[0].Add(&a[0], &a[m])
	a[m].Sub(&t, &a[m])

	// if m == 1, then next iteration ends, no need to call 2 extra functions for that
	if m == 1 {
		return
	}

	// wPow == w^1
	wPow := w

	for i := 1; i < m; i++ {
		t = a[i]
		a[i].Add(&a[i], &a[i+m])

		a[i+m].
			Sub(&t, &a[i+m]).
			Mul(&a[i+m], &wPow)

		wPow.Mul(&wPow, &w)
	}

	// note: w is passed by value
	w.Square(&w)

	serial := (splits<<1) > numCpus || m <= fftParallelThreshold

	if serial {
		difFFT(a[0:m], w, splits,nil)
		difFFT(a[m:n], w, splits,nil)
	} else {
		splits <<= 1
		chDone := make(chan struct{}, 1)
		go difFFT(a[m:n], w, splits,chDone)
		difFFT(a[0:m], w, splits,nil)
		<-chDone
	}

}


func ditFFT(a []fr.Element, w fr.Element, splits uint, chDone chan struct{})  {
	if chDone != nil {
		defer func() {
			chDone <- struct{}{}
		}()
	}
	n := len(a)
	if n == 1 {
		return
	}
	m := n >> 1
	var wSquare fr.Element
	wSquare.Square(&w)

	serial := (splits<<1) > numCpus || m <= fftParallelThreshold

	if serial {
		ditFFT(a[0:m], wSquare,  splits, nil) // even
		ditFFT(a[m:], wSquare,  splits, nil)  // odds
	} else {
		splits <<= 1
		chDone := make(chan struct{}, 1)
		go ditFFT(a[m:n], wSquare,  splits, chDone)
		ditFFT(a[0:m], wSquare, splits, nil)
		<-chDone
	}
	var tm fr.Element

	// k == 0
	// wPow == 1
	t := a[0]
	a[0].Add(&a[0], &a[m])
	a[m].Sub(&t, &a[m])

	if m == 1 {
		return
	}

	// k == 1 
	// wPow == w
	t = a[1]
	tm.Mul(&a[1+m], &w)
	a[1].Add(&a[1], &tm)
	a[1+m].Sub(&t, &tm)
	
	// k > 2
	wPow := wSquare
	for k := 2; k < m; k++ {
		t = a[k]
		tm.Mul(&a[k+m], &wPow)
		a[k].Add(&a[k], &tm)

		a[k+m].Sub(&t, &tm)

		wPow.Mul(&wPow, &w)
	}
}

// BitReverse applies the bit-reversal permutation to a.
// len(a) must be a power of 2 (as in every single function in this file)
func BitReverse(a []fr.Element) {
	n := uint(len(a))
	nn := uint(bits.UintSize - bits.TrailingZeros(n))

	for i := uint(0); i < n; i++ {
		irev := bits.Reverse(i) >> nn
		if irev > i {
			a[i], a[irev] = a[irev],a[i]
		}
	}
}

// Domain with a power of 2 cardinality
// compute a field element of order 2x and store it in GeneratorSqRt
// all other values can be derived from x, GeneratorSqrt
type Domain struct {
	Generator        fr.Element
	GeneratorInv     fr.Element
	GeneratorSqRt    fr.Element // generator of 2 adic subgroup of order 2*nb_constraints
	GeneratorSqRtInv fr.Element
	Cardinality      int
	CardinalityInv   fr.Element
}

// NewDomain returns a subgroup with a power of 2 cardinality
// cardinality >= m
// compute a field element of order 2x and store it in GeneratorSqRt
// all other values can be derived from x, GeneratorSqrt
func NewDomain(m int) *Domain {

	// generator of the largest 2-adic subgroup
	var rootOfUnity fr.Element
	{{if eq .Curve "BLS377"}}
		rootOfUnity.SetString("8065159656716812877374967518403273466521432693661810619979959746626482506078")
		const maxOrderRoot uint = 47
	{{else if eq .Curve "BLS381"}}
		rootOfUnity.SetString("10238227357739495823651030575849232062558860180284477541189508159991286009131")
		const maxOrderRoot uint = 32
	{{else if eq .Curve "BN256"}}
		rootOfUnity.SetString("19103219067921713944291392827692070036145651957329286315305642004821462161904")
		const maxOrderRoot uint = 28
	{{else if eq .Curve "BW761"}}
		rootOfUnity.SetString("32863578547254505029601261939868325669770508939375122462904745766352256812585773382134936404344547323199885654433")
		const maxOrderRoot uint = 46
	{{end}}
	

	subGroup := &Domain{}
	x := nextPowerOfTwo(uint(m))

	// maxOderRoot is the largest power-of-two order for any element in the field
	// set subGroup.GeneratorSqRt = rootOfUnity^(2^(maxOrderRoot-log(x)-1))
	// to this end, compute expo = 2^(maxOrderRoot-log(x)-1)
	logx := uint(bits.TrailingZeros(x))
	if logx > maxOrderRoot-1 {
		panic("m is too big: the required root of unity does not exist")
	}
	expo := uint64(1 << (maxOrderRoot - logx - 1))
	bExpo := new(big.Int).SetUint64(expo)
	subGroup.GeneratorSqRt.Exp(rootOfUnity, bExpo)

	// Generator = GeneratorSqRt^2 has order x
	subGroup.Generator.Mul(&subGroup.GeneratorSqRt, &subGroup.GeneratorSqRt) // order x
	subGroup.Cardinality = int(x)
	subGroup.GeneratorSqRtInv.Inverse(&subGroup.GeneratorSqRt)
	subGroup.GeneratorInv.Inverse(&subGroup.Generator)
	subGroup.CardinalityInv.SetUint64(uint64(x)).Inverse(&subGroup.CardinalityInv)

	return subGroup
}

func nextPowerOfTwo(n uint) uint {
	p := uint(1)
	if (n & (n - 1)) == 0 {
		return n
	}
	for p < n {
		p <<= 1
	}
	return p
}


`
