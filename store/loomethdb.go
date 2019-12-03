package store

import (
	"bytes"
	"log"
	"os"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	loom "github.com/loomnetwork/go-loom"
	"github.com/loomnetwork/go-loom/util"
)

var (
	LogEthDBBatch = true
	logger        log.Logger
	loggerStarted = false
)

type EthDBLogContext struct {
	blockHeight  int64
	contractAddr loom.Address
	callerAddr   loom.Address
}

func NewEthDBLogContext(height int64, contractAddr loom.Address, callerAddr loom.Address) *EthDBLogContext {
	return &EthDBLogContext{
		blockHeight:  height,
		contractAddr: contractAddr,
		callerAddr:   callerAddr,
	}
}

// implements ethdb.Database
type LoomEthDB struct {
	evmstore   *EvmStore
	lock       sync.RWMutex
	logContext *EthDBLogContext
}

func NewLoomEthDB(evmstore *EvmStore, logContext *EthDBLogContext) *LoomEthDB {
	return &LoomEthDB{
		evmstore:   evmstore,
		logContext: logContext,
	}
}

func (s *LoomEthDB) Put(key []byte, value []byte) error {
	key = util.PrefixKey(vmPrefix, key)
	s.evmstore.Set(key, value)
	return nil
}

func (s *LoomEthDB) Get(key []byte) ([]byte, error) {
	key = util.PrefixKey(vmPrefix, key)
	return s.evmstore.Get(key), nil
}

func (s *LoomEthDB) Has(key []byte) (bool, error) {
	key = util.PrefixKey(vmPrefix, key)
	return s.evmstore.Has(key), nil
}

func (s *LoomEthDB) Delete(key []byte) error {
	key = util.PrefixKey(vmPrefix, key)
	s.evmstore.Delete(key)
	return nil
}

func (s *LoomEthDB) Close() {
}

func (s *LoomEthDB) NewBatch() ethdb.Batch {
	if LogEthDBBatch {
		return s.NewLogBatch(s.logContext)
	} else {
		newBatch := new(batch)
		newBatch.parentStore = s
		newBatch.Reset()
		return newBatch
	}
}

// implements ethdb.Batch
type kvPair struct {
	key   []byte
	value []byte
}

type batch struct {
	cache       []kvPair
	parentStore *LoomEthDB
	size        int
}

func (b *batch) Put(key, value []byte) error {
	b.cache = append(b.cache, kvPair{
		key:   common.CopyBytes(key),
		value: common.CopyBytes(value),
	})
	b.size += len(value)
	return nil
}

func (b *batch) ValueSize() int {
	return b.size
}

func (b *batch) Write() error {
	b.parentStore.lock.Lock()
	defer b.parentStore.lock.Unlock()

	sort.Slice(b.cache, func(j, k int) bool {
		return bytes.Compare(b.cache[j].key, b.cache[k].key) < 0
	})

	levelDBBatch := b.parentStore.evmstore.NewBatch()
	for _, kv := range b.cache {
		if kv.value == nil {
			levelDBBatch.Delete(util.PrefixKey(vmPrefix, kv.key))
		} else {
			levelDBBatch.Set(util.PrefixKey(vmPrefix, kv.key), kv.value)
		}
	}
	levelDBBatch.WriteSync()
	return nil
}

func (b *batch) Reset() {
	b.cache = make([]kvPair, 0)
	b.size = 0
}

func (b *batch) Delete(key []byte) error {
	b.cache = append(b.cache, kvPair{
		key:   common.CopyBytes(key),
		value: nil,
	})
	return nil
}

func (b *batch) Dump(logger *log.Logger) {
	b.parentStore.lock.Lock()
	defer b.parentStore.lock.Unlock()
	logger.Print("\n---- BATCH DUMP ----\n")
	for i, kv := range b.cache {
		logger.Printf("IDX %d, KEY %s\n", i, kv.key)
	}
}

