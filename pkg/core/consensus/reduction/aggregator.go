package reduction

import (
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/message"
	"github.com/dusk-network/dusk-blockchain/pkg/util/nativeutils/sortedset"
	log "github.com/sirupsen/logrus"
)

var lg = log.WithField("process", "reduction")

// The Aggregator acts as a de facto storage unit for Reduction messages. Any message
// it receives will be Aggregated into a StepVotes struct, organized by block hash.
// Once the key set for a StepVotes of a certain block hash reaches quorum, this
// StepVotes is passed on to the Reducer by use of the `haltChan` channel.
// An Aggregator should be instantiated on a per-step basis and is no longer usable
// after reaching quorum and sending on `haltChan`.
type Aggregator struct {
	handler *Handler

	voteSets map[string]struct {
		*message.StepVotes
		sortedset.Cluster
	}
}

// NewAggregator returns an instantiated Aggregator, ready for use by both
// reduction steps.
func NewAggregator(handler *Handler) *Aggregator {
	return &Aggregator{
		handler: handler,
		voteSets: make(map[string]struct {
			*message.StepVotes
			sortedset.Cluster
		}),
	}
}

// CollectVote collects a Reduction message, and add its sender public key and signature to the
// StepVotes/Set kept under the corresponding block hash. If the Set reaches or exceeds
// quorum, a result is created with the voted hash and the related StepVotes
// added. The validation of the candidate block is left to the caller
// FIXME: this function should not return error. If it does, it should panic
func (a *Aggregator) CollectVote(ev message.Reduction) *Result {
	hdr := ev.State()
	hash := string(hdr.BlockHash)
	sv, found := a.voteSets[hash]
	if !found {
		sv.StepVotes = message.NewStepVotes()
		sv.Cluster = sortedset.NewCluster()
	}

	if err := sv.StepVotes.Add(ev.SignedHash, hdr.PubKeyBLS, hdr.Step); err != nil {
		// adding the vote to the cluster failed. This is a programming error
		panic(err)
	}

	votes := a.handler.VotesFor(hdr.PubKeyBLS, hdr.Round, hdr.Step)
	for i := 0; i < votes; i++ {
		sv.Cluster.Insert(hdr.PubKeyBLS)
	}
	a.voteSets[hash] = sv
	if sv.Cluster.TotalOccurrences() >= a.handler.Quorum(hdr.Round) {
		// quorum reached
		a.addBitSet(sv.StepVotes, sv.Cluster, hdr.Round, hdr.Step)
		return &Result{hdr.BlockHash, *sv.StepVotes}
	}

	// quorum not reached
	return nil
}

func (a *Aggregator) addBitSet(sv *message.StepVotes, cluster sortedset.Cluster, round uint64, step uint8) {
	committee := a.handler.Committee(round, step)
	sv.BitSet = committee.Bits(cluster.Set)
}
