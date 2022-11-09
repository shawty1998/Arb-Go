package main_old

// TODO: implmementation https://gist.github.com/ewancook/d39a06b2ee6e3f7c7c4d6cb66f2dcff7

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Book struct {
	UpdateId int64  `json:"u"`
	Symbol   string `json:"s"`
	Bid      string `json:"b"`
	BidQty   string `json:"B"`
	Ask      string `json:"a"`
	AskQty   string `json:"A"`
}

type Graph struct {
	nodes   []*GraphNode
	nodeIds map[string]int
	mu      sync.Mutex
}

type GraphNode struct {
	id    int
	asset string
	edges map[int]float64
}

type Edge struct {
	From   int
	To     int
	Weight float64
}

func New() *Graph {
	return &Graph{
		nodes:   []*GraphNode{},
		nodeIds: make(map[string]int),
	}
}

func (g *Graph) AddNode(asset string) (id int) {
	g.mu.Lock()
	id, exist := g.nodeIds[asset]
	g.mu.Unlock()
	if exist {
		return id
	} else {
		g.mu.Lock()
		id = len(g.nodes)
		g.nodes = append(g.nodes, &GraphNode{
			id:    id,
			asset: asset,
			edges: make(map[int]float64),
		})
		g.nodeIds[asset] = id
		g.mu.Unlock()
		return id

	}
}

