// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT License was not distributed with this
// file, you can obtain one at https://opensource.org/licenses/MIT.
//
// Copyright (c) DUSK NETWORK. All rights reserved.

package dupemap_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/dusk-network/dusk-blockchain/pkg/p2p/peer/dupemap"
	"github.com/stretchr/testify/assert"
)

var dupeTests = []struct {
	height    uint64
	tolerance uint64
	canFwd    bool
}{
	{1, 3, true},
	{1, 3, false},
	{2, 3, false},
	{4, 3, true},
	{4, 3, false},
	{5, 3, false},
	{7, 1, true},
	{8, 1, false},
	{9, 1, true},
}

var dupeFilterTests = []struct {
	data   uint16
	canFwd bool
}{
	{1, true},
	{1, false},
	{2, true},
	{4, true},
	{4, false},
	{5, true},
	{7, true},
	{7, false},
	{7, false},
	{7, false},
	{9, true},
}

func TestDupeMap(t *testing.T) {
	dupeMap := dupemap.NewDupeMap(1, 100)

	test := bytes.NewBufferString("This is a test")

	for _, tt := range dupeTests {
		dupeMap.UpdateHeight(tt.height)
		dupeMap.SetTolerance(tt.tolerance)

		res := dupeMap.CanFwd(test)
		if !assert.Equal(t, tt.canFwd, res) {
			assert.FailNowf(t, "failure", "DupeMap.CanFwd: expected %t, got %t with height %d and tolerance %d", res, tt.canFwd, tt.height, tt.tolerance)
		}
	}

	t.Log("Size: ", dupeMap.Size())
}

func TestCanFwd(t *testing.T) {
	dupeMap := dupemap.NewDupeMap(1, 100)
	dupeMap.SetTolerance(10)

	for i, tt := range dupeFilterTests {
		test := make([]byte, 2)
		binary.BigEndian.PutUint16(test, tt.data)

		res := dupeMap.CanFwd(bytes.NewBuffer(test))
		if !assert.Equal(t, tt.canFwd, res) {
			assert.FailNowf(t, "failure", "DupeMap.CanFwd: expected %t, got %t, index %d", res, tt.canFwd, i)
		}
	}
}

func TestCanFwdBigData(t *testing.T) {
	type testu struct {
		payload *bytes.Buffer
		canFwd  bool
	}

	testData := make([]testu, 0)

	// Populate test data
	for i := uint32(0); i < 800*1000; i++ {
		d := make([]byte, 4)
		binary.BigEndian.PutUint32(d, i)
		payload := bytes.NewBuffer(d)
		testData = append(testData, testu{payload, false})
	}

	// Initialize a dupemap with 1M capacity per round-filter
	itemsCount := uint(1000 * 1000)
	dupeMap := dupemap.NewDupeMap(1, itemsCount)
	dupeMap.SetTolerance(10)

	falsePositiveCount := uint(0)

	for _, d := range testData {
		// underlying filter structure is a probabilistic data structure
		// That's said, Few false positive are possible.
		if !dupeMap.CanFwd(d.payload) {
			falsePositiveCount++
		}
	}

	// Ensure false positive rate is less than 1.3%
	falsePositiveRate := float64(100*falsePositiveCount) / float64(itemsCount)
	if falsePositiveRate > 1.3 {
		assert.Failf(t, "failure", "false positive are too many %f", falsePositiveRate)
	}

	// Now CanFwd should always returns false
	for _, d := range testData {
		// Ensure that the underlying filter structure supports "definitely
		// no" a.k.a no false negative
		if dupeMap.CanFwd(d.payload) != false {
			t.FailNow()
		}
	}

	// Ensure dupemap underlying structure does not consume more than 1MB for 1M capacity
	assert.LessOrEqual(t, dupeMap.Size(), 1024*1024)
}

func BenchmarkCanFwd(b *testing.B) {
	b.StopTimer()

	type testu struct {
		payload *bytes.Buffer
		canFwd  bool
	}

	testData := make([]testu, 0)

	for i := uint32(0); i < 900*1001; i++ {
		d := make([]byte, 4)
		binary.BigEndian.PutUint32(d, i)
		payload := bytes.NewBuffer(d)
		testData = append(testData, testu{payload, false})
	}

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		dupeMap := dupemap.NewDupeMap(1, 1000000)
		dupeMap.SetTolerance(10)

		b.StartTimer()

		// CanFwd always returns true
		for _, t := range testData {
			_ = dupeMap.CanFwd(t.payload)
		}

		for _, t := range testData {
			_ = dupeMap.CanFwd(t.payload)
		}
	}
}
