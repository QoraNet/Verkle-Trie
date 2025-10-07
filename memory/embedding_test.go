package memory

import (
	"testing"

	"github.com/QoraNet/qoraDB/go/common"
	"github.com/QoraNet/qoraDB/go/database/vt/memory/trie"
	"github.com/ethereum/go-ethereum/trie/utils"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestGetTreeKey_CompareWithGethResults(t *testing.T) {

	getKey := func(address int, index int, subIndex byte) trie.Key {
		return getTrieKey(
			common.Address{byte(address)},
			*uint256.NewInt(uint64(index)),
			subIndex,
		)
	}

	getRefKey := func(address int, index int, subIndex byte) trie.Key {
		res := utils.GetTreeKey(
			[]byte{byte(address), 19: 0},
			uint256.NewInt(uint64(index)),
			subIndex,
		)
		return trie.Key(res[:])
	}

	const N = 10
	for i := range N {
		for j := range N {
			for k := range N {
				have := getKey(i, j, byte(k))
				want := getRefKey(i, j, byte(k))
				require.Equal(t, want, have, "i=%d,j=%d,k=%d", i, j, k)
			}
		}
	}
}

func TestBasicDataKey_CompareWithGethResults(t *testing.T) {
	addresses := []common.Address{
		{}, {1}, {2}, {1, 19: 2},
	}

	for _, address := range addresses {
		have := getBasicDataKey(address)
		want := trie.Key(utils.BasicDataKey(address[:]))
		require.Equal(t, want, have, "address=%x", address)
	}
}

func TestCodeHashKey_CompareWithGethResults(t *testing.T) {
	addresses := []common.Address{
		{}, {1}, {2}, {1, 19: 2},
	}

	for _, address := range addresses {
		have := getCodeHashKey(address)
		want := trie.Key(utils.CodeHashKey(address[:]))
		require.Equal(t, want, have, "address=%x", address)
	}
}

func TestGetCodeChunkKey_CompareWithGethResults(t *testing.T) {

	getKey := func(address int, chunkNumber int) trie.Key {
		return getCodeChunkKey(
			common.Address{byte(address)},
			chunkNumber,
		)
	}

	getRefKey := func(address int, chunkNumber int) trie.Key {
		res := utils.CodeChunkKey(
			[]byte{byte(address), 19: 0},
			uint256.NewInt(uint64(chunkNumber)),
		)
		return trie.Key(res[:])
	}

	const N = 10
	for i := range N {
		for _, j := range []int{0, 1, 2, 16, 64, 128, 255, 256, 10_000} {
			have := getKey(i, j)
			want := getRefKey(i, j)
			require.Equal(t, want, have, "i=%d,j=%d", i, j)
		}
	}
}

func TestGetStorageKey_CompareWithGethResults(t *testing.T) {

	getKey := func(address int, key int) trie.Key {
		return getStorageKey(
			common.Address{byte(address)},
			common.Key{byte(key)},
		)
	}

	getRefKey := func(address int, key int) trie.Key {
		res := utils.StorageSlotKey(
			[]byte{byte(address), 19: 0},
			[]byte{byte(key), 31: 0},
		)
		return trie.Key(res[:])
	}

	const N = 10
	for i := range N {
		for j := range N {
			have := getKey(i, j)
			want := getRefKey(i, j)
			require.Equal(t, want, have, "i=%d,j=%d", i, j)
		}
	}
}
