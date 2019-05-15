package agreement

import (
	"bytes"
	"crypto/rand"

	"github.com/stretchr/testify/mock"
	"gitlab.dusk.network/dusk-core/dusk-go/mocks"
	"gitlab.dusk.network/dusk-core/dusk-go/pkg/core/consensus/events"
	"gitlab.dusk.network/dusk-core/dusk-go/pkg/core/consensus/msg"
	"gitlab.dusk.network/dusk-core/dusk-go/pkg/core/consensus/user"
	"gitlab.dusk.network/dusk-core/dusk-go/pkg/crypto/bls"
	"gitlab.dusk.network/dusk-core/dusk-go/pkg/p2p/wire"
	"gitlab.dusk.network/dusk-core/dusk-go/pkg/util/nativeutils/sortedset"
)

// PublishMock is a mock-up method to facilitate testing of publishing of Agreement events
func PublishMock(bus wire.EventBroker, hash []byte, round uint64, step uint8, keys []*user.Keys) {
	buf := MockAgreement(hash, round, step, keys)
	bus.Publish(msg.OutgoingBlockAgreementTopic, buf)
}

func MockAgreementEvent(hash []byte, round uint64, step uint8, keys []*user.Keys) *Agreement {
	if step < uint8(2) {
		panic("Need at least 2 steps to create an Agreement")
	}
	a := NewAgreement()
	pk, sk, _ := bls.GenKeyPair(rand.Reader)
	a.PubKeyBLS = pk.Marshal()
	a.Round = round
	a.Step = step
	a.BlockHash = hash

	// generating reduction events (votes) and signing them
	steps := genVotes(hash, round, step, keys)
	a.VotesPerStep = steps
	buf := new(bytes.Buffer)
	_ = MarshalVotes(buf, a.VotesPerStep)
	sig, _ := bls.Sign(sk, pk, buf.Bytes())
	a.SignedVotes = sig.Compress()
	return a
}

func MockAgreement(hash []byte, round uint64, step uint8, keys []*user.Keys) *bytes.Buffer {
	if step < 2 {
		panic("Aggregated agreement needs to span for at least two steps")
	}
	buf := new(bytes.Buffer)
	ev := MockAgreementEvent(hash, round, step, keys)

	marshaller := NewAgreementUnMarshaller()
	_ = marshaller.Marshal(buf, ev)
	return buf
}

func genVotes(hash []byte, round uint64, step uint8, keys []*user.Keys) []*StepVotes {
	if len(keys) < 2 {
		panic("At least two votes are required to mock an Agreement")
	}

	votes := make([]*StepVotes, 2)
	for i, k := range keys {

		stepCycle := i % 2
		thisStep := step - uint8((stepCycle+1)%2)
		stepVote := votes[stepCycle]
		if stepVote == nil {
			stepVote = NewStepVotes()
		}

		h := &events.Header{
			BlockHash: hash,
			Round:     round,
			Step:      thisStep,
			PubKeyBLS: k.BLSPubKeyBytes,
		}

		r := new(bytes.Buffer)
		_ = events.MarshalSignableVote(r, h)
		sigma, _ := bls.Sign(k.BLSSecretKey, k.BLSPubKey, r.Bytes())

		if err := stepVote.Add(sigma.Compress(), k.BLSPubKeyBytes, thisStep); err != nil {
			panic(err)
		}
		votes[stepCycle] = stepVote
	}

	return votes
}

func mockCommittee(quorum int, isMember bool, membersNr int) (*mocks.Committee, []*user.Keys) {
	keys := make([]*user.Keys, membersNr)
	mockSubCommittees := make([]sortedset.Set, 2)
	wholeCommittee := sortedset.New()

	// splitting the subcommittees into 2 different sets
	for i := 0; i < membersNr; i++ {
		stepCycle := i % 2
		sc := mockSubCommittees[stepCycle]
		if sc == nil {
			sc = sortedset.New()
		}
		k, _ := user.NewRandKeys()
		sc.Insert(k.BLSPubKeyBytes)
		wholeCommittee.Insert(k.BLSPubKeyBytes)
		keys[i] = k
		mockSubCommittees[stepCycle] = sc
	}

	committeeMock := &mocks.Committee{}
	committeeMock.On("Quorum").Return(quorum)
	committeeMock.On("ReportAbsentees", mock.Anything,
		mock.Anything, mock.Anything).Return(nil)
	committeeMock.On("IsMember",
		mock.AnythingOfType("[]uint8"),
		mock.AnythingOfType("uint64"),
		mock.AnythingOfType("uint8")).Return(isMember)
	committeeMock.On("AmMember",
		mock.AnythingOfType("uint64"),
		mock.AnythingOfType("uint8")).Return(true)
	committeeMock.On("Unpack",
		mock.AnythingOfType("uint64"),
		mock.AnythingOfType("uint64"),
		uint8(1)).Return(mockSubCommittees[0])
	committeeMock.On("Unpack",
		mock.AnythingOfType("uint64"),
		mock.AnythingOfType("uint64"),
		uint8(2)).Return(mockSubCommittees[1])
	committeeMock.On("Pack",
		mock.AnythingOfType("uint64"),
		mock.AnythingOfType("uint64"),
		uint8(1)).Return(wholeCommittee.Bits(mockSubCommittees[0]))
	committeeMock.On("Pack",
		mock.AnythingOfType("uint64"),
		mock.AnythingOfType("uint64"),
		uint8(2)).Return(wholeCommittee.Bits(mockSubCommittees[1]))
	return committeeMock, keys
}
