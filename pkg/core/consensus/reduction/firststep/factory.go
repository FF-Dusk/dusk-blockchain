package firststep

import (
	"time"

	"github.com/dusk-network/dusk-blockchain/pkg/core/consensus"
	"github.com/dusk-network/dusk-blockchain/pkg/util/nativeutils/eventbus"
	"github.com/dusk-network/dusk-blockchain/pkg/util/nativeutils/rpcbus"
	"github.com/dusk-network/dusk-wallet/key"
)

// Factory creates a first step reduction Component
type Factory struct {
	broker  eventbus.Broker
	rpcBus  *rpcbus.RPCBus
	keys    key.ConsensusKeys
	timeout time.Duration
}

// NewFactory instantiates a Factory
func NewFactory(broker eventbus.Broker, rpcBus *rpcbus.RPCBus, keys key.ConsensusKeys, timeout time.Duration) *Factory {
	return &Factory{
		broker,
		rpcBus,
		keys,
		timeout,
	}
}

// Instantiate a first step reduction Component
func (f *Factory) Instantiate() consensus.Component {
	return NewComponent(f.broker, f.rpcBus, f.keys, f.timeout)
}
