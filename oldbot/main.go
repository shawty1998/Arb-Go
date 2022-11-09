package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"os"
	"sync"
	"time"

	"example.com/m/pancakeFactory"
	"example.com/m/pancakePair"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Pairs struct {
	Pairs []PairIn `json:"pairs"`
}

type PairIn struct {
	From        common.Address `json:"from"`
	From_symbol string         `json:"from_symbol"`
	To          common.Address `json:"to"`
	To_symbol   string         `json:"to_symbol"`
	Factory     common.Address `json:"factory"`
}

type Pair struct {
	from        common.Address
	to          common.Address
	from_symbol string
	to_symbol   string
	r_from      big.Int
	r_to        big.Int
	price       big.Float
	factory     common.Address
}

type TokenPair struct {
	pair_address  string
	base_name     string
	base_symbol   string
	base_address  string
	quote_name    string
	quote_symbol  string
	quote_address string
	price         string
	base_volume   string
	quote_volume  string
	liquidity     string
	liquidity_BNB string
}

type Graph struct {
	nodes   []*GraphNode
	nodeIds map[string]int
	mu      sync.Mutex
}

type GraphNode struct {
	id       int
	asset    string
	address  common.Address
	edges    map[int]float64
	edgePair map[int]Pair
}

type Edge struct {
	From   int
	To     int
	Weight float64
	pair   Pair
}

func New() *Graph {
	return &Graph{
		nodes:   []*GraphNode{},
		nodeIds: make(map[string]int),
	}
}

func (g *Graph) AddNode(asset string, address common.Address) (id int, exists bool) {
	g.mu.Lock()
	id, exist := g.nodeIds[asset]
	g.mu.Unlock()
	if exist {
		return id, true
	} else {
		g.mu.Lock()
		id = len(g.nodes)
		g.nodes = append(g.nodes, &GraphNode{
			id:       id,
			asset:    asset,
			address:  address,
			edges:    make(map[int]float64),
			edgePair: make(map[int]Pair),
		})
		g.nodeIds[asset] = id
		g.mu.Unlock()
		return id, false
	}
}

func (g *Graph) AddEdge(n1, n2 int, w float64, pair Pair) {
	g.mu.Lock()
	g.nodes[n1].edges[n2] = w
	g.nodes[n1].edgePair[n2] = pair
	g.mu.Unlock()
}

func (g *Graph) Neighbors(id int) []int {
	g.mu.Lock()
	defer g.mu.Unlock()
	neighbors := []int{}
	for _, node := range g.nodes {
		for edge := range node.edges {
			if node.id == id {
				neighbors = append(neighbors, edge)
			}
			if edge == id {
				neighbors = append(neighbors, node.id)
			}
		}
	}
	return neighbors
}

func (g *Graph) Edges() []Edge {
	edges := make([]Edge, 0, len(g.nodes))
	for i := 0; i < len(g.nodes); i++ {
		for k, v := range g.nodes[i].edges {
			pair := g.nodes[i].edgePair[k]
			edges = append(edges, Edge{From: i, To: k, Weight: v, pair: pair})
		}
	}
	return edges
}

func (g *Graph) BellmanFord(source int) ([]int, []float64) {
	size := len(g.nodes)
	distances := make([]float64, size)
	predecessors := make([]int, size)
	for i := 0; i < size; i++ {
		distances[i] = math.MaxFloat64
	}
	distances[source] = 0

	for i, changes := 0, 0; i < size-1; i, changes = i+1, 0 {
		for _, edge := range g.Edges() {
			if newDist := distances[edge.From] + edge.Weight; newDist < distances[edge.To] {
				distances[edge.To] = newDist
				predecessors[edge.To] = edge.From
				changes++
			}
		}
		if changes == 0 {
			break
		}
	}
	return predecessors, distances

}

func (g *Graph) FindNegativeWeightCycle(predecessors []int, distances []float64, source int) []int {
	for _, edge := range g.Edges() {
		if distances[edge.From]+edge.Weight < distances[edge.To] {
			return arbitrageLoop(predecessors, source)
		}
	}
	return nil
}

func arbitrageLoop(predecessors []int, source int) []int {
	size := len(predecessors)
	loop := make([]int, size)
	loop[0] = source

	exists := make([]bool, size)
	exists[source] = true

	indices := make([]int, size)

	var index, next int
	for index, next = 1, source; ; index++ {
		next = predecessors[next]
		loop[index] = next
		if exists[next] {
			return loop[indices[next] : index+1]
		}
		indices[next] = index
		exists[next] = true
	}
}

