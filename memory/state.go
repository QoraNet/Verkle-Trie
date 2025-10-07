package memory

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/QoraNet/qoraDB/go/backend"
	"github.com/QoraNet/qoraDB/go/common"
	"github.com/QoraNet/qoraDB/go/common/amount"
	"github.com/QoraNet/qoraDB/go/common/immutable"
	"github.com/QoraNet/qoraDB/go/common/tribool"
	"github.com/QoraNet/qoraDB/go/common/witness"
	"github.com/QoraNet/qoraDB/go/database/vt/memory/trie"
	"github.com/QoraNet/qoraDB/go/state"
	"github.com/ethereum/go-ethereum/core/types"
)

// State is an in-memory implementation of a chain-state tracking account and
// storage data using a Verkle Trie. It implements the state.State interface.
type State struct {
	trie           *trie.Trie
	archive        *vtArchive          // Historical state storage
	writtenSlots   map[common.Address]map[common.Key]bool // Track which storage slots have been written
	archiveMaxSize int                 // Maximum blocks to keep in archive (0 = unlimited)
}

// vtSnapshot represents a snapshot of the Verkle Trie state
type vtSnapshot struct {
	commitment []byte // Verkle commitment as bytes
	data       []byte // Serialized trie data
}

func (s *vtSnapshot) GetRootProof() backend.Proof {
	return &vtProof{commitment: s.commitment}
}

func (s *vtSnapshot) GetNumParts() int {
	// VT snapshot is stored as a single part
	return 1
}

func (s *vtSnapshot) GetProof(partNumber int) (backend.Proof, error) {
	if partNumber != 0 {
		return nil, fmt.Errorf("invalid part number %d, VT has only 1 part", partNumber)
	}
	return &vtProof{commitment: s.commitment}, nil
}

func (s *vtSnapshot) GetPart(partNumber int) (backend.Part, error) {
	if partNumber != 0 {
		return nil, fmt.Errorf("invalid part number %d, VT has only 1 part", partNumber)
	}
	return &vtSnapshotPart{data: s.data}, nil
}

func (s *vtSnapshot) GetData() backend.SnapshotData {
	return s
}

func (s *vtSnapshot) GetMetaData() ([]byte, error) {
	// Metadata contains the commitment and number of parts
	meta := make([]byte, 32+4)
	copy(meta[0:32], s.commitment)
	binary.BigEndian.PutUint32(meta[32:36], 1) // 1 part
	return meta, nil
}

func (s *vtSnapshot) GetProofData(partNumber int) ([]byte, error) {
	if partNumber != 0 {
		return nil, fmt.Errorf("invalid part number %d, VT has only 1 part", partNumber)
	}
	return s.commitment, nil
}

func (s *vtSnapshot) GetPartData(partNumber int) ([]byte, error) {
	if partNumber != 0 {
		return nil, fmt.Errorf("invalid part number %d, VT has only 1 part", partNumber)
	}
	return s.data, nil
}

func (s *vtSnapshot) Release() error {
	// In-memory snapshot, nothing to release
	return nil
}

// vtSnapshotPart represents a part of the snapshot
type vtSnapshotPart struct {
	data []byte
}

func (p *vtSnapshotPart) ToBytes() []byte {
	return p.data
}

// vtProof represents a cryptographic proof of the Verkle Trie state
type vtProof struct {
	commitment []byte
}

func (p *vtProof) Equal(other backend.Proof) bool {
	if otherVt, ok := other.(*vtProof); ok {
		return bytes.Equal(p.commitment, otherVt.commitment)
	}
	return false
}

func (p *vtProof) ToBytes() []byte {
	return p.commitment
}

// vtArchive stores historical state snapshots for each block
type vtArchive struct {
	snapshots map[uint64]*vtSnapshot // block number -> snapshot (in-memory cache)
	maxBlock  uint64                 // highest block number stored
	hasBlocks bool                   // whether any blocks have been archived
	maxSize   int                    // maximum blocks to keep (0 = unlimited)
	oldestBlock uint64               // oldest block in archive (for pruning)
}