func (g *Graph) AddEdge(n1, n2 int, w float64) {
	g.mu.Lock()
	g.nodes[n1].edges[n2] = w
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
			edges = append(edges, Edge{From: i, To: k, Weight: v})
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

func run_real() {
	// websocket client connection
	c, _, err := websocket.DefaultDialer.Dial("wss://stream.binance.com:9443/ws/!bookTicker", nil)
	if err != nil {
		panic(err)
	}
	defer c.Close()

	input := make(chan Book) // 1️⃣
	go func() {              // 2️⃣
		// read from the websocket
		for {
			_, message, err := c.ReadMessage() // 3️⃣
			if err != nil {
				break
			}
			// unmarshal the message
			var book Book
			json.Unmarshal(message, &book) // 4️⃣
			// send the trade to the channel
			input <- book // 5️⃣
		}
		close(input) // 6️⃣
	}()

	market := New()
	go func() {
		for trade := range input {
			if strings.Contains(trade.Symbol, "ETH") && !strings.Contains(trade.Symbol, "ETHUP") && !strings.Contains(trade.Symbol, "ETHDOWN") {
				// split the pair
				var other string
				var eth string
				eth_pair_index := strings.Index(trade.Symbol, "ETH")
				if eth_pair_index == 0 {
					other = trade.Symbol[3:]
					eth = trade.Symbol[:3]

					other_node := market.AddNode(other)
					eth_node := market.AddNode(eth)

					// Buy Eth for bid amount of other, sell eth of ask amount of other
					// ETH -> OTHER @ bid
					// Other -> ETH @ ask
					var Bid, Ask float64
					var errBid, errAsk error

					Bid, errBid = strconv.ParseFloat(trade.Bid, 64)
					Ask, errAsk = strconv.ParseFloat(trade.Ask, 64)
					if errAsk == nil && errBid == nil {
						market.AddEdge(eth_node, other_node, -math.Log(Bid))
						market.AddEdge(other_node, eth_node, -math.Log(1/Ask))
					}

					// fmt.Println(other_node, eth_node, trade.Symbol)
				} else {
					other = trade.Symbol[:eth_pair_index]
					eth = trade.Symbol[eth_pair_index:]
					other_node := market.AddNode(other)
					eth_node := market.AddNode(eth)

					// Buy Other for bid amount of eth, sell other for ask amount of eth
					// OTHER -> ETH @ bid
					// ETH -> OTHER @ ask
					var Bid, Ask float64
					var errBid, errAsk error

					Bid, errBid = strconv.ParseFloat(trade.Bid, 64)
					Ask, errAsk = strconv.ParseFloat(trade.Ask, 64)
					if errAsk == nil && errBid == nil {
						market.AddEdge(other_node, eth_node, -math.Log(Bid))
						market.AddEdge(eth_node, other_node, -math.Log(1/Ask))
					}

					// fmt.Println(other_node, eth_node, trade.Symbol)
				}
			} else if strings.Contains(trade.Symbol, "BNB") {
				// split the pair
				var other string
				var eth string
				eth_pair_index := strings.Index(trade.Symbol, "BNB")
				if eth_pair_index == 0 {
					other = trade.Symbol[3:]
					eth = trade.Symbol[:3]

					other_node := market.AddNode(other)
					eth_node := market.AddNode(eth)

					// Buy Eth for bid amount of other, sell eth of ask amount of other
					// ETH -> OTHER @ bid
					// Other -> ETH @ ask
					var Bid, Ask float64
					var errBid, errAsk error

					Bid, errBid = strconv.ParseFloat(trade.Bid, 64)
					Ask, errAsk = strconv.ParseFloat(trade.Ask, 64)
					if errAsk == nil && errBid == nil {
						market.AddEdge(eth_node, other_node, -math.Log(Bid))
						market.AddEdge(other_node, eth_node, -math.Log(1/Ask))
					}

					// fmt.Println(other_node, eth_node, trade.Symbol)
				} else {
					other = trade.Symbol[:eth_pair_index]
					eth = trade.Symbol[eth_pair_index:]
					other_node := market.AddNode(other)
					eth_node := market.AddNode(eth)

					// Buy Other for bid amount of eth, sell other for ask amount of eth
					// OTHER -> ETH @ bid
					// ETH -> OTHER @ ask
					var Bid, Ask float64
					var errBid, errAsk error

					Bid, errBid = strconv.ParseFloat(trade.Bid, 64)
					Ask, errAsk = strconv.ParseFloat(trade.Ask, 64)
					if errAsk == nil && errBid == nil {
						market.AddEdge(other_node, eth_node, -math.Log(Bid))
						market.AddEdge(eth_node, other_node, -math.Log(1/Ask))
					}

					// fmt.Println(other_node, eth_node, trade.Symbol)
				}
			}
		}
	}()

	var max_found_arb float64
	var max_found_arb_path []string
	max_found_arb_time := time.Now()

	for {
		market.mu.Lock()
		sources := [2]int{market.nodeIds["BNB"], market.nodeIds["ETH"]}
		market.mu.Unlock()
		loops := [][]int{}
		for _, source := range sources {
			loop := market.FindArbitrageLoop(source)
			loops = append(loops, loop)
		}
		arbs := []float64{}
		for _, loop := range loops {
			if loop != nil {
				market.mu.Lock()

				var value float64 = 0
				for i, j := 0, 1; i < len(loop)-1; i, j = i+1, j+1 {
					node_i := loop[i]
					node_j := loop[j]
					value += market.nodes[node_i].edges[node_j]
				}
				arb := 1 - (math.Exp((-value)))
				arbs = append(arbs, arb)
				market.mu.Unlock()
			}
		}
		var max_arb float64
		var max_arb_i int
		for i, arb := range arbs {
			if i == 0 || arb > max_arb {
				max_arb = arb
				max_arb_i = i
			}
		}
		max_loop := loops[max_arb_i]
		pairs := []string{}
		for _, v := range max_loop {
			market.mu.Lock()
			pairs = append(pairs, market.nodes[v].asset)
			market.mu.Unlock()
		}
		if max_arb > max_found_arb || max_found_arb_time.Add(1*time.Minute).Before(time.Now()) {
			max_found_arb = max_arb
			max_found_arb_path = pairs
			max_found_arb_time = time.Now()

			fmt.Println("Path:", max_found_arb_path)
			fmt.Println("Percentage yield:", fmt.Sprintf("%.2f", max_found_arb*100))
			fmt.Println("Found @:", max_found_arb_time)
		}

	}
}

// TODO: Given a path, find the highest profit volume
//

func test() {
	test_graph := New()
	from := test_graph.AddNode("0")
	to := test_graph.AddNode("1")
	test_graph.AddEdge(from, to, -math.Log(1.380))

	to = test_graph.AddNode("2")
	from = test_graph.AddNode("1")
	test_graph.AddEdge(from, to, -math.Log(3.08))

	to = test_graph.AddNode("3")
	from = test_graph.AddNode("2")
	test_graph.AddEdge(from, to, -math.Log(15.120))

	to = test_graph.AddNode("4")
	from = test_graph.AddNode("3")
	test_graph.AddEdge(from, to, -math.Log(0.012))

	to = test_graph.AddNode("0")
	from = test_graph.AddNode("4")
	test_graph.AddEdge(from, to, -math.Log(1.30))

	to = test_graph.AddNode("5")
	from = test_graph.AddNode("4")
	test_graph.AddEdge(from, to, -math.Log(0.57))

	fmt.Println(test_graph.FindArbitrageLoop(0))
}

func main_old() {
	run_real()
}
