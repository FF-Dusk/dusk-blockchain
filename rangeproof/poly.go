package rangeproof

import (
	"math/big"

	"github.com/pkg/errors"

	"github.com/toghrulmaharramov/dusk-go/rangeproof/vector"

	"github.com/toghrulmaharramov/dusk-go/ristretto"
)

// Polynomial construction
type polynomial struct {
	l0, l1, r0, r1 []ristretto.Scalar
	t0, t1, t2     ristretto.Scalar
}

func computePoly(aL, aR, sL, sR []ristretto.Scalar, y, z ristretto.Scalar) (*polynomial, error) {

	// calculate l_0
	l0 := vector.SubScalar(aL, z)

	// calculate l_1

	l1 := sL

	// calculate r_0

	yNM := vector.ScalarPowers(y, uint32(N*M))

	zMTwoN := sumZMTwoN(z)

	r0 := vector.AddScalar(aR[:], z)

	r0, err := vector.Hadamard(r0, yNM)
	if err != nil {
		return nil, errors.Wrap(err, "[ComputePoly] - r0 (1)")
	}
	r0, err = vector.Add(r0, zMTwoN)
	if err != nil {
		return nil, errors.Wrap(err, "[ComputePoly] - r0 (2)")
	}
	// calculate r_1
	r1, err := vector.Hadamard(yNM, sR[:])
	if err != nil {
		return nil, errors.Wrap(err, "[ComputePoly] - r1")
	}

	// calculate t0 // t_0 = <l_0, r_0>
	t0, err := innerProduct(l0, r0)
	if err != nil {
		return nil, errors.Wrap(err, "[ComputePoly] - t0")
	}

	// calculate t1 // t_1 = <l_0, r_1> + <l_1, r_0>
	t1Left, err := innerProduct(l1[:], r0[:])
	if err != nil {
		return nil, errors.Wrap(err, "[ComputePoly] - t1Left")
	}
	t1Right, err := innerProduct(l0, r1)
	if err != nil {
		return nil, errors.Wrap(err, "[ComputePoly] - t1Right")
	}
	var t1 ristretto.Scalar
	t1.Add(&t1Left, &t1Right)

	// calculate t2 // t_2 = <l_1, r_1>
	t2, err := innerProduct(l1[:], r1[:])
	if err != nil {
		return nil, errors.Wrap(err, "[ComputePoly] - t2")
	}
	return &polynomial{
		l0: l0,
		l1: l1[:],
		r0: r0,
		r1: r1,
		t0: t0,
		t1: t1,
		t2: t2,
	}, nil
}

// evalute the polynomial with coefficients t
// t = t_0 + t_1 * x + t_2 x^2
func (p *polynomial) eval(x ristretto.Scalar) ristretto.Scalar {

	var t1x ristretto.Scalar
	t1x.Mul(&x, &p.t1)

	var xsq ristretto.Scalar
	xsq.Square(&x)

	var t2xsq ristretto.Scalar
	t2xsq.Mul(&xsq, &p.t2)

	var t ristretto.Scalar
	t.Add(&t1x, &t2xsq)
	t.Add(&t, &p.t0)

	return t
}

// l = l_0 + l_1 * x
func (p *polynomial) computeL(x ristretto.Scalar) ([]ristretto.Scalar, error) {

	lLeft := p.l0

	lRight := vector.MulScalar(p.l1, x)

	l, err := vector.Add(lLeft, lRight)
	if err != nil {
		return nil, errors.Wrap(err, "[ComputeL]")
	}
	return l, nil
}

// r = r_0 + r_1 * x
func (p *polynomial) computeR(x ristretto.Scalar) ([]ristretto.Scalar, error) {
	rLeft := p.r0

	rRight := vector.MulScalar(p.r1, x)

	r, err := vector.Add(rLeft, rRight)
	if err != nil {
		return nil, errors.Wrap(err, "[computeR]")
	}
	return r, nil
}

// t_0 = z^2 * v + D(y,z)
func (p *polynomial) computeT0(y, z ristretto.Scalar, v []ristretto.Scalar) ristretto.Scalar {

	delta := computeDelta(y, z)

	var zN ristretto.Scalar
	zN.Square(&z)

	var sumZnV ristretto.Scalar
	sumZnV.SetZero()

	for i := range v {
		sumZnV.MulAdd(&zN, &v[i], &sumZnV)
		zN.Mul(&zN, &z)
	}

	var t0 ristretto.Scalar
	t0.SetZero()

	t0.Add(&delta, &sumZnV)

	return t0
}

// calculates sum( z^(1+j) * ( 0^(j-1)n || 2 ^n || 0^(m-j)n ) ) from j = 1 to j=M (71)
// implementation taken directly from java implementation.
// XXX: Look into ways to speed this up, and improve readability
func sumZMTwoN(z ristretto.Scalar) []ristretto.Scalar {

	res := make([]ristretto.Scalar, N*M)

	zM := vector.ScalarPowers(z, uint32(M+3))

	var two ristretto.Scalar
	two.SetBigInt(big.NewInt(2))
	twoN := vector.ScalarPowers(two, N)

	for i := 0; i < M*N; i++ {
		res[i].SetZero()
		for j := 1; j <= M; j++ {
			if (i > (j-1)*N) && (i < j*N) {
				res[i].MulAdd(&zM[j+1], &twoN[i-(j-1)*N], &res[i])
			}
		}

	}
	return res
}

// D(y,z) - This is the data shared by both prover and verifier
func computeDelta(y, z ristretto.Scalar) ristretto.Scalar {
	var res ristretto.Scalar
	res.SetZero()

	var one ristretto.Scalar
	one.SetOne()

	var two ristretto.Scalar
	two.SetBigInt(big.NewInt(2))

	oneDotYNM := vector.ScalarPowersSum(y, uint64(N*M))
	oneDot2N := vector.ScalarPowersSum(two, uint64(N))

	var zsq ristretto.Scalar
	zsq.Square(&z)

	var zM ristretto.Scalar
	zM = zsq

	var zMinusZsq ristretto.Scalar
	zMinusZsq.Sub(&z, &zsq)

	var sumZ ristretto.Scalar
	for i := 1; i <= M; i++ {
		zM.Mul(&zM, &z)
		sumZ.Add(&sumZ, &zM)
	}

	var zMOneDot2N ristretto.Scalar
	zMOneDot2N.Mul(&oneDot2N, &zM)

	res.Mul(&zMinusZsq, &oneDotYNM)
	res.Sub(&res, &zMOneDot2N)

	return res
}
