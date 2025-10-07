package geth

import (
	"github.com/QoraNet/qoraDB/go/common/immutable"
	"github.com/ethereum/go-ethereum/common"
)

// NodeSource is an interface for retrieving and storing nodes in memory.
// It serves as a base for different node storage implementations, such as
// in-memory storage, cached storage, or persistent storage.
// It is itself not intended for real-life usage, as the amount of memory
// would quickly grow to an unmanageable size.
type memorySource struct {
	nodes map[immutable.Bytes]immutable.Bytes
}

func newMemorySource() NodeSource {
	return &memorySource{
		nodes: make(map[immutable.Bytes]immutable.Bytes),
	}
}

func (s *memorySource) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	key := immutable.NewBytes(path)
	bytes, exists := s.nodes[key]
	if !exists {
		return nil, nil
	}
	return bytes.ToBytes(), nil
}

func (s *memorySource) set(path []byte, value []byte) error {
	s.nodes[immutable.NewBytes(path)] = immutable.NewBytes(value)

	return nil
}

func (s *memorySource) Flush() error {
	return nil
}

func (s *memorySource) Close() error {
	return s.Flush()
}
