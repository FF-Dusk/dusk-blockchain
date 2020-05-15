package mempool

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/dusk-network/dusk-blockchain/pkg/config"
	"github.com/dusk-network/dusk-blockchain/pkg/core/consensus"
	"github.com/dusk-network/dusk-blockchain/pkg/core/data/block"
	"github.com/dusk-network/dusk-blockchain/pkg/core/data/transactions"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/peer/peermsg"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/encoding"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/message"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/topics"
	"github.com/dusk-network/dusk-blockchain/pkg/util/nativeutils/eventbus"
	"github.com/dusk-network/dusk-blockchain/pkg/util/nativeutils/rpcbus"
	"github.com/dusk-network/dusk-crypto/merkletree"
	"github.com/dusk-network/dusk-protobuf/autogen/go/node"
	logger "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

var log = logger.WithFields(logger.Fields{"prefix": "mempool"})

const (
	//consensusSeconds = 20
	maxPendingLen = 1000
)

var (
	// ErrCoinbaseTxNotAllowed coinbase tx must be built by block generator only
	ErrCoinbaseTxNotAllowed = errors.New("coinbase tx not allowed")
	// ErrAlreadyExists transaction with same txid already exists in
	ErrAlreadyExists = errors.New("already exists")
	// ErrDoubleSpending transaction uses outputs spent in other mempool txs
	ErrDoubleSpending = errors.New("double-spending in mempool")
)

// Mempool is a storage for the chain transactions that are valid according to the
// current chain state and can be included in the next block.
type Mempool struct {
	getMempoolTxsChan       <-chan rpcbus.Request
	getMempoolTxsBySizeChan <-chan rpcbus.Request
	sendTxChan              <-chan rpcbus.Request

	// transactions emitted by RPC and Peer subsystems
	// pending to be verified before adding them to verified pool
	pending chan TxDesc

	// verified txs to be included in next block
	verified Pool

	// the collector to listen for new intermediate blocks
	intermediateBlockChan <-chan block.Block
	acceptedBlockChan     <-chan block.Block

	// used by tx verification procedure
	latestBlockTimestamp int64

	eventBus *eventbus.EventBus

	// the magic function that knows best what is valid chain Tx
	verifier transactions.Verifier
	quitChan chan struct{}

	// ID of subscription to the TX topic on the EventBus
	txSubscriberID uint32

	ctx context.Context
}

// checkTx is responsible to determine if a tx is valid or not.
// Among the other checks, the underlying verifier also checks double spending
func (m *Mempool) checkTx(tx transactions.ContractCall) error {
	ctx, cancel := context.WithDeadline(m.ctx, time.Now().Add(500*time.Millisecond))
	defer cancel()

	// check if external verifyTx is provided
	if err := m.verifier.VerifyTransaction(ctx, tx); err != nil {
		return fmt.Errorf("transaction verification failed: %v", err)
	}
	return nil
}

// NewMempool instantiates and initializes node mempool
func NewMempool(ctx context.Context, eventBus *eventbus.EventBus, rpcBus *rpcbus.RPCBus, verifier transactions.Verifier, srv *grpc.Server) *Mempool {

	log.Infof("Create instance")

	getMempoolTxsChan := make(chan rpcbus.Request, 1)
	if err := rpcBus.Register(topics.GetMempoolTxs, getMempoolTxsChan); err != nil {
		log.Errorf("rpcbus.GetMempoolTxs err=%v", err)
	}

	getMempoolTxsBySizeChan := make(chan rpcbus.Request, 1)
	if err := rpcBus.Register(topics.GetMempoolTxsBySize, getMempoolTxsBySizeChan); err != nil {
		log.Errorf("rpcbus.getMempoolTxsBySize err=%v", err)
	}

	sendTxChan := make(chan rpcbus.Request, 1)
	if err := rpcBus.Register(topics.SendMempoolTx, sendTxChan); err != nil {
		log.Errorf("rpcbus.SendMempoolTx err=%v", err)
	}

	intermediateBlockChan := initIntermediateBlockCollector(eventBus)
	acceptedBlockChan, _ := consensus.InitAcceptedBlockUpdate(eventBus)

	m := &Mempool{
		ctx:                     ctx,
		eventBus:                eventBus,
		latestBlockTimestamp:    math.MinInt32,
		quitChan:                make(chan struct{}),
		intermediateBlockChan:   intermediateBlockChan,
		acceptedBlockChan:       acceptedBlockChan,
		getMempoolTxsChan:       getMempoolTxsChan,
		getMempoolTxsBySizeChan: getMempoolTxsBySizeChan,
		sendTxChan:              sendTxChan,
		verifier:                verifier,
	}

	// Setting the pool where to cache verified transactions.
	// The pool is normally a Hashmap
	m.verified = m.newPool()

	log.Infof("Running with pool type %s", config.Get().Mempool.PoolType)

	// topics.Tx will be published by RPC subsystem or Peer subsystem (deserialized from gossip msg)
	m.pending = make(chan TxDesc, maxPendingLen)
	l := eventbus.NewCallbackListener(m.CollectPending)
	m.txSubscriberID = m.eventBus.Subscribe(topics.Tx, l)

	if srv != nil {
		node.RegisterMempoolServer(srv, m)
	}
	return m
}

