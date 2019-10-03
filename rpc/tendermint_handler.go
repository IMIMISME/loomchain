package rpc

import (
	"encoding/json"

	etypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/gogo/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/loomnetwork/go-loom"
	ltypes "github.com/loomnetwork/go-loom/types"
	"github.com/loomnetwork/loomchain/auth"
	"github.com/loomnetwork/loomchain/evm/utils"
	"github.com/loomnetwork/loomchain/rpc/eth"
	"github.com/loomnetwork/loomchain/vm"
	ctypes "github.com/tendermint/tendermint/rpc/core/types"
	"github.com/tendermint/tendermint/types"
)

const ethTxID = 4

type TendermintPRCFunc struct {
	eth.HttpRPCFunc
	chainID     string
	broadcastTx func(tx types.Tx) (*ctypes.ResultBroadcastTx, error)
}

// Tendermint handlers need parameters translated.
// Only one method supported.
func NewTendermintRPCFunc(chainID string, broadcastTx func(tx types.Tx) (*ctypes.ResultBroadcastTx, error)) eth.RPCFunc {
	return &TendermintPRCFunc{
		chainID:     chainID,
		broadcastTx: broadcastTx,
	}
}

func (t *TendermintPRCFunc) UnmarshalParamsAndCall(
	input eth.JsonRpcRequest, conn *websocket.Conn,
) (json.RawMessage, *eth.Error) {
	if len(input.Params) == 0 {
		return nil, eth.NewError(eth.EcInvalidParams, "Parse params", "expected one or more parameters")
	}

	paramsBytes := []json.RawMessage{}
	if err := json.Unmarshal(input.Params, &paramsBytes); err != nil {
		return nil, eth.NewError(eth.EcParseError, "Parse params", err.Error())
	}

	var data string
	if err := json.Unmarshal(paramsBytes[0], &data); err != nil {
		return nil, eth.NewErrorf(eth.EcParseError, "Parse params", "failed to unmarshal input: %v", err)
	}

	txBytes, err := eth.DecDataToBytes(eth.Data(data))
	if err != nil {
		return nil, eth.NewErrorf(
			eth.EcParseError, "Parse params", "failed to convert input %v to bytes: %v", data, err,
		)
	}

	tmTx, err := ethereumToTendermintTx(t.chainID, txBytes)
	if err != nil {
		return nil, eth.NewErrorf(
			eth.EcServer, "Parse parameters", "convert ethereum tx to tendermint tx, error %v", err,
		)
	}

	r, err := t.broadcastTx(tmTx)
	if err != nil {
		return nil, eth.NewErrorf(eth.EcServer, "Server error", "transaction returned error %v", err)
	}
	if r == nil {
		return nil, eth.NewError(eth.EcServer, "Server error", "transaction returned nil result")
	}

	var result json.RawMessage
	result, err = json.Marshal(eth.EncBytes(r.Hash))
	if err != nil {
		return nil, eth.NewErrorf(eth.EcServer, "Server error", "failed to marshal tx hash: %v", err)
	}
	return result, nil
}

// Wraps a raw Ethereum tx in a Loom SignedTx
func ethereumToTendermintTx(chainID string, txBytes []byte) (types.Tx, error) {
	msg := &vm.MessageTx{}
	msg.Data = txBytes
	var tx etypes.Transaction
	if err := rlp.DecodeBytes(txBytes, &tx); err != nil {
		return nil, err
	}

	if tx.To() != nil {
		msg.To = loom.Address{
			ChainID: chainID,
			Local:   tx.To().Bytes(),
		}.MarshalPB()
	}

	chainConfig := utils.DefaultChainConfig(true)
	ethSigner := etypes.MakeSigner(&chainConfig, chainConfig.EIP155Block)
	ethFrom, err := etypes.Sender(ethSigner, &tx)
	if err != nil {
		return nil, err
	}
	msg.From = loom.Address{
		ChainID: "eth",
		Local:   ethFrom.Bytes(),
	}.MarshalPB()

	txTx := &ltypes.Transaction{
		Id: ethTxID,
	}
	txTx.Data, err = proto.Marshal(msg)
	if err != nil {
		return nil, err
	}

	nonceTx := &auth.NonceTx{
		Sequence: tx.Nonce(),
	}
	nonceTx.Inner, err = proto.Marshal(txTx)
	if err != nil {
		return nil, err
	}

	signedTx := &auth.SignedTx{}
	signedTx.Inner, err = proto.Marshal(nonceTx)
	if err != nil {
		return nil, err
	}

	return proto.Marshal(signedTx)
}