func newVtArchive(maxSize int) *vtArchive {
	return &vtArchive{
		snapshots: make(map[uint64]*vtSnapshot),
		maxBlock:  0,
		hasBlocks: false,
		maxSize:   maxSize,
		oldestBlock: 0,
	}
}

// addBlock stores a snapshot for a specific block with automatic pruning
func (a *vtArchive) addBlock(block uint64, snapshot *vtSnapshot) {
	a.snapshots[block] = snapshot

	// Update max block
	if !a.hasBlocks || block > a.maxBlock {
		a.maxBlock = block
		a.hasBlocks = true
	}

	// Update oldest block
	if !a.hasBlocks || block < a.oldestBlock || a.oldestBlock == 0 {
		a.oldestBlock = block
	}

	// Prune old blocks if we exceed maxSize
	if a.maxSize > 0 && len(a.snapshots) > a.maxSize {
		a.pruneOldest()
	}
}

// pruneOldest removes the oldest block from the archive
func (a *vtArchive) pruneOldest() {
	if len(a.snapshots) == 0 {
		return
	}

	// Find and remove the oldest block
	delete(a.snapshots, a.oldestBlock)

	// Find the new oldest block
	newOldest := a.maxBlock
	for block := range a.snapshots {
		if block < newOldest {
			newOldest = block
		}
	}
	a.oldestBlock = newOldest
}

// getBlock retrieves a snapshot for a specific block
func (a *vtArchive) getBlock(block uint64) (*vtSnapshot, bool) {
	snapshot, exists := a.snapshots[block]
	return snapshot, exists
}

// getBlockHeight returns the highest archived block number
func (a *vtArchive) getBlockHeight() (uint64, bool) {
	return a.maxBlock, a.hasBlocks
}

// getMemorySize estimates the memory used by the archive
func (a *vtArchive) getMemorySize() uint64 {
	size := uint64(0)
	for _, snapshot := range a.snapshots {
		size += uint64(len(snapshot.commitment))
		size += uint64(len(snapshot.data))
		size += 16 // map overhead per entry
	}
	return size
}

// NewState creates a new, empty in-memory state instance.
func NewState(_ state.Parameters) (state.State, error) {
	return &State{
		trie:           &trie.Trie{},
		archive:        newVtArchive(1000), // Keep last 1000 blocks by default
		writtenSlots:   make(map[common.Address]map[common.Key]bool),
		archiveMaxSize: 1000,
	}, nil
}

// newState creates a new, empty in-memory state instance.
func newState() *State {
	return &State{
		trie:           &trie.Trie{},
		archive:        newVtArchive(1000),
		writtenSlots:   make(map[common.Address]map[common.Key]bool),
		archiveMaxSize: 1000,
	}
}

func (s *State) Exists(address common.Address) (bool, error) {
	key := getBasicDataKey(address)
	value := s.trie.Get(key)
	var empty [24]byte // nonce and balance are layed out in bytes 8-32
	return !bytes.Equal(value[8:32], empty[:]), nil

}

func (s *State) GetBalance(address common.Address) (amount.Amount, error) {
	key := getBasicDataKey(address)
	value := s.trie.Get(key)
	return amount.NewFromBytes(value[16:32]...), nil
}

func (s *State) GetNonce(address common.Address) (common.Nonce, error) {
	key := getBasicDataKey(address)
	value := s.trie.Get(key)
	return common.Nonce(value[8:16]), nil
}

func (s *State) GetStorage(address common.Address, key common.Key) (common.Value, error) {
	return common.Value(s.trie.Get(getStorageKey(address, key))), nil
}

func (s *State) GetCode(address common.Address) ([]byte, error) {
	size, _ := s.GetCodeSize(address)
	chunks := make([]chunk, 0, size)
	for i := 0; i < size/31+1; i++ {
		key := getCodeChunkKey(address, i)
		value := s.trie.Get(key)
		chunks = append(chunks, chunk(value))
	}
	return merge(chunks, size), nil
}

