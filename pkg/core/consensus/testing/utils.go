package testing

import (
	"os"
	"strconv"

	"github.com/dusk-network/dusk-blockchain/pkg/core/consensus/key"
	"github.com/dusk-network/dusk-blockchain/pkg/core/consensus/user"
	"github.com/dusk-network/dusk-blockchain/pkg/core/data/ipc/transactions"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/message"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/topics"
	"github.com/dusk-network/dusk-blockchain/pkg/util/nativeutils/eventbus"
	"github.com/dusk-network/dusk-blockchain/pkg/util/nativeutils/rpcbus"
	"github.com/stretchr/testify/assert"
)

// Block generators will need to communicate with the mempool. So we can
// mock a collector here instead, which just provides them with nothing.
func catchGetMempoolTxsBySize(assert *assert.Assertions, rb *rpcbus.RPCBus) {
	c := make(chan rpcbus.Request, 20)
	assert.NoError(rb.Register(topics.GetMempoolTxsBySize, c))

	go func() {
		for {
			r := <-c
			r.RespChan <- rpcbus.Response{
				Resp: make([]transactions.ContractCall, 0),
				Err:  nil,
			}
		}
	}()
}

func setupProvisioners(assert *assert.Assertions, amount int) (user.Provisioners, []key.Keys) {
	p := user.NewProvisioners()
	keys := make([]key.Keys, amount)
	for i := 0; i < amount; i++ {
		var err error
		keys[i], err = key.NewRandKeys()
		assert.NoError(err)

		// All nodes are given an equal stake and locktime
		assert.NoError(p.Add(keys[i].BLSPubKeyBytes, 100000, 0, 250000))
	}

	return *p, keys
}

func mockProxy(p user.Provisioners) transactions.Proxy {
	return transactions.MockProxy{
		P: &transactions.PermissiveProvisioner{},
		E: &transactions.PermissiveExecutor{
			P: &p,
		},
		BG: &transactions.MockBlockGenerator{},
	}
}

type gossipRouter struct {
	eb eventbus.Publisher
}

func (g *gossipRouter) route(m message.Message) {
	// The incoming message will be a message.SafeBuffer, as it's coming from
	// the consensus.Emitter.
	b := m.Payload().(message.SafeBuffer).Buffer
	m, err := message.Unmarshal(&b)
	if err != nil {
		panic(err)
	}

	g.eb.Publish(m.Category(), m)
}

func rerouteGossip(eb *eventbus.EventBus) {
	router := &gossipRouter{eb}
	gossipListener := eventbus.NewSafeCallbackListener(router.route)
	eb.Subscribe(topics.Gossip, gossipListener)
}

func getNumNodes(assert *assert.Assertions) int {
	numNodesStr := os.Getenv("DUSK_TESTBED_NUM_NODES")
	if numNodesStr != "" {
		numNodes, err := strconv.Atoi(numNodesStr)
		assert.NoError(err)
		return numNodes
	}

	// Standard test case runs with 10 nodes.
	return 10
}

func getNumRounds(assert *assert.Assertions) int {
	numRoundsStr := os.Getenv("DUSK_TESTBED_NUM_ROUNDS")
	if numRoundsStr != "" {
		numRounds, err := strconv.Atoi(numRoundsStr)
		assert.NoError(err)
		return numRounds
	}

	// Standard test case runs for 10 rounds.
	return 10
}
