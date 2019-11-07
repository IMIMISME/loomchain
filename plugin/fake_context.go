// +build evm

package plugin

import (
	"context"
	"time"

	loom "github.com/loomnetwork/go-loom"
	"github.com/loomnetwork/go-loom/plugin"
	"github.com/loomnetwork/go-loom/types"
	"github.com/loomnetwork/loomchain"
	cdb "github.com/loomnetwork/loomchain/db"
	levm "github.com/loomnetwork/loomchain/evm"
	"github.com/loomnetwork/loomchain/store"
	abci "github.com/tendermint/tendermint/abci/types"
)

// Contract context for tests that need both Go & EVM contracts.
type FakeContextWithEVM struct {
	*plugin.FakeContext
	State                    loomchain.State
	EvmStore                 *store.EvmStore
	useAccountBalanceManager bool
}

func CreateFakeContextWithEVM(caller, address loom.Address) *FakeContextWithEVM {
	block := abci.Header{
		ChainID: "chain",
		Height:  int64(34),
		Time:    time.Unix(123456789, 0),
	}
	ctx := plugin.CreateFakeContext(caller, address).WithBlock(
		types.BlockHeader{
			ChainID: block.ChainID,
			Height:  block.Height,
			Time:    block.Time.Unix(),
		},
	)
	state := loomchain.NewStoreState(context.Background(), ctx, block, nil, nil)
	evmDB, err := cdb.LoadDB("memdb", "", "", 256, 4, false)
	if err != nil {
		panic(err)
	}
	evmStore := store.NewEvmStore(evmDB, 100, 0)
	return &FakeContextWithEVM{
		FakeContext: ctx,
		State:       state,
		EvmStore:    evmStore,
	}
}

func (c *FakeContextWithEVM) WithValidators(validators []*types.Validator) *FakeContextWithEVM {
	return &FakeContextWithEVM{
		FakeContext:              c.FakeContext.WithValidators(validators),
		State:                    c.State,
		useAccountBalanceManager: c.useAccountBalanceManager,
		EvmStore:                 c.EvmStore,
	}
}

func (c *FakeContextWithEVM) WithBlock(header loom.BlockHeader) *FakeContextWithEVM {
	return &FakeContextWithEVM{
		FakeContext:              c.FakeContext.WithBlock(header),
		State:                    c.State,
		useAccountBalanceManager: c.useAccountBalanceManager,
		EvmStore:                 c.EvmStore,
	}
}

func (c *FakeContextWithEVM) WithSender(caller loom.Address) *FakeContextWithEVM {
	return &FakeContextWithEVM{
		FakeContext:              c.FakeContext.WithSender(caller),
		State:                    c.State,
		useAccountBalanceManager: c.useAccountBalanceManager,
		EvmStore:                 c.EvmStore,
	}
}

func (c *FakeContextWithEVM) WithAddress(addr loom.Address) *FakeContextWithEVM {
	return &FakeContextWithEVM{
		FakeContext:              c.FakeContext.WithAddress(addr),
		State:                    c.State,
		useAccountBalanceManager: c.useAccountBalanceManager,
		EvmStore:                 c.EvmStore,
	}
}

func (c *FakeContextWithEVM) WithFeature(name string, value bool) *FakeContextWithEVM {
	c.State.SetFeature(name, value)
	return &FakeContextWithEVM{
		FakeContext:              c.FakeContext,
		State:                    c.State,
		useAccountBalanceManager: c.useAccountBalanceManager,
		EvmStore:                 c.EvmStore,
	}
}

func (c *FakeContextWithEVM) WithAccountBalanceManager(enable bool) *FakeContextWithEVM {
	return &FakeContextWithEVM{
		FakeContext:              c.FakeContext,
		State:                    c.State,
		useAccountBalanceManager: enable,
		EvmStore:                 c.EvmStore,
	}
}

func (c *FakeContextWithEVM) AccountBalanceManager(readOnly bool) levm.AccountBalanceManager {
	ethCoinAddr, err := c.Resolve("ethcoin")
	if err != nil {
		panic(err)
	}
	return NewAccountBalanceManager(c.WithAddress(ethCoinAddr))
}

func (c *FakeContextWithEVM) CallEVM(addr loom.Address, input []byte, value *loom.BigUInt) ([]byte, error) {
	var createABM levm.AccountBalanceManagerFactoryFunc
	if c.useAccountBalanceManager {
		createABM = c.AccountBalanceManager
	}
	vm := levm.NewLoomVm(c.State, nil, nil, createABM, false)
	return vm.Call(c.ContractAddress(), addr, input, value)
}

func (c *FakeContextWithEVM) StaticCallEVM(addr loom.Address, input []byte) ([]byte, error) {
	var createABM levm.AccountBalanceManagerFactoryFunc
	if c.useAccountBalanceManager {
		createABM = c.AccountBalanceManager
	}
	vm := levm.NewLoomVm(c.State, nil, nil, createABM, false)
	return vm.StaticCall(c.ContractAddress(), addr, input)
}

func (c *FakeContextWithEVM) FeatureEnabled(name string, value bool) bool {
	return c.State.FeatureEnabled(name, value)
}

func (c *FakeContextWithEVM) EnabledFeatures() []string {
	return nil
}