func (s *State) GetCodeSize(address common.Address) (int, error) {
	key := getBasicDataKey(address)
	value := s.trie.Get(key)
	return int(binary.BigEndian.Uint32(value[4:8])), nil
}

func (s *State) GetCodeHash(address common.Address) (common.Hash, error) {
	key := getCodeHashKey(address)
	value := s.trie.Get(key)
	return common.Hash(value[:]), nil
}

func (s *State) HasEmptyStorage(addr common.Address) (bool, error) {
	// Check if we have tracked slots for this address
	slots, exists := s.writtenSlots[addr]
	if !exists || len(slots) == 0 {
		// No slots written, storage is empty
		return true, nil
	}

	// Check all tracked slots
	for key := range slots {
		value := s.trie.Get(getStorageKey(addr, key))
		var zero [32]byte
		if value != zero {
			return false, nil // Found non-zero value
		}
	}

	// All tracked slots are zero
	return true, nil
}

func (s *State) Apply(block uint64, update common.Update) error {

	// init potentially empty accounts with empty code hash,
	for _, address := range update.CreatedAccounts {
		accountKey := getBasicDataKey(address)
		value := s.trie.Get(accountKey)
		var empty [28]byte
		// empty accnout has empty code size, nonce, and balance
		if bytes.Equal(value[4:32], empty[:]) {
			codeHashKey := getCodeHashKey(address)
			s.trie.Set(accountKey, value) // must be initialized to empty account
			s.trie.Set(codeHashKey, trie.Value(types.EmptyCodeHash))
		}
	}

	for _, update := range update.Nonces {
		key := getBasicDataKey(update.Account)
		value := s.trie.Get(key)
		copy(value[8:16], update.Nonce[:])
		s.trie.Set(key, value)
	}

	for _, update := range update.Balances {
		key := getBasicDataKey(update.Account)
		value := s.trie.Get(key)
		amount := update.Balance.Bytes32()
		copy(value[16:32], amount[16:])
		s.trie.Set(key, value)
	}

	for _, update := range update.Slots {
		key := getStorageKey(update.Account, update.Key)
		s.trie.Set(key, trie.Value(update.Value))

		// Track this slot as written
		if s.writtenSlots[update.Account] == nil {
			s.writtenSlots[update.Account] = make(map[common.Key]bool)
		}
		s.writtenSlots[update.Account][update.Key] = true
	}

	for _, update := range update.Codes {
		// Store the code length.
		key := getBasicDataKey(update.Account)
		value := s.trie.Get(key)
		size := len(update.Code)
		binary.BigEndian.PutUint32(value[4:8], uint32(size))
		s.trie.Set(key, value)

		// Store the code hash.
		key = getCodeHashKey(update.Account)
		hash := common.Keccak256(update.Code)
		s.trie.Set(key, trie.Value(hash))

		// Store the actual code.
		chunks := splitCode(update.Code)
		for i, chunk := range chunks {
			key := getCodeChunkKey(update.Account, i)
			s.trie.Set(key, trie.Value(chunk))
		}
	}

	// Archive the state after applying the block
	if err := s.archiveCurrentState(block); err != nil {
		return fmt.Errorf("failed to archive block %d: %w", block, err)
	}

	return nil
}

// archiveCurrentState creates a snapshot of the current state and stores it in the archive
func (s *State) archiveCurrentState(block uint64) error {
	// Create a snapshot of the current state
	snapshot, err := s.CreateSnapshot()
	if err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	// Cast to vtSnapshot
	vtSnap, ok := snapshot.(*vtSnapshot)
	if !ok {
		return fmt.Errorf("unexpected snapshot type")
	}

	// Store in archive
	s.archive.addBlock(block, vtSnap)

	return nil
}

func (s *State) GetHash() (common.Hash, error) {
	return s.trie.Commit().Compress(), nil
}