type EthDBLogParams struct {
	LogFilename        string
	LogFlags           int
	LogReset           bool
	LogDelete          bool
	LogWrite           bool
	LogValueSize       bool
	LogPutKey          bool
	LogPutValue        bool
	LogPutDump         bool
	LogWriteDump       bool
	LogBeforeWriteDump bool
}

type LogBatch struct {
	batch  batch
	params EthDBLogParams
}

const batchHeaderWithContext = `

-----------------------------
| NEW BATCH
| Block: %v
| Contract: %v
| Caller: %v
-----------------------------

`

const batchHeader = `

-----------------------------
| NEW BATCH
-----------------------------

`

func (s *LoomEthDB) NewLogBatch(logContext *EthDBLogContext) ethdb.Batch {
	b := new(LogBatch)
	b.batch = *new(batch)
	b.batch.parentStore = s
	b.batch.Reset()
	b.params = EthDBLogParams{
		LogFilename:        "ethdb-batch.log",
		LogFlags:           0,
		LogReset:           true,
		LogDelete:          true,
		LogWrite:           true,
		LogValueSize:       false,
		LogPutKey:          true,
		LogPutValue:        false,
		LogPutDump:         false,
		LogWriteDump:       true,
		LogBeforeWriteDump: false,
	}

	if !loggerStarted {
		file, err := os.Create(b.params.LogFilename)
		if err != nil {
			return &b.batch
		}
		logger = *log.New(file, "", b.params.LogFlags)
		logger.Println("Created ethdb batch logger")
		loggerStarted = true
	}
	if logContext != nil {
		logger.Printf(batchHeaderWithContext, logContext.blockHeight, logContext.contractAddr, logContext.callerAddr)
	} else {
		logger.Print(batchHeader)
	}
	return b
}

func (b *LogBatch) Delete(key []byte) error {
	if b.params.LogDelete {
		logger.Println("Delete key: ", string(key))
	}
	return b.batch.Delete(key)
}

func (b *LogBatch) Put(key, value []byte) error {
	if b.params.LogPutKey {
		logger.Println("Put key: ", string(key))
	}
	if b.params.LogPutValue {
		logger.Println("Put value: ", string(value))
	}
	err := b.batch.Put(key, value)
	if b.params.LogPutDump {
		b.batch.Dump(&logger)
	}
	return err
}

func (b *LogBatch) ValueSize() int {
	size := b.batch.ValueSize()
	if b.params.LogValueSize {
		logger.Println("ValueSize : ", size)
	}
	return size
}

func (b *LogBatch) Write() error {
	if b.params.LogWrite {
		logger.Println("Write")
	}
	if b.params.LogBeforeWriteDump {
		logger.Println("Write, before : ")
		b.batch.Dump(&logger)
	}
	err := b.batch.Write()
	if b.params.LogWriteDump {
		logger.Println("Write, after : ")
		b.batch.Dump(&logger)
	}
	return err
}

func (b *LogBatch) Reset() {
	if b.params.LogReset {
		logger.Println("Reset batch")
	}
	b.batch.Reset()
}

// sortKeys sorts prefixed keys, it will sort the postfix of the key in ascending lexographical order
func sortKeys(prefix []byte, kvs []kvPair) []kvPair {
	var unsorted, sorted []int
	var tmpKv []kvPair
	for i, kv := range kvs {
		if 0 == bytes.Compare(prefix, kv.key[:len(prefix)]) {
			unsorted = append(unsorted, i)
			sorted = append(sorted, i)
		}
		tmpKv = append(tmpKv, kv)
	}
	sort.Slice(sorted, func(j, k int) bool {
		return bytes.Compare(kvs[sorted[j]].key, kvs[sorted[k]].key) < 0
	})
	for index := 0; index < len(sorted); index++ {
		kvs[unsorted[index]] = tmpKv[sorted[index]]
	}
	return kvs
}