// Run spawns the mempool lifecycle routine. The whole mempool cycle is around
// getting input from the outside world (from input channels) and provide the
// actual list of the verified txs (onto output channel).
//
// All operations are always executed in a single go-routine so no
// protection-by-mutex needed
func (m *Mempool) Run() {
	go func() {
		for {
			select {
			//rpcbus methods
			case r := <-m.sendTxChan:
				handleRequest(r, m.processSendMempoolTxRequest, "SendTx")
			case r := <-m.getMempoolTxsChan:
				handleRequest(r, m.processGetMempoolTxsRequest, "GetMempoolTxs")
			case r := <-m.getMempoolTxsBySizeChan:
				handleRequest(r, m.processGetMempoolTxsBySizeRequest, "GetMempoolTxsBySize")
			// Mempool input channels
			case b := <-m.intermediateBlockChan:
				m.onBlock(b)
			case b := <-m.acceptedBlockChan:
				m.onBlock(b)
			case tx := <-m.pending:
				// TODO: the m.pending channel looks a bit wasteful. Consider
				// removing it and call onPendingTx directly within
				// CollectPending
				_, _ = m.onPendingTx(tx)
			case <-time.After(20 * time.Second):
				m.onIdle()
			// Mempool terminating
			case <-m.quitChan:
				//m.eventBus.Unsubscribe(topics.Tx, m.txSubscriberID)
				return
			}
		}
	}()
}

// onPendingTx handles a submitted tx from any source (rpcBus or eventBus)
func (m *Mempool) onPendingTx(t TxDesc) ([]byte, error) {

	log.Infof("Pending txs=%d", len(m.pending))

	start := time.Now()
	txid, err := m.processTx(t)
	elapsed := time.Since(start)

	if err != nil {
		log.Errorf("Failed txid=%s err='%v' duration=%d μs", toHex(txid), err, elapsed.Microseconds())
	} else {
		log.Infof("Verified txid=%s duration=%d μs", toHex(txid), elapsed.Microseconds())
	}

	return txid, err
}

// processTx ensures all transaction rules are satisfied before adding the tx
// into the verified pool
func (m *Mempool) processTx(t TxDesc) ([]byte, error) {
	txid, err := t.tx.CalculateHash()
	if err != nil {
		return txid, fmt.Errorf("hash err: %s", err.Error())
	}

	log.Infof("Pending txid=%s size=%d bytes", toHex(txid), t.size)

	if t.tx.Type() == transactions.Distribute {
		// coinbase tx should be built by block generator only
		return txid, ErrCoinbaseTxNotAllowed
	}

	// expect it is not already a verified tx
	if m.verified.Contains(txid) {
		return txid, ErrAlreadyExists
	}

	// execute tx verification procedure
	if err := m.checkTx(t.tx); err != nil {
		return txid, fmt.Errorf("verification: %v", err)
	}

	// if consumer's verification passes, mark it as verified
	t.verified = time.Now()

	// we've got a valid transaction pushed
	if err := m.verified.Put(t); err != nil {
		return txid, fmt.Errorf("store: %v", err)
	}

	// advertise the hash of the verified tx to the P2P network
	if err := m.advertiseTx(txid); err != nil {
		// TODO: Perform re-advertise procedure
		return txid, fmt.Errorf("advertise: %v", err)
	}

	return txid, nil
}

func (m *Mempool) onBlock(b block.Block) {
	m.latestBlockTimestamp = b.Header.Timestamp
	m.removeAccepted(b)
}