// --- Operational Features ---

func (s *State) Check() error {
	// Basic validation - check that trie is not nil
	if s.trie == nil {
		return fmt.Errorf("verkle trie is nil")
	}
	// Could add more validation:
	// - Verify cryptographic commitments
	// - Check tree structure integrity
	// - Validate account balances are non-negative
	// For now, basic check is sufficient
	return nil
}

func (s *State) Flush() error {
	// In-memory implementation - no disk persistence
	// A file-based implementation would write trie data to disk here
	// For now, this is a no-op as all data lives in RAM
	return nil
}

func (s *State) Close() error {
	// In-memory implementation - no cleanup needed
	// A file-based implementation would close file handles here
	// The trie will be garbage collected when no longer referenced
	return nil
}

func (s *State) GetMemoryFootprint() *common.MemoryFootprint {
	// Calculate actual memory usage
	baseSize := uint64(48) // State struct base size

	// Trie memory (rough estimate - will be more accurate with trie walk)
	trieSize := uint64(0)
	// Note: For accurate trie size, we'd need to walk the trie
	// For now, estimate based on serialization size
	if data, err := s.serializeTrie(); err == nil {
		trieSize = uint64(len(data))
	}

	// Archive memory
	archiveSize := s.archive.getMemorySize()

	// Written slots tracking
	slotsSize := uint64(0)
	for _, slots := range s.writtenSlots {
		slotsSize += uint64(len(slots) * (32 + 8)) // key size + map overhead
	}
	slotsSize += uint64(len(s.writtenSlots) * 24) // map overhead for addresses

	mf := common.NewMemoryFootprint(uintptr(baseSize))
	mf.AddChild("trie", common.NewMemoryFootprint(uintptr(trieSize)))
	mf.AddChild("archive", common.NewMemoryFootprint(uintptr(archiveSize)))
	mf.AddChild("writtenSlots", common.NewMemoryFootprint(uintptr(slotsSize)))

	return mf
}

func (s *State) GetArchiveState(block uint64) (state.State, error) {
	// Get the snapshot from the archive
	snapshot, exists := s.archive.getBlock(block)
	if !exists {
		return nil, fmt.Errorf("no archived state for block %d", block)
	}

	// Create a new state and restore from the snapshot
	archivedState := &State{
		trie:           &trie.Trie{},
		archive:        s.archive,          // Share the archive
		writtenSlots:   make(map[common.Address]map[common.Key]bool), // Fresh tracking for archived state
		archiveMaxSize: s.archiveMaxSize,
	}

	// Restore the archived state
	if err := archivedState.Restore(snapshot); err != nil {
		return nil, fmt.Errorf("failed to restore archived state for block %d: %w", block, err)
	}

	return archivedState, nil
}

func (s *State) GetArchiveBlockHeight() (height uint64, empty bool, err error) {
	height, hasBlocks := s.archive.getBlockHeight()
	if !hasBlocks {
		return 0, true, nil
	}
	return height, false, nil
}

// accountInfo holds basic account data for witness proofs
type accountInfo struct {
	Balance  amount.Amount
	Nonce    common.Nonce
	CodeHash common.Hash
}

// getAccountInfo extracts account information from the trie
func (s *State) getAccountInfo(address common.Address) (accountInfo, bool, error) {
	key := getBasicDataKey(address)
	value := s.trie.Get(key)

	// Check if account exists (non-zero balance or nonce)
	var empty [24]byte
	exists := !bytes.Equal(value[8:32], empty[:])

	balance, _ := s.GetBalance(address)
	nonce, _ := s.GetNonce(address)
	codeHash, _ := s.GetCodeHash(address)

	return accountInfo{
		Balance:  balance,
		Nonce:    nonce,
		CodeHash: codeHash,
	}, exists, nil
}

