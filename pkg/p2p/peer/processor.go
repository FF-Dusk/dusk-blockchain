package peer

import (
	"bytes"

	log "github.com/sirupsen/logrus"

	"github.com/dusk-network/dusk-blockchain/pkg/p2p/peer/dupemap"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/message"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/topics"
	"github.com/dusk-network/dusk-blockchain/pkg/util/nativeutils/eventbus"
)

// ProcessorFunc defines an interface for callbacks which can be registered
// to the MessageProcessor, in order to process messages from the network.
type ProcessorFunc func(message.Message) ([]*bytes.Buffer, error)

// MessageProcessor is connected to all of the processing units that are tied to the peer.
// It sends an incoming message in the right direction, according to its topic.
type MessageProcessor struct {
	dupeMap    *dupemap.DupeMap
	processors map[topics.Topic]ProcessorFunc
}

// NewMessageProcessor returns an initialized MessageProcessor.
func NewMessageProcessor(bus eventbus.Broker) *MessageProcessor {
	return &MessageProcessor{
		dupeMap:    dupemap.Launch(bus),
		processors: make(map[topics.Topic]ProcessorFunc),
	}
}

// Register a method to a certain topic. This method will be called when a message
// of the given topic is received.
func (m *MessageProcessor) Register(topic topics.Topic, fn ProcessorFunc) {
	m.processors[topic] = fn
}

// Collect a message from the network. The message is unmarshaled and passed down
// to the processing function.
func (m *MessageProcessor) Collect(packet []byte, respChan chan<- bytes.Buffer) error {
	b := bytes.NewBuffer(packet)
	msg, err := message.Unmarshal(b)
	if err != nil {
		return err
	}
	return m.process(msg, respChan)
}

// CanRoute determines whether or not a message needs to be filtered by the
// dupemap.
// TODO: rename
func (m *MessageProcessor) CanRoute(topic topics.Topic) bool {
	switch topic {
	case topics.Tx,
		topics.Candidate,
		topics.Score,
		topics.Reduction,
		topics.Agreement,
		topics.GetCandidate,
		topics.RoundResults:
		return true
	}

	return false
}

func (m *MessageProcessor) process(msg message.Message, respChan chan<- bytes.Buffer) error {
	category := msg.Category()
	if m.CanRoute(category) {
		if !m.dupeMap.CanFwd(bytes.NewBuffer(msg.Id())) {
			return nil
		}
	}

	processFn, ok := m.processors[category]
	if !ok {
		log.WithField("topic", category).Debugln("received message with unknown topic")
		return nil
	}

	bufs, err := processFn(msg)
	if err != nil {
		return err
	}

	for _, buf := range bufs {
		respChan <- *buf
	}

	return nil
}
