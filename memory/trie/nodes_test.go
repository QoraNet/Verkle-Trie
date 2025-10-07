
package trie

import (
	"testing"

	"github.com/QoraNet/qoraDB/go/database/vt/commit"
	"github.com/stretchr/testify/require"
)

func TestInnerNode_Get_ReturnsZeroIfThereIsNoNextNode(t *testing.T) {
	require := require.New(t)
	innerNode := &inner{}
	require.Zero(innerNode.get(Key{}, 0))
}

func TestInnerNode_Get_ReturnsValueFromNextNode(t *testing.T) {
	require := require.New(t)

	key1 := Key{1, 2, 3}
	key2 := Key{1, 2, 4}

	var root node = newLeaf(key1)
	root = root.set(key1, 2, Value{42})
	root = root.set(key2, 2, Value{84})

	inner, ok := root.(*inner)
	require.True(ok, "Root should be an inner node")

	require.Equal(Value{42}, inner.get(key1, 2))
	require.Equal(Value{84}, inner.get(key2, 2))
}

func TestInnerNode_Set_CreatesNewLeafIfThereIsNoNextNode(t *testing.T) {
	require := require.New(t)

	key := Key{1, 2, 3}

	innerNode := &inner{}
	require.Nil(innerNode.children[key[2]])

	res, ok := innerNode.set(key, 2, Value{42}).(*inner)
	require.True(ok, "Setting a new key not result in a leaf node")
	require.Equal(innerNode, res, "Setting a new key should not change the inner node")

	require.NotNil(innerNode.children[key[2]])
}

func TestInnerNode_CommitCleanStateIsTracked(t *testing.T) {
	require := require.New(t)

	innerNode := &inner{}
	require.False(innerNode.commitmentClean)

	// Setting a value should mark the commitment as dirty.
	key := Key{1, 2, 3}
	innerNode.set(key, 0, Value{42})
	require.False(innerNode.commitmentClean)

	// Committing should clean the state.
	firstCommit := innerNode.commit()
	require.True(innerNode.commitmentClean)

	// Committing again should return the same commitment.
	secondCommit := innerNode.commit()
	require.True(innerNode.commitmentClean)
	require.True(firstCommit.Equal(secondCommit))

	// Setting another value should mark the commitment as dirty again.
	innerNode.set(Key{1, 2, 4}, 0, Value{84})
	require.False(innerNode.commitmentClean)
}

func TestInnerNode_Commit_ComputesCommitmentFromChildren(t *testing.T) {
	require := require.New(t)

	innerNode := &inner{}
	key1 := Key{1, 2, 3}
	key2 := Key{1, 2, 4}

	// Set two values in the inner node.
	innerNode.set(key1, 2, Value{42})
	innerNode.set(key2, 2, Value{84})

	// Compute the commitment.
	commitment := innerNode.commit()

	require.NotNil(commitment)
	require.True(commitment.IsValid())

	// The commitment should be computed from the values of the children.
	expectedCommitment := commit.Commit([256]commit.Value{
		3: innerNode.children[key1[2]].commit().ToValue(),
		4: innerNode.children[key2[2]].commit().ToValue(),
	})

	require.True(commitment.Equal(expectedCommitment))
}

func TestLeafNode_NewLeaf_ProducesEmptyLeafWithStem(t *testing.T) {
	require := require.New(t)

	// Create a new leaf node with a specific key.
	key := Key{1, 2, 3, 4, 5}
	leafNode := newLeaf(key)

	// Check that the stem is set correctly.
	require.Equal(key[:31], leafNode.stem[:], "Stem should match the first 31 bytes of the key")

	// Check that all values are initialized to zero.
	require.Equal([256]Value{}, leafNode.values, "All values should be initialized to zero")

	// Check that the used bitmap is empty.
	require.Equal([256 / 8]byte{}, leafNode.used, "Used bitmap should be empty")
}

func TestLeafNode_Get_ReturnsValueForMatchingStem(t *testing.T) {
	require := require.New(t)

	key := Key{1, 2, 3, 31: 1}
	leaf := newLeaf(key)

	// Initially, the value for the key should be zero.
	require.Zero(leaf.get(key, 0), "Value for the key should be zero initially")

	// Set a value for the key.
	leaf.set(key, 0, Value{42})

	// Now retrieving the value should return the set value.
	require.Equal(Value{42}, leaf.get(key, 0), "Value for the key should match the set value")
}

func TestLeafNode_Get_ReturnsZeroForNonMatchingStem(t *testing.T) {
	require := require.New(t)

	key1 := Key{1, 2, 3}
	key2 := Key{4, 5, 6}

	leaf := newLeaf(key1)
	leaf.set(key1, 0, Value{42})

	require.Zero(leaf.get(key2, 0), "Value for non-matching key should be zero")
}