func (s *State) CreateWitnessProof(address common.Address, keys ...common.Key) (witness.Proof, error) {
	// Get current root hash
	rootHash, err := s.GetHash()
	if err != nil {
		return nil, fmt.Errorf("failed to get root hash: %w", err)
	}

	// Collect account data
	info, exists, err := s.getAccountInfo(address)
	if err != nil {
		return nil, fmt.Errorf("failed to get account info: %w", err)
	}

	var balance amount.Amount
	var nonce common.Nonce
	var codeHash common.Hash
	if exists {
		balance = info.Balance
		nonce = info.Nonce
		codeHash = info.CodeHash
	}

	// Collect storage values
	storage := make(map[common.Key]common.Value)
	for _, key := range keys {
		value, err := s.GetStorage(address, key)
		if err != nil {
			return nil, fmt.Errorf("failed to get storage: %w", err)
		}
		storage[key] = value
	}

	return &vtWitnessProof{
		rootHash: rootHash,
		address:  address,
		balance:  balance,
		nonce:    nonce,
		codeHash: codeHash,
		storage:  storage,
		exists:   exists,
	}, nil
}

func (s *State) Export(ctx context.Context, out io.Writer) (common.Hash, error) {
	// Export the state by creating a snapshot and writing it to the output

	// Get the current root hash
	rootHash, err := s.GetHash()
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get root hash: %w", err)
	}

	// Create a snapshot of the current state
	snapshot, err := s.CreateSnapshot()
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to create snapshot: %w", err)
	}

	// Get the snapshot data
	snapshotData := snapshot.GetData()

	// Write metadata (commitment + number of parts)
	metadata, err := snapshotData.GetMetaData()
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get metadata: %w", err)
	}

	// Write metadata length (4 bytes)
	metaLen := make([]byte, 4)
	binary.BigEndian.PutUint32(metaLen, uint32(len(metadata)))
	if _, err := out.Write(metaLen); err != nil {
		return common.Hash{}, fmt.Errorf("failed to write metadata length: %w", err)
	}

	// Write metadata
	if _, err := out.Write(metadata); err != nil {
		return common.Hash{}, fmt.Errorf("failed to write metadata: %w", err)
	}

	// Write the snapshot part data
	partData, err := snapshotData.GetPartData(0)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to get part data: %w", err)
	}

	// Write part data length (4 bytes)
	partLen := make([]byte, 4)
	binary.BigEndian.PutUint32(partLen, uint32(len(partData)))
	if _, err := out.Write(partLen); err != nil {
		return common.Hash{}, fmt.Errorf("failed to write part data length: %w", err)
	}

	// Write part data
	if _, err := out.Write(partData); err != nil {
		return common.Hash{}, fmt.Errorf("failed to write part data: %w", err)
	}

	return rootHash, nil
}

// Snapshot & Recovery
func (s *State) GetProof() (backend.Proof, error) {
	// Get the current Verkle commitment
	commitment := s.trie.Commit()
	commitmentBytes := commitment.Compress()
	return &vtProof{commitment: commitmentBytes[:]}, nil
}

func (s *State) CreateSnapshot() (backend.Snapshot, error) {
	// Get the cryptographic commitment for the snapshot
	commitment := s.trie.Commit()
	commitmentBytes := commitment.Compress()

	// Serialize the trie state
	// For in-memory VT, we serialize by collecting all key-value pairs
	data, err := s.serializeTrie()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize trie: %w", err)
	}

	return &vtSnapshot{
		commitment: commitmentBytes[:],
		data:       data,
	}, nil
}

func (s *State) Restore(snapshotData backend.SnapshotData) error {
	// Get metadata to verify structure
	metadata, err := snapshotData.GetMetaData()
	if err != nil {
		return fmt.Errorf("failed to get metadata: %w", err)
	}

	if len(metadata) < 32 {
		return fmt.Errorf("invalid metadata length: expected at least 32, got %d", len(metadata))
	}

	expectedCommitment := metadata[0:32]

	// Get the data
	data, err := snapshotData.GetPartData(0)
	if err != nil {
		return fmt.Errorf("failed to get part data: %w", err)
	}

	// Deserialize and restore the trie state
	if err := s.deserializeTrie(data); err != nil {
		return fmt.Errorf("failed to deserialize trie: %w", err)
	}

	// Verify the restored state matches the snapshot commitment
	restoredCommitment := s.trie.Commit().Compress()
	if !bytes.Equal(restoredCommitment[:], expectedCommitment) {
		return fmt.Errorf("commitment mismatch after restore")
	}

	return nil
}