// removeAccepted to clean up all txs from the mempool that have been already
// added to the chain.
//
// Instead of doing a full DB scan, here we rely on the latest accepted block to
// update.
//
// The passed block is supposed to be the last one accepted. That said, it must
// contain a valid TxRoot.
func (m *Mempool) removeAccepted(b block.Block) {

	blockHash := toHex(b.Header.Hash)

	log.Infof("Processing block %s with %d txs", blockHash, len(b.Txs))

	if m.verified.Len() == 0 {
		// No txs accepted then no cleanup needed
		return
	}

	payloads := make([]merkletree.Payload, len(b.Txs))
	for i, tx := range b.Txs {
		payloads[i] = tx.(merkletree.Payload)
	}

	tree, err := merkletree.NewTree(payloads)

	if err == nil && tree != nil {

		s := m.newPool()
		// Check if mempool verified tx is part of merkle tree of this block
		// if not, then keep it in the mempool for the next block
		err = m.verified.Range(func(k txHash, t TxDesc) error {
			if r, _ := tree.VerifyContent(t.tx); !r {
				if e := s.Put(t); e != nil {
					return e
				}
			}
			return nil
		})

		if err != nil {
			log.Error(err.Error())
		}

		m.verified = s
	}

	log.Infof("Processing block %s completed", toHex(b.Header.Hash))
}

func (m *Mempool) onIdle() {

	// stats to log
	poolSize := float32(m.verified.Size()) / 1000
	log.Infof("Txs count %d, total size %.3f kB", m.verified.Len(), poolSize)

	// trigger alarms/notifications in case of abnormal state

	// trigger alarms on too much txs memory allocated
	maxSizeBytes := config.Get().Mempool.MaxSizeMB * 1000 * 1000
	if m.verified.Size() > maxSizeBytes {
		log.Warnf("Mempool is bigger than %d MB", config.Get().Mempool.MaxSizeMB)
	}

	if log.Logger.Level == logger.TraceLevel {
		if m.verified.Len() > 0 {
			_ = m.verified.Range(func(k txHash, t TxDesc) error {
				log.Tracef("txid=%s", toHex(k[:]))
				return nil
			})
		}
	}

	// TODO: Get rid of stuck/expired transactions

	// TODO: Check periodically the oldest txs if somehow were accepted into the
	// blockchain but were not removed from mempool verified list.
	/*()
	err = c.db.View(func(t database.Transaction) error {
		_, _, _, err := t.FetchBlockTxByHash(txID)
		return err
	})
	*/
}

func (m *Mempool) newPool() Pool {

	preallocTxs := config.Get().Mempool.PreallocTxs

	var p Pool
	switch config.Get().Mempool.PoolType {
	case "hashmap":
		p = &HashMap{Capacity: preallocTxs}
	case "syncpool":
		log.Panic("syncpool not supported")
	default:
		p = &HashMap{Capacity: preallocTxs}
	}

	return p
}

// CollectPending process the emitted transactions.
// Fast-processing and simple impl to avoid locking here.
// NB This is always run in a different than main mempool routine
func (m *Mempool) CollectPending(msg message.Message) error {
	tx := msg.Payload().(transactions.ContractCall)
	m.pending <- TxDesc{tx: tx, received: time.Now(), size: uint(len(msg.Id()))}
	return nil
}

// processGetMempoolTxsRequest retrieves current state of the mempool of the verified but
// still unaccepted txs.
// Called by P2P on InvTypeMempoolTx msg
func (m Mempool) processGetMempoolTxsRequest(r rpcbus.Request) (interface{}, error) {
	// Read inputs
	params := r.Params.(bytes.Buffer)
	filterTxID := params.Bytes()

	outputTxs := make([]transactions.ContractCall, 0)

	// If we are looking for a specific tx, just look it up by key.
	if len(filterTxID) == 32 {
		tx := m.verified.Get(filterTxID)
		if tx == nil {
			return outputTxs, nil
		}

		outputTxs = append(outputTxs, tx)
		return outputTxs, nil
	}

	// When filterTxID is empty, mempool returns all verified txs sorted
	// by fee from highest to lowest
	err := m.verified.RangeSort(func(k txHash, t TxDesc) (bool, error) {
		outputTxs = append(outputTxs, t.tx)
		return false, nil
	})

	if err != nil {
		return nil, err
	}

	return outputTxs, err
}

// uType translates the node.TxType into transactions.TxType
func uType(t node.TxType) (transactions.TxType, error) {
	switch t {
	case node.TxType_STANDARD:
		return transactions.Tx, nil
	case node.TxType_DISTRIBUTE:
		return transactions.Distribute, nil
	case node.TxType_BID:
		return transactions.Bid, nil
	case node.TxType_STAKE:
		return transactions.Stake, nil
	case node.TxType_WITHDRAWFEES:
		return transactions.WithdrawFees, nil
	case node.TxType_WITHDRAWSTAKE:
		return transactions.WithdrawStake, nil
	case node.TxType_WITHDRAWBID:
		return transactions.WithdrawBid, nil
	case node.TxType_SLASH:
		return transactions.Slash, nil
	default:
		return transactions.Tx, errors.New("unknown transaction type")
	}
}

