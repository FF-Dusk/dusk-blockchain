package kadcast

import "golang.org/x/crypto/sha3"

// Peer stores the info of a peer which consists on:
// - IP of the peer.
// - Port to connect to it.
// - The ID of the peer.
type Peer struct {
	ip   [4]byte
	port uint16
	id   [16]byte
}

// Constructs a Peer with it's fields values as inputs.
func makePeer(ip *[4]byte, port *uint16, id *[16]byte) Peer {
	peer := Peer{*ip, *port, *id}
	return peer
}

// The function receives the user's `Peer` and computes the
// ID nonce in order to be able to join the network.
//
// This operation is basically a PoW algorithm that ensures
// that Sybil attacks are more costly.
func getMyNonce(myPeer *Peer) uint32 {
	var nonce uint32 = 0
	var hash [32]byte
	id := myPeer.id
	for {
		hash = sha3.Sum256(append(id[:], getBytesFromUint(nonce)[:]...))
		if (hash[31] | hash[30] | hash[29]) == 0 {
			return nonce
		}
		nonce++
	}
}

// Sets the Id sent as parameter as the Peer ID.
func (peer *Peer) addIP(ip *[4]byte) {
	peer.ip = *ip
}

// Sets the Id sent as parameter as the Peer ID.
func (peer *Peer) addID(id *[16]byte) {
	peer.id = *id
}

// Sets the port sent as parameter as the Peer port.
func (peer *Peer) addPort(port *uint16) {
	peer.port = *port
}

// Computes the XOR distance between two Peers.
func (me *Peer) computePeerDistance(peer *Peer) uint16 {
	return idXor(&me.id, &peer.id)
}
