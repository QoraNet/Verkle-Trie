package geth

import (
	"github.com/QoraNet/qoraDB/go/common"
	"github.com/ethereum/go-ethereum/triedb/database"

	ethcommon "github.com/ethereum/go-ethereum/common"
)

//go:generate mockgen -source node_source.go -destination node_source_mocks.go -package geth

// NodeSource is an interface for a source of verkle nodes.
// It provides methods to get and set nodes at specific paths.
// It supports the adaptation for Geth's Verkle trie implementation.
type NodeSource interface {
	common.FlushAndCloser
	database.NodeReader

	// set sets the node at the given path.
	// The input is navigation path in the tree and the serialised node.
	set(path []byte, value []byte) error
}

// singleNodeReader is a wrapper around a single NodeReader.
// When the method NodeReader is called, it returns always the same NodeReader.
type singleNodeReader struct {
	source NodeSource
}

func (r singleNodeReader) NodeReader(stateRoot ethcommon.Hash) (database.NodeReader, error) {
	return r.source, nil
}

// getSource is a convenience method to retrieve the underlying NodeSource.
func (r singleNodeReader) getSource() NodeSource {
	return r.source
}