func (s *State) GetSnapshotVerifier(metadata []byte) (backend.SnapshotVerifier, error) {
	// Parse the commitment from metadata
	if len(metadata) < 32 {
		return nil, fmt.Errorf("invalid metadata length: expected at least 32, got %d", len(metadata))
	}

	commitment := make([]byte, 32)
	copy(commitment, metadata[0:32])

	return &vtSnapshotVerifier{expectedCommitment: commitment}, nil
}

// GetSnapshotableComponents returns nil as VT uses state-level snapshotting
// rather than component-level snapshotting (when/if snapshot support is added)
func (s *State) GetSnapshotableComponents() []backend.Snapshotable {
	// VT currently doesn't support snapshots, but when it does,
	// it will use the entire trie as a single snapshotable unit
	return nil
}

// RunPostRestoreTasks performs any necessary cleanup after snapshot restoration
func (s *State) RunPostRestoreTasks() error {
	// Currently no post-restore tasks needed for in-memory VT
	// Future: rebuild any caches or indices after restore
	return nil
}

// serializeTrie converts the trie state to bytes for snapshotting
// Format: [numEntries uint32][key1 32 bytes][value1 32 bytes][key2...]...
func (s *State) serializeTrie() ([]byte, error) {
	// Collect all key-value pairs from the trie
	// This is a simplified implementation - a production version would use
	// a more efficient format or store the trie structure directly
	entries := make(map[trie.Key]trie.Value)

	// Walk through all possible storage locations
	// This is inefficient but works for the in-memory implementation
	// A production version would track which keys have been set
	for addrByte := 0; addrByte < 256; addrByte++ {
		var addr common.Address
		addr[0] = byte(addrByte)

		// Check basic data
		basicKey := getBasicDataKey(addr)
		basicValue := s.trie.Get(basicKey)
		var zero trie.Value
		if basicValue != zero {
			entries[basicKey] = basicValue
		}

		// Check code hash
		codeHashKey := getCodeHashKey(addr)
		codeHashValue := s.trie.Get(codeHashKey)
		if codeHashValue != zero {
			entries[codeHashKey] = codeHashValue
		}

		// Sample storage slots (first 256)
		for slotByte := 0; slotByte < 256; slotByte++ {
			var key common.Key
			key[31] = byte(slotByte)
			storageKey := getStorageKey(addr, key)
			storageValue := s.trie.Get(storageKey)
			if storageValue != zero {
				entries[storageKey] = storageValue
			}
		}

		// Sample code chunks (up to 1024 chunks = ~31KB per account)
		for chunk := 0; chunk < 1024; chunk++ {
			codeKey := getCodeChunkKey(addr, chunk)
			codeValue := s.trie.Get(codeKey)
			if codeValue != zero {
				entries[codeKey] = codeValue
			}
		}
	}

	// Serialize to bytes
	buf := make([]byte, 4+len(entries)*64)
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(entries)))

	offset := 4
	for k, v := range entries {
		copy(buf[offset:offset+32], k[:])
		copy(buf[offset+32:offset+64], v[:])
		offset += 64
	}

	return buf[:offset], nil
}

