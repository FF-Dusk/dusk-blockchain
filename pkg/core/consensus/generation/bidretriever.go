package generation

import (
	"bytes"
	"errors"

	"github.com/dusk-network/dusk-blockchain/pkg/core/block"
	"github.com/dusk-network/dusk-blockchain/pkg/core/database"
	"github.com/dusk-network/dusk-blockchain/pkg/core/database/heavy"
	"github.com/dusk-network/dusk-blockchain/pkg/wallet/transactions"
	log "github.com/sirupsen/logrus"
)

// BidRetriever is a simple searcher, who's responsibility is to find a bid transaction, when given
// an M value.
type BidRetriever struct {
	db database.DB
}

var NoBidFound = errors.New("could not find corresponding value for specified item")

// NewBidRetriever returns an initialized BidRetriever, ready for use.
func NewBidRetriever(db database.DB) *BidRetriever {
	// Get a db connection, if none was given.
	if db == nil {
		_, db = heavy.CreateDBConnection()
	}

	return &BidRetriever{
		db: db,
	}
}

// SearchForBid will walk up the blockchain to find a bid with the corresponding `m`,
// by running a comparison function on each tx for every found block. If the comparison function finds a match,
// then this tx is returned. This tx can then be used by the generation component to start or reset itself.
func (i *BidRetriever) SearchForBid(m []byte) (transactions.Transaction, uint64, error) {
	currentHeight := i.getCurrentHeight()
	searchingHeight := i.getSearchingHeight(currentHeight)

	for {
		blk, err := i.getBlock(searchingHeight)
		if err == database.ErrBlockNotFound {
			break
		}
		if err != nil {
			return nil, 0, err
		}

		tx, err := findCorrespondingBid(blk.Txs, m, searchingHeight, currentHeight)
		if err != nil {
			searchingHeight++
			continue
		}

		return tx, searchingHeight, nil
	}

	return nil, 0, NoBidFound
}

// If given a set of transactions and an M value, this function will return a bid
// transaction corresponding to that M value.
func findCorrespondingBid(txs []transactions.Transaction, m []byte, searchingHeight, currentHeight uint64) (transactions.Transaction, error) {
	for _, tx := range txs {
		bid, ok := tx.(*transactions.Bid)
		if !ok {
			continue
		}

		if bytes.Equal(m, bid.M) && bid.Lock+searchingHeight > currentHeight {
			hash, err := bid.CalculateHash()
			if err != nil {
				// If we found a valid bid tx, we should under no circumstance have issues marshalling it
				panic(err)
			}

			log.WithFields(log.Fields{
				"process": "generation",
				"height":  searchingHeight,
				"tx hash": hash,
			}).Debugln("bid found in chain")
			return bid, nil
		}
	}

	return nil, errors.New("could not find a corresponding d value")
}

// Bid transactions can only be valid for the maximum locktime. So, we will begin our search at
// the tip height, minus the maximum locktime. This function will tell us exactly what that height is.
func (i *BidRetriever) getSearchingHeight(currentHeight uint64) uint64 {
	if currentHeight < transactions.MaxLockTime {
		return 0
	}
	return currentHeight - transactions.MaxLockTime
}

func (i *BidRetriever) getBlock(searchingHeight uint64) (*block.Block, error) {
	var b *block.Block
	err := i.db.View(func(t database.Transaction) error {
		hash, err := t.FetchBlockHashByHeight(searchingHeight)
		if err != nil {
			return err
		}

		b, err = t.FetchBlock(hash)
		return err
	})

	return b, err
}

func (i *BidRetriever) getCurrentHeight() (currentHeight uint64) {
	err := i.db.View(func(t database.Transaction) error {
		var err error
		currentHeight, err = t.FetchCurrentHeight()
		return err
	})

	if err != nil {
		// If we can not get the current height, it means we have no DB state object, or the header of the tip hash is missing
		// Neither should ever happen, so we panic
		panic(err)
	}

	return
}
