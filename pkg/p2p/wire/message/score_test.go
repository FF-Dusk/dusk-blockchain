// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT License was not distributed with this
// file, you can obtain one at https://opensource.org/licenses/MIT.
//
// Copyright (c) DUSK NETWORK. All rights reserved.

package message_test

import (
	"bytes"
	"testing"

	"github.com/dusk-network/dusk-blockchain/pkg/config"
	"github.com/dusk-network/dusk-blockchain/pkg/core/consensus/header"
	"github.com/dusk-network/dusk-blockchain/pkg/p2p/wire/message"
	crypto "github.com/dusk-network/dusk-crypto/hash"
	"github.com/stretchr/testify/assert"
)

// Test both the Marshal and Unmarshal functions, making sure the data is correctly
// stored to/retrieved from a Buffer.
func TestUnMarshal(t *testing.T) {
	hash, _ := crypto.RandEntropy(32)
	hdr := header.Mock()
	hdr.BlockHash = hash

	// mock candidate
	genesis := config.DecodeGenesis()
	se := message.MockScore(hdr, *genesis)

	buf := new(bytes.Buffer)
	assert.NoError(t, message.MarshalScore(buf, se))

	other := &message.Score{}
	assert.NoError(t, message.UnmarshalScore(buf, other))
	assert.True(t, se.Equal(*other))
}

func TestDeepCopy(t *testing.T) {
	hash, _ := crypto.RandEntropy(32)
	hdr := header.Mock()
	hdr.BlockHash = hash

	// mock candidate
	genesis := config.DecodeGenesis()
	se := message.MockScore(hdr, *genesis)

	buf := new(bytes.Buffer)
	assert.NoError(t, message.MarshalScore(buf, se))

	deepCopy := se.Copy().(message.Score)
	buf2 := new(bytes.Buffer)
	assert.NoError(t, message.MarshalScore(buf2, deepCopy))

	assert.Equal(t, buf.Bytes(), buf2.Bytes())
}
