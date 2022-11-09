package main

import (
	"fmt"
	"math"
	"math/big"
)

type Pair struct {
	r_from big.Int
	r_to   big.Int
}

func Eb(e1, convertFrom, convertTo *big.Rat) big.Rat {
	//Do conversions at the start to make things cleaner

	eb := new(big.Rat)
	numerator := new(big.Rat)
	denominator := new(big.Rat)
	er := new(big.Rat)

	//pancake swap fee is 0.025 so value out r, is .975
	fee := big.NewRat(975, 1000)
	// (E1*r*ConvertTo)/(ConvertFrom+r*E1)
	//e1 * r

	er.Mul(e1, fee)

	numerator.Mul(er, convertTo)

	denominator = denominator.Add(er, convertFrom)

	eb.Quo(numerator, denominator)

	return *eb
}

func Ea(e0, e1, convertFrom *big.Rat) big.Rat {
	//conversion to Rat

	ea := new(big.Rat)
	numerator := new(big.Rat)
	denominator := new(big.Rat)

	//pancake swap fee is 0.025 so value out r, is .975
	fee := big.NewRat(975, 1000)
	// (E0*ConvertFrom)/(ConvertFrom+r*E1)
	//e1 * r

	numerator.Mul(e0, convertFrom)

	denominator.Mul(e1, fee)
	denominator.Add(denominator, convertFrom)

	ea.Quo(numerator, denominator)
	return *ea
}

func evaluate(e0, f0 *big.Rat, delta *big.Int) big.Int {

	_delta := big.NewRat(delta.Int64(), 1)

	e := new(big.Rat)

	numerator := new(big.Rat)
	denominator := new(big.Rat)

	//pancake swap fee is 0.025 so value out r, is .975

	//E0*delta*r/(F0+r*delta)

	fee := big.NewRat(975, 1000)

	delta_r := new(big.Rat)

	delta_r.Mul(_delta, fee)

	numerator.Mul(f0, _delta)
	denominator.Add(e0, delta_r)

	fmt.Printf("Denom %v, Num %v \n", numerator, denominator)

	e.Quo(numerator, denominator)

	Float, _ := e.Float64()
	return *big.NewInt(int64(math.RoundToEven(Float)))
}

func simplifyArb(eVals [][]big.Rat, pairs []Pair) [][]big.Rat {

	// 4 pairs [(A,B), (B',C), (C',D), (D',A)]
	// evals: [[e0,e1]]

	// first step:
	//	e1_ = ea(e0, e1, B')
	//	e2 + eb(e1, B', C)
	// add to evals [e1_, e2]
	//	evals: [[[e0,e1],[e1_, e2]]

	//Whats returned here
	// [[e0,e1],[e1_, e2], [e2_, e3],[e3_,e4]]

	for i := 2; i < len(pairs); i = i + 1 {
		//loop was started at 1 instead of 2 big issue
		last := len(eVals) - 1
		e0 := eVals[last][0]
		e1 := eVals[last][1]
		e1_ := big.NewRat(pairs[i].r_from.Int64(), 1)
		e2_ := big.NewRat(pairs[i].r_to.Int64(), 1)
		val_i0 := Ea(&e0, &e1, e1_)
		val_i1 := Eb(&e1, e1_, e2_)

		val_i := []big.Rat{val_i0, val_i1}
		eVals = append(eVals, val_i)
	}
	return eVals
}

func findDelta(e0, e1 *big.Rat) big.Int {

	//note that the Rationals are not closed under square roots

	delta := new(big.Int)

	numerator_rat := new(big.Rat)
	x := new(big.Rat)

	// ((Ea*Eb*r)**(1/2)-Ea)/r) : rewritten
	// (Ea*Eb/r)**(1/2)-Ea/r

	//pancake swap fee is 0.025 so value out r, is .975
	fee := big.NewRat(975, 1000)

	x = x.Quo(e0, fee)

	numerator_rat = numerator_rat.Mul(x, e1)

	num_float, _ := numerator_rat.Float64()

	numerator := math.Sqrt(num_float)

	x_float, _ := x.Float64()

	delta = big.NewInt(int64(math.RoundToEven(numerator - x_float)))

	return *delta
}

// Note: this might not work for a swap

func optimalVolume(pairs []Pair) (big.Int, big.Int) {
	eVals := make([][]big.Rat, 0)
	e0 := Ea(big.NewRat(pairs[0].r_from.Int64(), 1),
		big.NewRat(pairs[0].r_to.Int64(), 1),
		big.NewRat(pairs[1].r_from.Int64(), 1),
	)

	e1 := Eb(big.NewRat(pairs[0].r_to.Int64(), 1),
		big.NewRat(pairs[1].r_from.Int64(), 1),
		big.NewRat(pairs[1].r_to.Int64(), 1),
	)
	eVals = append(eVals, []big.Rat{e0, e1})

	eVals_simp := simplifyArb(eVals, pairs)

	ea_val := eVals_simp[len(eVals_simp)-1][0]
	eb_val := eVals_simp[len(eVals_simp)-1][1]

	delta_in := findDelta(&ea_val, &eb_val)

	fmt.Printf("Delta in: %v \n", &delta_in)
	if delta_in.Cmp(big.NewInt(0)) > 0 {
		delta_out := evaluate(&ea_val, &eb_val, &delta_in)
		fmt.Println(ea_val.String(), eb_val.String())
		fmt.Println("In & out: ", delta_in.String(), delta_out.String())
		delta_out.Sub(&delta_out, &delta_in)
		return delta_in, delta_out
	}
	return *big.NewInt(0), *big.NewInt(0)
}

func main() {
	pairs := []Pair{
		{*big.NewInt(100), *big.NewInt(100)},
		{*big.NewInt(50), *big.NewInt(90)},
		{*big.NewInt(30), *big.NewInt(45)},
	}
	fmt.Println(optimalVolume(pairs))

	//e0 := big.NewRat(100,1)
	//e1 := big.NewRat(100, 1)
	//convertFrom := big.NewRat(50, 1)
	//convertTo := big.NewRat(90, 1)

	//fmt.Println(Ea(e0, e1, convertFrom))

	//fmt.Println(Eb(e1, convertFrom, convertTo))

	//x := Ea(e0, e1, convertFrom)

}