func TestLeafNode_Set_SplitsLeafIfStemDoesNotMatch(t *testing.T) {
	require := require.New(t)

	key1 := Key{1, 2, 3}
	key2 := Key{1, 2, 4}

	leafNode := newLeaf(key1)
	leafNode.set(key1, 0, Value{42})

	// Setting a different key should split the leaf.
	newNode := leafNode.set(key2, 2, Value{84})

	// The new node should be an inner node now.
	innerNode, ok := newNode.(*inner)
	require.True(ok, "Setting a different key should create an inner node")

	// The inner node should have the leaf as one of its children.
	require.Equal(leafNode, innerNode.children[key1[2]].(*leaf))
	require.NotNil(innerNode.children[key2[2]])
}

func TestLeafNode_CanSetAndGetValues(t *testing.T) {
	require := require.New(t)

	key1 := Key{1, 2, 3, 31: 1}
	key2 := Key{1, 2, 3, 31: 2}
	key3 := Key{1, 2, 3, 31: 3}

	leaf := newLeaf(key1)

	require.False(leaf.isUsed(key1[31]))
	require.False(leaf.isUsed(key2[31]))
	require.False(leaf.isUsed(key3[31]))

	require.Zero(leaf.get(key1, 0))
	require.Zero(leaf.get(key2, 0))
	require.Zero(leaf.get(key3, 0))

	// Setting a value for key 1 makes the value retrievable and marks the
	// suffix as used.
	leaf.set(key1, 0, Value{10})

	require.True(leaf.isUsed(key1[31]))
	require.False(leaf.isUsed(key2[31]))
	require.False(leaf.isUsed(key3[31]))

	require.Equal(Value{10}, leaf.get(key1, 0))
	require.Zero(leaf.get(key2, 0))
	require.Zero(leaf.get(key3, 0))

	// Setting the value for key 2 to zero does not change the value but marks
	// the suffix as used.
	leaf.set(key2, 0, Value{})

	require.True(leaf.isUsed(key1[31]))
	require.True(leaf.isUsed(key2[31]))
	require.False(leaf.isUsed(key3[31]))

	require.Equal(Value{10}, leaf.get(key1, 0))
	require.Zero(leaf.get(key2, 0))
	require.Zero(leaf.get(key3, 0))

	// Resetting the value for key 1 to zero does not change the used bitmap.
	leaf.set(key1, 0, Value{})
	require.True(leaf.isUsed(key1[31]))
	require.True(leaf.isUsed(key2[31]))
	require.False(leaf.isUsed(key3[31]))

	require.Zero(leaf.get(key1, 0))
	require.Zero(leaf.get(key2, 0))
	require.Zero(leaf.get(key3, 0))
}

func TestLeafNode_CanComputeCommitment(t *testing.T) {
	require := require.New(t)

	key1 := Key{1, 2, 3, 31: 1}
	key2 := Key{1, 2, 3, 31: 130}

	val1 := Value{8: 1, 20: 10}
	val2 := Value{8: 2, 20: 20}

	leaf := newLeaf(key1)
	leaf.set(key1, 0, val1)
	leaf.set(key2, 0, val2)

	have := leaf.commit()

	require.NotNil(have)
	require.True(have.IsValid())

	low1 := commit.NewValueFromLittleEndianBytes(val1[:16])
	low2 := commit.NewValueFromLittleEndianBytes(val2[:16])
	high1 := commit.NewValueFromLittleEndianBytes(val1[16:])
	high2 := commit.NewValueFromLittleEndianBytes(val2[16:])

	low1.SetBit128()
	low2.SetBit128()

	c1 := commit.Commit([256]commit.Value{2: low1, 3: high1})
	c2 := commit.Commit([256]commit.Value{4: low2, 5: high2})

	want := commit.Commit([256]commit.Value{
		commit.NewValue(1),
		commit.NewValueFromLittleEndianBytes(key1[:31]),
		c1.ToValue(),
		c2.ToValue(),
	})
	require.True(have.Equal(want))
}

func TestLeafNode_CommitmentDirtyStateIsTracked(t *testing.T) {
	require := require.New(t)

	key1 := Key{1, 2, 3, 31: 1}
	key2 := Key{1, 2, 3, 31: 130}

	leaf := newLeaf(key1)
	require.False(leaf.commitmentClean)

	leaf.set(key1, 0, Value{10})
	require.False(leaf.commitmentClean)

	leaf.set(key2, 0, Value{20})
	require.False(leaf.commitmentClean)

	first := leaf.commit()
	require.True(leaf.commitmentClean)

	second := leaf.commit()
	require.True(leaf.commitmentClean)
	require.True(first.Equal(second))

	leaf.set(key1, 0, Value{30})
	require.False(leaf.commitmentClean)

	third := leaf.commit()
	require.True(leaf.commitmentClean)

	require.False(first.Equal(third))
}
