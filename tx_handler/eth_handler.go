// +build evm

package tx_handler

import (
	"fmt"

	etypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/golang/protobuf/proto"
	"github.com/loomnetwork/go-loom"
	"github.com/loomnetwork/go-loom/common"
	"github.com/loomnetwork/go-loom/types"
	"github.com/loomnetwork/loomchain"
	"github.com/loomnetwork/loomchain/auth"
	"github.com/loomnetwork/loomchain/eth/utils"
	"github.com/loomnetwork/loomchain/features"
	"github.com/loomnetwork/loomchain/registry/factory"
	"github.com/loomnetwork/loomchain/vm"
	"github.com/pkg/errors"
)

type EthTxHandler struct {
	*vm.Manager
	CreateRegistry factory.RegistryFactoryFunc
}

func (h *EthTxHandler) ProcessTx(
	state loomchain.State,
	txBytes []byte,
	isCheckTx bool,
) (loomchain.TxHandlerResult, error) {
	var r loomchain.TxHandlerResult

	if !state.FeatureEnabled(features.EthTxFeature, false) {
		return r, errors.New("ethereum transactions feature not enabled")
	}

	var msg vm.MessageTx
	err := proto.Unmarshal(txBytes, &msg)
	if err != nil {
		return r, err
	}

	origin := auth.Origin(state.Context())
	caller := loom.UnmarshalAddressPB(msg.From)

	if caller.Compare(origin) != 0 {
		return r, fmt.Errorf("Origin doesn't match caller: - %v != %v", origin, caller)
	}

	vmInstance, err := h.Manager.InitVM(vm.VMType_EVM, state)
	if err != nil {
		return r, err
	}

	var ethTx etypes.Transaction
	if err := rlp.DecodeBytes(msg.Data, &ethTx); err != nil {
		return r, err
	}

	value := &loom.BigUInt{Int: ethTx.Value()}
	if !common.IsPositive(*value) && !common.IsZero(*value) {
		return r, errors.Errorf("value %v must be non negative", value)
	}
	if ethTx.To() == nil {
		retCreate, addr, errCreate := vmInstance.Create(origin, ethTx.Data(), value)

		response, errMarshal := proto.Marshal(&vm.DeployResponse{
			Contract: &types.Address{
				ChainId: addr.ChainID,
				Local:   addr.Local,
			},
			Output: retCreate,
		})
		if errMarshal != nil {
			if errCreate != nil {
				return r, errors.Wrapf(errCreate, "[DeployTxHandler] Error deploying contract on create")
			} else {
				return r, errors.Wrapf(errMarshal, "[DeployTxHandler] Error deploying contract on marshaling error")
			}
		}
		r.Data = append(r.Data, response...)
		if errCreate != nil {
			return r, errors.Wrapf(errCreate, "[DeployTxHandler] Error deploying contract on create")
		}

		reg := h.CreateRegistry(state)
		_ = reg.Register("", addr, caller)

		r.Info = utils.DeployEvm
		return r, nil
	} else {
		to := loom.UnmarshalAddressPB(msg.To)
		r.Data, err = vmInstance.Call(origin, to, ethTx.Data(), value)
		if err != nil {
			return r, err
		}
		r.Info = utils.CallEVM
		return r, err
	}
}
