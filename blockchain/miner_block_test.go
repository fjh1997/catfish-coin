package blockchain

import (
	"testing"

	"github.com/deroproject/derohe/cryptography/crypto"
	"github.com/deroproject/graviton"
)

func TestAddressHashMatchesSerializedMiniBlockHash(t *testing.T) {
	key := []byte("catfish-miner-address-key")
	fullHash := graviton.Sum(key)

	var serializedHash crypto.Hash
	copy(serializedHash[:16], fullHash[:16])

	if !addressHashMatchesKey(key, fullHash) {
		t.Fatal("full address hash should match")
	}
	if !addressHashMatchesKey(key, serializedHash) {
		t.Fatal("miniblock serialized hash prefix should match")
	}
}
