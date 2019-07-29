package utils

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

func DefaultChainConfig(enableConstantinople bool) params.ChainConfig {
	cliqueCfg := params.CliqueConfig{
		Period: 10,   // Number of seconds between blocks to enforce
		Epoch:  1000, // Epoch length to reset votes and checkpoint
	}

	var constantinopleBlock *big.Int
	if enableConstantinople {
		constantinopleBlock = big.NewInt(0)
	}

	return params.ChainConfig{
		ChainID:        big.NewInt(0), // Chain id identifies the current chain and is used for replay protection
		HomesteadBlock: nil,           // Homestead switch block (nil = no fork, 0 = already homestead)
		DAOForkBlock:   nil,           // TheDAO hard-fork switch block (nil = no fork)
		DAOForkSupport: true,          // Whether the nodes supports or opposes the DAO hard-fork
		// EIP150 implements the Gas price changes (https://github.com/ethereum/EIPs/issues/150)
		EIP150Block: nil, // EIP150 HF block (nil = no fork)
		// EIP150 HF hash (needed for header only clients as only gas pricing changed)
		EIP150Hash:  common.BytesToHash([]byte("myHash")),
		EIP155Block: big.NewInt(0), // EIP155 HF block
		EIP158Block: big.NewInt(0), // EIP158 HF block
		// Byzantium switch block (nil = no fork, 0 = already on byzantium)
		ByzantiumBlock: big.NewInt(0),
		// Constantinople switch block (nil = no fork, 0 = already activated)
		ConstantinopleBlock: constantinopleBlock,
		// Various consensus engines
		Ethash: new(params.EthashConfig),
		Clique: &cliqueCfg,
	}
}