func (g *Graph) FindArbitrageLoop(source int) []int {
	g.mu.Lock()
	size := len(g.nodes)
	defer g.mu.Unlock()
	if size > 1 {
		predecessors, distances := g.BellmanFord(source)
		return g.FindNegativeWeightCycle(predecessors, distances, source)
	} else {
		return nil
	}
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

func runArb(factory *pancakeFactory.PancakeFactory, pairs []PairIn, client *ethclient.Client) {
	market := New()
	for i := 0; i < len(pairs); i++ {
		pair_address := pairs[i].Factory
		pair_contract, err := pancakePair.NewPancakePair(pair_address, client)
		if err != nil {
			log.Fatal(err)
		}
		reserves, err := pair_contract.GetReserves(nil)
		if err != nil {
			log.Fatal(err)
		}

		from := pairs[i].From_symbol
		to := pairs[i].To_symbol
		r_from := *reserves.Reserve0
		r_to := *reserves.Reserve1
		res0 := new(big.Float).SetInt(reserves.Reserve0)
		res1 := new(big.Float).SetInt(reserves.Reserve1)
		one_token := 10000000000000000.0
		if res0.Cmp(big.NewFloat(one_token)) < 0 || res1.Cmp(big.NewFloat(one_token)) < 0 {
			continue
		}

		price := new(big.Float)
		price_float, _ := price.Quo(res1, res0).Float64()
		// pairs[i].price = *pairs[i].price.Quo(res1, res0)

		from_id, from_exists := market.AddNode(from, pairs[i].From)
		to_id, to_exist := market.AddNode(to, pairs[i].To)

		/**
		Need to change how edges are modeled
		Right now a node is a token of a particular symbol
		This should be a tokenAddress to be correct
		Second there can be multiple edges from one node to the next

		*/

		if from_exists && to_exist {
			existing_price := market.nodes[from_id].edgePair[to_id].price
			// Going A => B you want the biggest price or smaller in reverse
			if price.Cmp(&existing_price) > 0 {
				pair_ := Pair{pairs[i].From, pairs[i].To, pairs[i].From_symbol, pairs[i].To_symbol, r_from, r_to, *price.Quo(res1, res0), pairs[i].Factory}
				market.AddEdge(from_id, to_id, -math.Log(price_float), pair_)
			} else {
				reverse_pair := Pair{pairs[i].To, pairs[i].From, pairs[i].To_symbol, pairs[i].From_symbol, r_to, r_from, *price.Quo(res0, res1), pairs[i].Factory}
				market.AddEdge(to_id, from_id, -math.Log(1/(price_float)), reverse_pair)
			}
		} else {
			pair_ := Pair{pairs[i].From, pairs[i].To, pairs[i].From_symbol, pairs[i].To_symbol, r_from, r_to, *price.Quo(res1, res0), pairs[i].Factory}
			market.AddEdge(from_id, to_id, -math.Log(price_float), pair_)
			reverse_pair := Pair{pairs[i].To, pairs[i].From, pairs[i].To_symbol, pairs[i].From_symbol, r_to, r_from, *price.Quo(res0, res1), pairs[i].Factory}
			market.AddEdge(to_id, from_id, -math.Log(1/(price_float)), reverse_pair)
		}
		fmt.Println(&market)
	}

	//Find Arbs starting from the tokens below
	sources := [3]int{market.nodeIds["WBNB"], market.nodeIds["BUSD"], market.nodeIds["USDT"]}
	loops := [][]int{}
	for i := 0; i < len(sources); i++ {
		loop := market.FindArbitrageLoop(sources[i])
		loops = append(loops, loop)
	}

	var arbPairs = make([][]Pair, len(loops))
	for loop_i, loop := range loops {
		market.mu.Lock()
		var value = 1.0
		for i, j := 0, 1; i < len(loop)-1; i, j = i+1, j+1 {
			arb_pair := market.nodes[loop[i]].edgePair[loop[j]]
			price, _ := arb_pair.price.Float64()
			arbPairs[loop_i] = append(arbPairs[loop_i], arb_pair)
			value = value * price
		}
		if len(arbPairs[loop_i]) > 0 {
			// fmt.Println(arbPairs[loop_i])
			value := 1.0
			for _, pair_in_arb := range arbPairs[loop_i] {
				price_in_pair, _ := pair_in_arb.price.Float64()
				value *= price_in_pair
				fmt.Println(pair_in_arb.from_symbol, pair_in_arb.to_symbol, price_in_pair, pair_in_arb.factory.String(), pair_in_arb.r_from.Uint64(), pair_in_arb.r_to.Uint64())
			}
			delta_in, profit := optimalVolume(arbPairs[loop_i])
			fmt.Println(delta_in.String(), profit.String())
			if delta_in.Cmp(big.NewInt(0)) > 0 {
				fmt.Printf("Expected Return: %0.2f%%\n", ((value - 1) * 100))
				fmt.Println("Tokens in wei in: ", delta_in.String())
				fmt.Println("Expected profit in wei: ", profit.String())
				fmt.Println()
			}
		}
		market.mu.Unlock()
	}
}

func main() {
	//Binance Client
	client, err := ethclient.Dial("https://bsc-dataseed.binance.org/")
	if err != nil {
		log.Fatal(err)
	}
	//Read Pairs from file
	jsonFile, _ := os.Open("./tokenPairs_final.json")
	defer jsonFile.Close()
	byteValue, _ := io.ReadAll(jsonFile)
	var read_pairs []PairIn
	json.Unmarshal(byteValue, &read_pairs)

	// //Get pancake factory
	address := common.HexToAddress("0xca143ce32fe78f1f7019d7d551a6402fc5350c73")
	factory, err := pancakeFactory.NewPancakeFactory(address, client)
	if err != nil {
		log.Fatal(err)
	}

	var searches = 0
	for searches < 5 {
		fmt.Println("Search: ", searches)
		runArb(factory, read_pairs, client)
		time.Sleep(10 * time.Second)
		searches++
	}

	return
}
