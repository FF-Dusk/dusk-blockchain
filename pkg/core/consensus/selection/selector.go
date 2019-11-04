package selection

import (
	"bytes"
	"sync"
	"time"

	"github.com/dusk-network/dusk-blockchain/pkg/core/consensus"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/topics"
	"github.com/dusk-network/dusk-blockchain/pkg/util/nativeutils/eventbus"
	log "github.com/sirupsen/logrus"
)

var lg *log.Entry = log.WithField("process", "selector")
var emptyScore = Score{}

type Selector struct {
	publisher eventbus.Publisher
	handler   Handler
	lock      sync.RWMutex
	bestEvent *Score

	timer   *timer
	timeout time.Duration

	scoreID uint32

	eventPlayer consensus.EventPlayer
	signer      consensus.Signer
}

// NewComponent creates and launches the component which responsibility is to validate
// and select the best score among the blind bidders. The component publishes under
// the topic BestScoreTopic
func NewComponent(publisher eventbus.Publisher, timeout time.Duration) *Selector {
	return &Selector{
		timeout:   timeout,
		publisher: publisher,
	}
}

func (s *Selector) Initialize(eventPlayer consensus.EventPlayer, signer consensus.Signer, r consensus.RoundUpdate) []consensus.TopicListener {
	s.eventPlayer = eventPlayer
	s.signer = signer
	s.handler = NewScoreHandler(r.BidList)
	s.timer = &timer{s: s}

	scoreSubscriber := consensus.TopicListener{
		Topic:         topics.Score,
		Preprocessors: []eventbus.Preprocessor{&consensus.Validator{}},
		Listener:      consensus.NewSimpleListener(s.CollectScoreEvent, consensus.LowPriority, false),
	}
	s.scoreID = scoreSubscriber.ID()

	generationSubscriber := consensus.TopicListener{
		Topic:    topics.Generation,
		Listener: consensus.NewSimpleListener(s.CollectGeneration, consensus.HighPriority, false),
	}

	return []consensus.TopicListener{scoreSubscriber, generationSubscriber}
}

func (s *Selector) ID() uint32 {
	return s.scoreID
}

func (s *Selector) Finalize() {
	s.stopSelection()
}

func (s *Selector) CollectScoreEvent(e consensus.Event) error {
	ev := Score{}
	if err := UnmarshalScore(&e.Payload, &ev); err != nil {
		return err
	}

	return s.Process(ev)
}

func (s *Selector) CollectGeneration(e consensus.Event) error {
	s.setBestEvent(emptyScore)
	_ = s.eventPlayer.Play(s.ID())
	s.startSelection()
	return nil
}

func (s *Selector) startSelection() {
	// Empty queue in a goroutine to avoid letting other listeners wait
	go s.eventPlayer.Resume(s.scoreID)
	s.timer.start(s.timeout)
}

func (s *Selector) stopSelection() {
	s.eventPlayer.Pause(s.scoreID)
	s.timer.stop()
}

func (s *Selector) Process(ev Score) error {
	bestEvent := s.getBestEvent()

	// Only check for priority if we already have a best event
	if !bestEvent.Equal(emptyScore) {
		if s.handler.Priority(bestEvent, ev) {
			// if the current best score has priority, we return
			return nil
		}
	}

	lg.WithFields(log.Fields{
		"current best": bestEvent.Score,
		"new best":     ev.Score,
	}).Debugln("swapping best score")
	s.setBestEvent(ev)
	if err := s.handler.Verify(ev); err != nil {
		s.setBestEvent(bestEvent)
		return err
	}

	if err := s.repropagate(ev); err != nil {
		s.setBestEvent(bestEvent)
		return err
	}

	return nil
}

func (s *Selector) IncreaseTimeOut() {
	s.timeout = s.timeout * 2
}

func (s *Selector) publishBestEvent() error {
	s.eventPlayer.Pause(s.scoreID)
	buf := new(bytes.Buffer)
	bestEvent := s.getBestEvent()
	// If we had no best event, we should send an empty hash
	if bestEvent.Equal(emptyScore) {
		s.signer.SendWithHeader(topics.BestScore, make([]byte, 32), buf, s.ID())
	} else {
		s.signer.SendWithHeader(topics.BestScore, bestEvent.VoteHash, buf, s.ID())
	}

	s.handler.LowerThreshold()
	s.IncreaseTimeOut()
	return nil
}

func (s *Selector) repropagate(ev Score) error {
	buf := new(bytes.Buffer)
	if err := MarshalScore(buf, &ev); err != nil {
		return err
	}

	s.signer.SendAuthenticated(topics.Score, ev.VoteHash, buf, s.ID())
	return nil
}

func (s *Selector) getBestEvent() Score {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return *s.bestEvent
}

func (s *Selector) setBestEvent(ev Score) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.bestEvent = &ev
}
