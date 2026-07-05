package walletapi

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/deroproject/derohe/cryptography/bn256"
	"github.com/deroproject/derohe/cryptography/crypto"
	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/derohe/transaction"
)

func (w *Wallet_Memory) Show_Mempool_Transfers(scid crypto.Hash) (entries []rpc.Entry) {
	if !w.GetMode() || !IsDaemonOnline() {
		return nil
	}

	var pool rpc.GetTxPool_Result
	if err := rpc_client.Call("DERO.GetTxPool", nil, &pool); err != nil || len(pool.Tx_list) == 0 {
		return nil
	}

	var txResult rpc.GetTransaction_Result
	if err := rpc_client.Call("DERO.GetTransaction", rpc.GetTransaction_Params{Tx_Hashes: pool.Tx_list}, &txResult); err != nil {
		return nil
	}

	compressedAddress := w.account.Keys.Public.EncodeCompressed()
	for i, txHex := range txResult.Txs_as_hex {
		if txHex == "" {
			continue
		}
		related := rpc.Tx_Related_Info{}
		if i < len(txResult.Txs) {
			related = txResult.Txs[i]
		}
		if !related.In_pool || len(related.Ring) == 0 {
			continue
		}

		raw, err := hex.DecodeString(txHex)
		if err != nil {
			continue
		}
		var tx transaction.Transaction
		if err := tx.Deserialize(raw); err != nil || tx.TransactionType == transaction.REGISTRATION {
			continue
		}

		refTopo := int64(-1)
		if blid := crypto.Hash(tx.BLID); !blid.IsZero() {
			var header rpc.GetBlockHeaderByHash_Result
			if err := rpc_client.Call("DERO.GetBlockHeaderByHash", rpc.GetBlockHeaderByHash_Params{Hash: blid.String()}, &header); err == nil {
				refTopo = header.Block_Header.TopoHeight
			}
		}

		_, _, _, previousBalanceE, err := w.GetEncryptedBalanceAtTopoHeight(scid, refTopo, w.GetAddress().String())
		if err != nil {
			continue
		}
		previousBalanceETx := new(crypto.ElGamal).Deserialize(previousBalanceE.Serialize())
		previousBalance := w.DecodeEncryptedBalance_Memory(previousBalanceETx, 0)

		for t := range tx.Payloads {
			if tx.Payloads[t].SCID != scid || t >= len(related.Ring) {
				continue
			}
			if int(tx.Payloads[t].Statement.RingSize) != len(related.Ring[t]) {
				continue
			}
			tx.Payloads[t].Statement.Publickeylist = tx.Payloads[t].Statement.Publickeylist[:0]
			tx.Payloads[t].Statement.Publickeylist_compressed = tx.Payloads[t].Statement.Publickeylist_compressed[:0]
			for _, ringAddress := range related.Ring[t] {
				addr, err := rpc.NewAddress(ringAddress)
				if err != nil {
					continue
				}
				var compressed [33]byte
				copy(compressed[:], addr.PublicKey.EncodeCompressed())
				tx.Payloads[t].Statement.Publickeylist_compressed = append(tx.Payloads[t].Statement.Publickeylist_compressed, compressed)
				tx.Payloads[t].Statement.Publickeylist = append(tx.Payloads[t].Statement.Publickeylist, addr.PublicKey.G1())
			}
			if int(tx.Payloads[t].Statement.RingSize) != len(tx.Payloads[t].Statement.Publickeylist_compressed) {
				continue
			}

			for j := 0; j < int(tx.Payloads[t].Statement.RingSize); j++ {
				if bytes.Compare(compressedAddress, tx.Payloads[t].Statement.Publickeylist_compressed[j][:]) != 0 {
					continue
				}

				changes := crypto.ConstructElGamal(tx.Payloads[t].Statement.C[j], tx.Payloads[t].Statement.D)
				changedBalanceE := previousBalanceETx.Add(changes)
				changedBalance := w.DecodeEncryptedBalance_Memory(changedBalanceE, previousBalance)
				previousBalanceETx = new(crypto.ElGamal).Deserialize(changedBalanceE.Serialize())

				if previousBalance >= changedBalance {
					previousBalance = changedBalance
					continue
				}

				entry := rpc.Entry{
					Height:         0,
					TopoHeight:     -1,
					TransactionPos: -1,
					Pos:            t,
					Incoming:       true,
					TXID:           tx.GetHash().String(),
					Time:           time.Now(),
					Fees:           tx.Fees(),
					Amount:         changedBalance - previousBalance,
				}
				decodeIncomingPayload(w, &entry, &tx, t, j)
				entries = append(entries, entry)
				previousBalance = changedBalance
			}
		}
	}
	return entries
}

func decodeIncomingPayload(w *Wallet_Memory, entry *rpc.Entry, tx *transaction.Transaction, payloadIndex, ringIndex int) {
	entry.PayloadType = tx.Payloads[payloadIndex].RPCType
	if tx.Payloads[payloadIndex].RPCType != transaction.ENCRYPTED_DEFAULT_PAYLOAD_CBOR {
		entry.PayloadError = fmt.Sprintf("unknown payload type %d", tx.Payloads[payloadIndex].RPCType)
		entry.Payload = tx.Payloads[payloadIndex].RPCPayload
		return
	}

	var x bn256.G1
	x.ScalarMult(crypto.G, new(big.Int).SetInt64(0-int64(entry.Amount)))
	x.Add(new(bn256.G1).Set(&x), tx.Payloads[payloadIndex].Statement.C[ringIndex])
	blinder := &x

	sharedKey := crypto.GenerateSharedSecret(w.account.Keys.Secret.BigInt(), tx.Payloads[payloadIndex].Statement.D)
	proof := rpc.NewAddressFromKeys((*crypto.Point)(blinder))
	proof.Proof = true
	proof.Arguments = rpc.Arguments{{Name: "H", DataType: rpc.DataHash, Value: crypto.Hash(sharedKey)}, {Name: rpc.RPC_VALUE_TRANSFER, DataType: rpc.DataUint64, Value: uint64(entry.Amount)}}
	entry.Proof = proof.String()

	payload := append([]byte{}, tx.Payloads[payloadIndex].RPCPayload...)
	crypto.EncryptDecryptUserData(crypto.Keccak256(sharedKey[:], w.GetAddress().PublicKey.EncodeCompressed()), payload)
	if len(payload) == 0 {
		return
	}
	senderIdx := uint(payload[0])
	if uint(tx.Payloads[payloadIndex].Statement.RingSize) == 2 {
		senderIdx = 0
		if ringIndex == 0 {
			senderIdx = 1
		}
	}
	if senderIdx < uint(tx.Payloads[payloadIndex].Statement.RingSize) {
		addr := rpc.NewAddressFromKeys((*crypto.Point)(tx.Payloads[payloadIndex].Statement.Publickeylist[senderIdx]))
		addr.Mainnet = w.GetNetwork()
		entry.Sender = addr.String()
	}
	entry.Payload = append(entry.Payload, payload[1:]...)
	entry.Data = append(entry.Data, payload...)
	_, _ = entry.ProcessPayload()
}