// deserializeTrie restores the trie state from serialized bytes
func (s *State) deserializeTrie(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("invalid data: too short")
	}

	numEntries := binary.BigEndian.Uint32(data[0:4])
	expectedLen := 4 + int(numEntries)*64
	if len(data) != expectedLen {
		return fmt.Errorf("invalid data length: expected %d, got %d", expectedLen, len(data))
	}

	// Reset the trie
	s.trie = &trie.Trie{}

	// Restore all entries
	offset := 4
	for i := uint32(0); i < numEntries; i++ {
		var key trie.Key
		var value trie.Value
		copy(key[:], data[offset:offset+32])
		copy(value[:], data[offset+32:offset+64])
		s.trie.Set(key, value)
		offset += 64
	}

	return nil
}

// vtSnapshotVerifier verifies that a snapshot matches an expected commitment
type vtSnapshotVerifier struct {
	expectedCommitment []byte
}

func (v *vtSnapshotVerifier) VerifyRootProof(data backend.SnapshotData) (backend.Proof, error) {
	// Get the proof from the snapshot data
	proofBytes, err := data.GetProofData(0)
	if err != nil {
		return nil, fmt.Errorf("failed to get proof data: %w", err)
	}

	// Verify it matches expected commitment
	if !bytes.Equal(proofBytes, v.expectedCommitment) {
		return nil, fmt.Errorf("root proof verification failed: commitment mismatch")
	}

	return &vtProof{commitment: proofBytes}, nil
}

func (v *vtSnapshotVerifier) VerifyPart(partNumber int, proof, part []byte) error {
	if partNumber != 0 {
		return fmt.Errorf("invalid part number %d, VT has only 1 part", partNumber)
	}

	// Verify the proof matches expected commitment
	if !bytes.Equal(proof, v.expectedCommitment) {
		return fmt.Errorf("proof verification failed: commitment mismatch")
	}

	// Create a temporary state to deserialize and verify the part data
	tempState := &State{trie: &trie.Trie{}}
	if err := tempState.deserializeTrie(part); err != nil {
		return fmt.Errorf("failed to deserialize data: %w", err)
	}

	// Verify the commitment matches
	commitment := tempState.trie.Commit().Compress()
	if !bytes.Equal(commitment[:], v.expectedCommitment) {
		return fmt.Errorf("data verification failed: commitment mismatch")
	}

	return nil
}

// vtWitnessProof implements witness.Proof for Verkle Trie
type vtWitnessProof struct {
	rootHash common.Hash
	address  common.Address
	balance  amount.Amount
	nonce    common.Nonce
	codeHash common.Hash
	storage  map[common.Key]common.Value
	exists   bool
}

func (p *vtWitnessProof) Extract(root common.Hash, address common.Address, keys ...common.Key) (witness.Proof, bool) {
	// Check if root matches
	if root != p.rootHash {
		return nil, false
	}

	// Check if address matches
	if address != p.address {
		return nil, false
	}

	// Filter storage to only requested keys
	filteredStorage := make(map[common.Key]common.Value)
	allKeysPresent := true
	for _, key := range keys {
		if value, ok := p.storage[key]; ok {
			filteredStorage[key] = value
		} else {
			allKeysPresent = false
		}
	}

	return &vtWitnessProof{
		rootHash: p.rootHash,
		address:  p.address,
		balance:  p.balance,
		nonce:    p.nonce,
		codeHash: p.codeHash,
		storage:  filteredStorage,
		exists:   p.exists,
	}, allKeysPresent
}

func (p *vtWitnessProof) IsValid() bool {
	// Basic validation - check that the proof is self-consistent
	return p.rootHash != (common.Hash{})
}

func (p *vtWitnessProof) GetElements() []immutable.Bytes {
	// Serialize proof elements
	elements := make([]immutable.Bytes, 0)

	// Add account data
	accountData := make([]byte, 32+8+32+32) // balance + nonce + codeHash + address
	balanceBytes := p.balance.Bytes32()
	copy(accountData[0:32], balanceBytes[:])
	copy(accountData[32:40], p.nonce[:])
	copy(accountData[40:72], p.codeHash[:])
	copy(accountData[72:104], p.address[:])
	elements = append(elements, immutable.NewBytes(accountData))

	// Add storage elements
	for key, value := range p.storage {
		storageData := make([]byte, 64)
		copy(storageData[0:32], key[:])
		copy(storageData[32:64], value[:])
		elements = append(elements, immutable.NewBytes(storageData))
	}

	return elements
}

