
package trie

import (
	"testing"

	"github.com/QoraNet/qoraDB/go/database/vt/commit"
	"github.com/stretchr/testify/require"
)

func TestTrie_InitialTrieIsEmpty(t *testing.T) {
	require := require.New(t)

	trie := &Trie{}
	require.Zero(trie.Get(Key{1}))
	require.Zero(trie.Get(Key{2}))
	require.Zero(trie.Get(Key{3}))
}

func TestTrie_ValuesCanBeSetAndRetrieved(t *testing.T) {
	require := require.New(t)

	trie := &Trie{}

	require.Zero(trie.Get(Key{1}))
	require.Zero(trie.Get(Key{2}))
	require.Zero(trie.Get(Key{0, 31: 1}))
	require.Zero(trie.Get(Key{0, 31: 2}))

	trie.Set(Key{1}, Value{1})

	require.Equal(Value{1}, trie.Get(Key{1}))
	require.Zero(trie.Get(Key{2}))
	require.Zero(trie.Get(Key{0, 31: 1}))
	require.Zero(trie.Get(Key{0, 31: 2}))

	trie.Set(Key{2}, Value{2})

	require.Equal(Value{1}, trie.Get(Key{1}))
	require.Equal(Value{2}, trie.Get(Key{2}))
	require.Zero(trie.Get(Key{0, 31: 1}))
	require.Zero(trie.Get(Key{0, 31: 2}))

	trie.Set(Key{0, 31: 1}, Value{3})

	require.Equal(Value{1}, trie.Get(Key{1}))
	require.Equal(Value{2}, trie.Get(Key{2}))
	require.Equal(Value{3}, trie.Get(Key{0, 31: 1}))
	require.Zero(trie.Get(Key{0, 31: 2}))

	trie.Set(Key{0, 31: 2}, Value{4})

	require.Equal(Value{1}, trie.Get(Key{1}))
	require.Equal(Value{2}, trie.Get(Key{2}))
	require.Equal(Value{3}, trie.Get(Key{0, 31: 1}))
	require.Equal(Value{4}, trie.Get(Key{0, 31: 2}))
}

func TestTrie_ValuesCanBeUpdated(t *testing.T) {
	require := require.New(t)

	trie := &Trie{}

	key := Key{1}
	require.Zero(trie.Get(key))
	trie.Set(key, Value{1})
	require.Equal(Value{1}, trie.Get(key))
	trie.Set(key, Value{2})
	require.Equal(Value{2}, trie.Get(key))
	trie.Set(key, Value{3})
	require.Equal(Value{3}, trie.Get(key))
}

func TestTrie_ManyValuesCanBeSetAndRetrieved(t *testing.T) {
	const N = 1000
	require := require.New(t)

	toKey := func(i int) Key {
		return Key{byte(i >> 8 & 0x0F), byte(i >> 4 & 0x0F), 31: byte(i & 0x0F)}
	}

	trie := &Trie{}
	for i := range N {
		for j := range N {
			want := Value{}
			if j < i {
				want = Value{byte(j)}
			}
			got := trie.Get(toKey(j))
			require.Equal(want, got, "In round %d Get(%d) should return %v, got %v", i, j, want, got)
		}
		trie.Set(toKey(i), Value{byte(i)})
	}
}

func TestTrie_SettingASingleValueProducesAnInnerNode(t *testing.T) {
	require := require.New(t)

	trie := &Trie{}
	require.Nil(trie.root)
	trie.Set(Key{1}, Value{1})

	_, ok := trie.root.(*inner)
	require.True(ok, "Root should be an inner node after setting a value")
}

func TestTrie_CommitmentOfEmptyTrieIsIdentity(t *testing.T) {
	require := require.New(t)

	trie := &Trie{}
	have := trie.Commit()
	want := commit.Identity()
	require.True(have.Equal(want))
}

func TestTrie_CommitmentOfNonEmptyTrieIsRootNodeCommitment(t *testing.T) {
	require := require.New(t)

	trie := &Trie{}
	trie.Set(Key{1, 31: 1}, Value{1})
	trie.Set(Key{2, 31: 2}, Value{2})
	trie.Set(Key{3, 31: 3}, Value{3})

	have := trie.Commit()
	require.True(have.IsValid(), "Commitment should be valid")

	want := trie.root.commit()
	require.True(have.Equal(want), "Commitment should match the root's commitment")
}
