package reduction

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dusk-network/dusk-blockchain/pkg/core/data/block"
	"github.com/dusk-network/dusk-blockchain/pkg/core/data/ipc/transactions"
	"github.com/dusk-network/dusk-blockchain/pkg/util/nativeutils/eventbus"
	"github.com/stretchr/testify/require"

	"github.com/dusk-network/dusk-blockchain/pkg/core/consensus"
	"github.com/dusk-network/dusk-blockchain/pkg/core/consensus/agreement"
	"github.com/dusk-network/dusk-blockchain/pkg/core/consensus/header"
	"github.com/dusk-network/dusk-blockchain/pkg/core/consensus/key"
	"github.com/dusk-network/dusk-blockchain/pkg/core/consensus/user"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/message"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/protocol"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/topics"
)

// PrepareSendReductionTest tests that the reduction step completes without problems
// and produces a StepVotesMsg in case it receives enough valid Reduction messages
func PrepareSendReductionTest(hlp *Helper, stepFn consensus.PhaseFn) func(t *testing.T) {
	return func(t *testing.T) {
		require := require.New(t)

		streamer := eventbus.NewGossipStreamer(protocol.TestNet)
		hlp.EventBus.Subscribe(topics.Gossip, eventbus.NewStreamListener(streamer))

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			_, err := streamer.Read()
			require.NoError(err)
			require.Equal(streamer.SeenTopics()[0], topics.Reduction)
			cancel()
		}()

		evChan := make(chan message.Message, 1)
		n := stepFn.Run(ctx, consensus.NewQueue(), evChan, consensus.MockRoundUpdate(uint64(1), hlp.P), uint8(2))
		require.Nil(n)
	}
}

// Helper for reducing test boilerplate
type Helper struct {
	*consensus.Emitter
	lock               sync.RWMutex
	failOnVerification bool

	ThisSender       []byte
	ProvisionersKeys []key.Keys
	P                *user.Provisioners
	Nr               int
	Handler          *Handler
}

// NewHelper creates a Helper
func NewHelper(provisioners int, timeOut time.Duration) *Helper {
	p, provisionersKeys := consensus.MockProvisioners(provisioners)

	mockProxy := transactions.MockProxy{
		P:  transactions.PermissiveProvisioner{},
		BG: transactions.MockBlockGenerator{},
	}
	emitter := consensus.MockEmitter(timeOut, mockProxy)
	emitter.Keys = provisionersKeys[0]

	hlp := &Helper{
		failOnVerification: false,

		ThisSender:       emitter.Keys.BLSPubKeyBytes,
		ProvisionersKeys: provisionersKeys,
		P:                p,
		Nr:               provisioners,
		Handler:          NewHandler(emitter.Keys, *p),
		Emitter:          emitter,
	}

	return hlp
}

// Verify StepVotes. The step must be specified otherwise verification would be dependent on the state of the Helper
func (hlp *Helper) Verify(hash []byte, sv message.StepVotes, round uint64, step uint8) error {
	vc := hlp.P.CreateVotingCommittee(round, step, hlp.Nr)
	sub := vc.IntersectCluster(sv.BitSet)
	apk, err := agreement.ReconstructApk(sub.Set)
	if err != nil {
		return err
	}

	return header.VerifySignatures(round, step, hash, apk, sv.Signature)
}

// Spawn a number of different valid events to the Agreement component bypassing the EventBus
func (hlp *Helper) Spawn(hash []byte, round uint64, step uint8) []message.Reduction {
	evs := make([]message.Reduction, 0, hlp.Nr)
	i := 0
	for count := 0; count < hlp.Handler.Quorum(round); {
		ev := message.MockReduction(hash, round, step, hlp.ProvisionersKeys, i)
		i++
		evs = append(evs, ev)
		count += hlp.Handler.VotesFor(hlp.ProvisionersKeys[i].BLSPubKeyBytes, round, step)
	}
	return evs
}

// FailOnVerification tells the RPC bus to return an error
func (hlp *Helper) FailOnVerification(flag bool) {
	hlp.lock.Lock()
	defer hlp.lock.Unlock()
	hlp.failOnVerification = flag
}

func (hlp *Helper) shouldFailVerification() bool {
	hlp.lock.RLock()
	defer hlp.lock.RUnlock()
	f := hlp.failOnVerification
	return f
}

// ProcessCandidateVerificationRequest is a callback used by the firststep
// reduction to verify potential winning candidates.
func (hlp *Helper) ProcessCandidateVerificationRequest(blk block.Block) error {
	if hlp.shouldFailVerification() {
		return errors.New("verification failed")
	}

	return nil
}
