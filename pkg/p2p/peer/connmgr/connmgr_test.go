package connmgr_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gitlab.dusk.network/dusk-core/dusk-go/pkg/p2p/peer/connmgr"
)

func TestDial(t *testing.T) {
	cfg := connmgr.Config{
		GetAddress:   nil,
		OnConnection: nil,
		OnAccept:     nil,
		Port:         "",
		DialTimeout:  0,
	}

	cm := connmgr.New(cfg)
	cm.Run()

	ipport := "google.com:80" // google unlikely to go offline, a better approach to test Dialing is welcome.

	conn, err := cm.Dial(ipport)
	assert.Equal(t, nil, err)
	assert.NotEqual(t, nil, conn)
}
func TestConnect(t *testing.T) {
	cfg := connmgr.Config{
		GetAddress:   nil,
		OnConnection: nil,
		OnAccept:     nil,
		Port:         "",
		DialTimeout:  0,
	}

	cm := connmgr.New(cfg)
	cm.Run()

	ipport := "google.com:80"

	r := connmgr.Request{Addr: ipport}

	cm.Connect(&r)

	assert.Equal(t, 1, len(cm.ConnectedList))

}
func TestNewRequest(t *testing.T) {

	address := "google.com:80"

	var getAddr = func() (string, error) {
		return address, nil
	}

	cfg := connmgr.Config{
		GetAddress:   getAddr,
		OnConnection: nil,
		OnAccept:     nil,
		Port:         "",
		DialTimeout:  0,
	}

	cm := connmgr.New(cfg)

	cm.Run()

	cm.NewRequest()

	if _, ok := cm.ConnectedList[address]; ok {
		assert.Equal(t, true, ok)
		assert.Equal(t, 1, len(cm.ConnectedList))
		return
	}

	assert.Fail(t, "Could not find the address in the connected lists")

}
func TestDisconnect(t *testing.T) {

	address := "google.com:80"

	var getAddr = func() (string, error) {
		return address, nil
	}

	cfg := connmgr.Config{
		GetAddress:   getAddr,
		OnConnection: nil,
		OnAccept:     nil,
		Port:         "",
		DialTimeout:  0,
	}

	cm := connmgr.New(cfg)

	cm.Run()

	cm.NewRequest()

	cm.Disconnect(address)

	assert.Equal(t, 0, len(cm.ConnectedList))

}