// SelectTx will return a view of the mempool, with optional filters applied.
func (m Mempool) SelectTx(ctx context.Context, req *node.SelectRequest) (*node.SelectResponse, error) {
	txs := make([]transactions.ContractCall, 0)
	switch {
	case len(req.Id) == 64:
		// If we want a tx with a certain ID, we can simply look it up
		// directly
		hash, err := hex.DecodeString(req.Id)
		if err != nil {
			return nil, err
		}

		tx := m.verified.Get(hash)
		if tx == nil {
			return nil, errors.New("tx not found")
		}

		txs = append(txs, tx)
	case len(req.Types) > 0:
		for _, t := range req.Types {
			trType, err := uType(t)
			if err != nil {
				// most likely an unsupported type. We just ignore it
				continue
			}
			txs = append(txs, m.verified.FilterByType(trType)...)
		}
	default:
		txs = m.verified.Clone()
	}

	resp := &node.SelectResponse{Result: make([]*node.Tx, len(txs))}
	for i, tx := range txs {
		txid, err := tx.CalculateHash()
		if err != nil {
			return nil, err
		}

		resp.Result[i] = &node.Tx{
			Type: node.TxType(tx.Type()),
			Id:   hex.EncodeToString(txid),
			// LockTime: tx.LockTime(),
		}
	}

	return resp, nil
}

// GetUnconfirmedBalance will return the amount of DUSK that is in the mempool
// for a given key.
// TODO: implement
func (m Mempool) GetUnconfirmedBalance(ctx context.Context, req *node.EmptyRequest) (*node.BalanceResponse, error) {
	return nil, nil
}

// processGetMempoolTxsBySizeRequest returns a subset of verified mempool txs which
// 1. contains only highest fee txs
// 2. has total txs size not bigger than maxTxsSize (request param)
// Called by BlockGenerator on generating a new candidate block
func (m Mempool) processGetMempoolTxsBySizeRequest(r rpcbus.Request) (interface{}, error) {

	// Read maxTxsSize param
	var maxTxsSize uint32
	params := r.Params.(bytes.Buffer)
	if err := encoding.ReadUint32LE(&params, &maxTxsSize); err != nil {
		return bytes.Buffer{}, err
	}

	txs := make([]transactions.ContractCall, 0)

	var totalSize uint32
	err := m.verified.RangeSort(func(k txHash, t TxDesc) (bool, error) {

		var done bool
		totalSize += uint32(t.size)

		if totalSize <= maxTxsSize {
			txs = append(txs, t.tx)
		} else {
			done = true
		}

		return done, nil
	})

	if err != nil {
		return bytes.Buffer{}, err
	}

	return txs, err
}

// processSendMempoolTxRequest utilizes rpcbus to allow submitting a tx to mempool with
func (m Mempool) processSendMempoolTxRequest(r rpcbus.Request) (interface{}, error) {
	tx := r.Params.(transactions.ContractCall)
	buf := new(bytes.Buffer)
	if err := transactions.Marshal(buf, tx); err != nil {
		return nil, err
	}

	txDesc := TxDesc{tx: tx, received: time.Now(), size: uint(buf.Len())}

	// Process request
	return m.onPendingTx(txDesc)
}

// Quit makes mempool main loop to terminate
func (m *Mempool) Quit() {
	m.quitChan <- struct{}{}
}

// Send Inventory message to all peers
//nolint:unparam
func (m *Mempool) advertiseTx(txID []byte) error {

	msg := &peermsg.Inv{}
	msg.AddItem(peermsg.InvTypeMempoolTx, txID)

	// TODO: can we simply encode the message directly on a topic carrying buffer?
	buf := new(bytes.Buffer)
	if err := msg.Encode(buf); err != nil {
		log.Panic(err)
	}

	if err := topics.Prepend(buf, topics.Inv); err != nil {
		log.Panic(err)
	}

	// TODO: interface - marshaling should done after the Gossip, not before
	packet := message.New(topics.Inv, *buf)
	m.eventBus.Publish(topics.Gossip, packet)
	return nil
}

func toHex(id []byte) string {
	enc := hex.EncodeToString(id[:])
	return enc
}

// TODO: handlers should just return []transactions.ContractCall, and the
// caller should be left to format the data however they wish
func handleRequest(r rpcbus.Request, handler func(r rpcbus.Request) (interface{}, error), name string) {

	log.Tracef("Handling %s request", name)

	result, err := handler(r)
	if err != nil {
		log.Errorf("Failed %s request: %v", name, err)
		r.RespChan <- rpcbus.Response{Err: err}
		return
	}

	r.RespChan <- rpcbus.Response{Resp: result, Err: nil}

	log.Tracef("Handled %s request", name)
}