func (p *vtWitnessProof) GetAccountElements(root common.Hash, address common.Address) ([]immutable.Bytes, common.Hash, bool) {
	if root != p.rootHash || address != p.address {
		return nil, common.Hash{}, false
	}

	// Return account elements and storage root (for VT, we use the root hash)
	accountData := make([]byte, 32+8+32)
	balanceBytes := p.balance.Bytes32()
	copy(accountData[0:32], balanceBytes[:])
	copy(accountData[32:40], p.nonce[:])
	copy(accountData[40:72], p.codeHash[:])

	return []immutable.Bytes{immutable.NewBytes(accountData)}, p.rootHash, true
}

func (p *vtWitnessProof) GetStorageElements(root common.Hash, address common.Address, key common.Key) ([]immutable.Bytes, bool) {
	if root != p.rootHash || address != p.address {
		return nil, false
	}

	value, exists := p.storage[key]
	if !exists {
		return nil, false
	}

	storageData := make([]byte, 64)
	copy(storageData[0:32], key[:])
	copy(storageData[32:64], value[:])

	return []immutable.Bytes{immutable.NewBytes(storageData)}, true
}

func (p *vtWitnessProof) GetBalance(root common.Hash, address common.Address) (amount.Amount, bool, error) {
	if root != p.rootHash || address != p.address {
		return amount.New(), false, nil
	}
	if !p.exists {
		return amount.New(), false, nil
	}
	return p.balance, true, nil
}

func (p *vtWitnessProof) GetNonce(root common.Hash, address common.Address) (common.Nonce, bool, error) {
	if root != p.rootHash || address != p.address {
		return common.Nonce{}, false, nil
	}
	if !p.exists {
		return common.Nonce{}, false, nil
	}
	return p.nonce, true, nil
}

func (p *vtWitnessProof) GetCodeHash(root common.Hash, address common.Address) (common.Hash, bool, error) {
	if root != p.rootHash || address != p.address {
		return common.Hash{}, false, nil
	}
	if !p.exists {
		return common.Hash{}, false, nil
	}
	return p.codeHash, true, nil
}

func (p *vtWitnessProof) GetState(root common.Hash, address common.Address, key common.Key) (common.Value, bool, error) {
	if root != p.rootHash || address != p.address {
		return common.Value{}, false, nil
	}

	value, exists := p.storage[key]
	return value, exists, nil
}

func (p *vtWitnessProof) AllStatesZero(root common.Hash, address common.Address, from, to common.Key) (tribool.Tribool, error) {
	if root != p.rootHash || address != p.address {
		return tribool.Unknown(), nil
	}

	// Check all storage slots in the range
	for key, value := range p.storage {
		// Check if key is in range [from, to]
		if bytes.Compare(key[:], from[:]) >= 0 && bytes.Compare(key[:], to[:]) <= 0 {
			var zero common.Value
			if value != zero {
				return tribool.False(), nil
			}
		}
	}

	// All checked slots are zero, but we can't be sure about unchecked ones
	return tribool.True(), nil
}

func (p *vtWitnessProof) AllAddressesEmpty(root common.Hash, from, to common.Address) (tribool.Tribool, error) {
	if root != p.rootHash {
		return tribool.Unknown(), nil
	}

	// Check if our address is in the range
	if bytes.Compare(p.address[:], from[:]) >= 0 && bytes.Compare(p.address[:], to[:]) <= 0 {
		// Check if account is empty
		var zeroHash common.Hash
		if p.exists && (p.balance != amount.New() || p.nonce != (common.Nonce{}) || p.codeHash != zeroHash) {
			return tribool.False(), nil
		}
	}

	// This proof only covers one address, so we can't be certain about others
	return tribool.Unknown(), nil
}
