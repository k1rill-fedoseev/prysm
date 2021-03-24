package slasher

import (
	"context"
	"sort"
	"testing"

	types "github.com/prysmaticlabs/eth2-types"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	mock "github.com/prysmaticlabs/prysm/beacon-chain/blockchain/testing"
	dbtest "github.com/prysmaticlabs/prysm/beacon-chain/db/testing"
	slashertypes "github.com/prysmaticlabs/prysm/beacon-chain/slasher/types"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	logTest "github.com/sirupsen/logrus/hooks/test"
)

func Test_determineChunksToUpdateForValidators_FromLatestWrittenEpoch(t *testing.T) {
	beaconDB := dbtest.SetupDB(t)
	ctx := context.Background()

	// Check if the chunk at chunk index already exists in-memory.
	s := &Service{
		params: &Parameters{
			chunkSize:          2, // 2 epochs in a chunk.
			validatorChunkSize: 2, // 2 validators in a chunk.
			historyLength:      4,
		},
		serviceCfg: &ServiceConfig{
			Database:      beaconDB,
			StateNotifier: &mock.MockStateNotifier{},
		},
	}
	validators := []types.ValidatorIndex{
		1, 2,
	}
	currentEpoch := types.Epoch(3)

	// Set the latest written epoch for validators to current epoch - 1.
	latestWrittenEpoch := currentEpoch - 1
	err := beaconDB.SaveLastEpochWrittenForValidators(ctx, validators, latestWrittenEpoch)
	require.NoError(t, err)

	// Because the validators have no recorded latest epoch written in the database,
	// Because the latest written epoch for the input validators is == 2, we expect
	// that we will update all epochs from 2 up to 3 (the current epoch). This is all
	// safe contained in chunk index 1.
	chunkIndices, err := s.determineChunksToUpdateForValidators(
		ctx,
		&chunkUpdateArgs{
			currentEpoch: currentEpoch,
		},
		validators,
	)
	require.NoError(t, err)
	require.DeepEqual(t, []uint64{1}, chunkIndices)
}

func Test_determineChunksToUpdateForValidators_FromGenesis(t *testing.T) {
	beaconDB := dbtest.SetupDB(t)
	ctx := context.Background()

	// Check if the chunk at chunk index already exists in-memory.
	s := &Service{
		params: &Parameters{
			chunkSize:          2, // 2 epochs in a chunk.
			validatorChunkSize: 2, // 2 validators in a chunk.
			historyLength:      4,
		},
		serviceCfg: &ServiceConfig{
			Database:      beaconDB,
			StateNotifier: &mock.MockStateNotifier{},
		},
	}
	validators := []types.ValidatorIndex{
		1, 2,
	}
	// Because the validators have no recorded latest epoch written in the database,
	// we expect that we will update all epochs from genesis up to the current epoch.
	// Given the chunk size is 2 epochs per chunk, updating with current epoch == 3
	// will mean that we should be updating from epoch 0 to 3, meaning chunk indices 0 and 1.
	chunkIndices, err := s.determineChunksToUpdateForValidators(
		ctx,
		&chunkUpdateArgs{
			currentEpoch: 3,
		},
		validators,
	)
	require.NoError(t, err)
	sort.Slice(chunkIndices, func(i, j int) bool {
		return chunkIndices[i] < chunkIndices[j]
	})
	require.DeepEqual(t, []uint64{0, 1}, chunkIndices)
}

func Test_applyAttestationForValidator_MinSpanChunk(t *testing.T) {
	ctx := context.Background()
	beaconDB := dbtest.SetupDB(t)
	params := DefaultParams()
	srv := &Service{
		params: params,
		serviceCfg: &ServiceConfig{
			Database:      beaconDB,
			StateNotifier: &mock.MockStateNotifier{},
		},
	}
	// We initialize an empty chunks slice.
	chunk := EmptyMinSpanChunksSlice(params)
	chunkIdx := uint64(0)
	currentEpoch := types.Epoch(3)
	validatorIdx := types.ValidatorIndex(0)
	args := &chunkUpdateArgs{
		chunkIndex:   chunkIdx,
		currentEpoch: currentEpoch,
	}
	chunksByChunkIdx := map[uint64]Chunker{
		chunkIdx: chunk,
	}

	// We apply attestation with (source 1, target 2) for our validator.
	source := types.Epoch(1)
	target := types.Epoch(2)
	att := createAttestationWrapper(t, source, target, nil, nil)
	slashing, err := srv.applyAttestationForValidator(
		ctx,
		args,
		validatorIdx,
		chunksByChunkIdx,
		att,
	)
	require.NoError(t, err)
	require.Equal(t, true, slashing == nil)
	att.IndexedAttestation.AttestingIndices = []uint64{uint64(validatorIdx)}
	err = beaconDB.SaveAttestationRecordsForValidators(
		ctx,
		[]*slashertypes.IndexedAttestationWrapper{att},
	)
	require.NoError(t, err)

	// Next, we apply an attestation with (source 0, target 3) and
	// expect a slashable offense to be returned.
	source = types.Epoch(0)
	target = types.Epoch(3)
	slashableAtt := createAttestationWrapper(t, source, target, nil, nil)
	slashing, err = srv.applyAttestationForValidator(
		ctx,
		args,
		validatorIdx,
		chunksByChunkIdx,
		slashableAtt,
	)
	require.NoError(t, err)
	require.NotNil(t, slashing)
}

func Test_applyAttestationForValidator_MaxSpanChunk(t *testing.T) {
	ctx := context.Background()
	beaconDB := dbtest.SetupDB(t)
	params := DefaultParams()
	srv := &Service{
		params: params,
		serviceCfg: &ServiceConfig{
			Database:      beaconDB,
			StateNotifier: &mock.MockStateNotifier{},
		},
	}
	// We initialize an empty chunks slice.
	chunk := EmptyMaxSpanChunksSlice(params)
	chunkIdx := uint64(0)
	currentEpoch := types.Epoch(3)
	validatorIdx := types.ValidatorIndex(0)
	args := &chunkUpdateArgs{
		chunkIndex:   chunkIdx,
		currentEpoch: currentEpoch,
	}
	chunksByChunkIdx := map[uint64]Chunker{
		chunkIdx: chunk,
	}

	// We apply attestation with (source 0, target 3) for our validator.
	source := types.Epoch(0)
	target := types.Epoch(3)
	att := createAttestationWrapper(t, source, target, nil, nil)
	slashing, err := srv.applyAttestationForValidator(
		ctx,
		args,
		validatorIdx,
		chunksByChunkIdx,
		att,
	)
	require.NoError(t, err)
	require.Equal(t, true, slashing == nil)
	att.IndexedAttestation.AttestingIndices = []uint64{uint64(validatorIdx)}
	err = beaconDB.SaveAttestationRecordsForValidators(
		ctx,
		[]*slashertypes.IndexedAttestationWrapper{att},
	)
	require.NoError(t, err)

	// Next, we apply an attestation with (source 1, target 2) and
	// expect a slashable offense to be returned.
	source = types.Epoch(1)
	target = types.Epoch(2)
	slashableAtt := createAttestationWrapper(t, source, target, nil, nil)
	slashing, err = srv.applyAttestationForValidator(
		ctx,
		args,
		validatorIdx,
		chunksByChunkIdx,
		slashableAtt,
	)
	require.NoError(t, err)
	require.NotNil(t, slashing)
}

func Test_checkDoubleVotes_SlashableInputAttestations(t *testing.T) {
	beaconDB := dbtest.SetupDB(t)
	ctx := context.Background()
	// For a list of input attestations, check that we can
	// indeed check there could exist a double vote offense
	// within the list with respect to other entries in the list.
	atts := []*slashertypes.IndexedAttestationWrapper{
		createAttestationWrapper(t, 0, 1, []uint64{1, 2}, []byte{1}),
		createAttestationWrapper(t, 0, 2, []uint64{1, 2}, []byte{1}),
		createAttestationWrapper(t, 0, 2, []uint64{1, 2}, []byte{2}), // Different signing root.
	}
	srv := &Service{
		serviceCfg: &ServiceConfig{
			Database:      beaconDB,
			StateNotifier: &mock.MockStateNotifier{},
		},
	}
	prev1 := createAttestationWrapper(t, 0, 2, []uint64{1, 2}, []byte{1})
	cur1 := createAttestationWrapper(t, 0, 2, []uint64{1, 2}, []byte{2})
	prev2 := createAttestationWrapper(t, 0, 2, []uint64{1, 2}, []byte{1})
	cur2 := createAttestationWrapper(t, 0, 2, []uint64{1, 2}, []byte{2})
	wanted := []*ethpb.AttesterSlashing{
		{
			Attestation_1: prev1.IndexedAttestation,
			Attestation_2: cur1.IndexedAttestation,
		},
		{
			Attestation_1: prev2.IndexedAttestation,
			Attestation_2: cur2.IndexedAttestation,
		},
	}
	slashings, err := srv.checkDoubleVotes(ctx, atts)
	require.NoError(t, err)
	require.DeepEqual(t, wanted, slashings)
}

func Test_checkDoubleVotes_SlashableAttestationsOnDisk(t *testing.T) {
	beaconDB := dbtest.SetupDB(t)
	ctx := context.Background()
	// For a list of input attestations, check that we can
	// indeed check there could exist a double vote offense
	// within the list with respect to previous entries in the db.
	prevAtts := []*slashertypes.IndexedAttestationWrapper{
		createAttestationWrapper(t, 0, 1, []uint64{1, 2}, []byte{1}),
		createAttestationWrapper(t, 0, 2, []uint64{1, 2}, []byte{1}),
	}
	err := beaconDB.SaveAttestationRecordsForValidators(ctx, prevAtts)
	require.NoError(t, err)

	srv := &Service{
		serviceCfg: &ServiceConfig{
			Database:      beaconDB,
			StateNotifier: &mock.MockStateNotifier{},
		},
	}
	prev1 := createAttestationWrapper(t, 0, 2, []uint64{1, 2}, []byte{1})
	cur1 := createAttestationWrapper(t, 0, 2, []uint64{1, 2}, []byte{2})
	prev2 := createAttestationWrapper(t, 0, 2, []uint64{1, 2}, []byte{1})
	cur2 := createAttestationWrapper(t, 0, 2, []uint64{1, 2}, []byte{2})
	wanted := []*ethpb.AttesterSlashing{
		{
			Attestation_1: prev1.IndexedAttestation,
			Attestation_2: cur1.IndexedAttestation,
		},
		{
			Attestation_1: prev2.IndexedAttestation,
			Attestation_2: cur2.IndexedAttestation,
		},
	}
	newAtts := []*slashertypes.IndexedAttestationWrapper{
		createAttestationWrapper(t, 0, 2, []uint64{1, 2}, []byte{2}), // Different signing root.
	}
	slashings, err := srv.checkDoubleVotes(ctx, newAtts)
	require.NoError(t, err)
	require.DeepEqual(t, wanted, slashings)
}

func Test_loadChunks_MinSpans(t *testing.T) {
	testLoadChunks(t, slashertypes.MinSpan)
}

func Test_loadChunks_MaxSpans(t *testing.T) {
	testLoadChunks(t, slashertypes.MaxSpan)
}

func testLoadChunks(t *testing.T, kind slashertypes.ChunkKind) {
	beaconDB := dbtest.SetupDB(t)
	ctx := context.Background()

	// Check if the chunk at chunk index already exists in-memory.
	params := DefaultParams()
	s := &Service{
		params: DefaultParams(),
		serviceCfg: &ServiceConfig{
			Database:      beaconDB,
			StateNotifier: &mock.MockStateNotifier{},
		},
	}
	// If a chunk at a chunk index does not exist, ensure it
	// is initialized as an empty chunk.
	var emptyChunk Chunker
	if kind == slashertypes.MinSpan {
		emptyChunk = EmptyMinSpanChunksSlice(params)
	} else {
		emptyChunk = EmptyMaxSpanChunksSlice(params)
	}
	chunkIdx := uint64(2)
	received, err := s.loadChunks(ctx, &chunkUpdateArgs{
		validatorChunkIndex: 0,
		kind:                kind,
	}, []uint64{chunkIdx})
	require.NoError(t, err)
	wanted := map[uint64]Chunker{
		chunkIdx: emptyChunk,
	}
	require.DeepEqual(t, wanted, received)

	// Save chunks to disk, then load them properly from disk.
	var existingChunk Chunker
	if kind == slashertypes.MinSpan {
		existingChunk = EmptyMinSpanChunksSlice(params)
	} else {
		existingChunk = EmptyMaxSpanChunksSlice(params)
	}
	validatorIdx := types.ValidatorIndex(0)
	epochInChunk := types.Epoch(0)
	targetEpoch := types.Epoch(2)
	err = setChunkDataAtEpoch(
		params,
		existingChunk.Chunk(),
		validatorIdx,
		epochInChunk,
		targetEpoch,
	)
	require.NoError(t, err)
	require.DeepNotEqual(t, existingChunk, emptyChunk)

	updatedChunks := map[uint64]Chunker{
		2: existingChunk,
		4: existingChunk,
		6: existingChunk,
	}
	err = s.saveUpdatedChunks(
		ctx,
		&chunkUpdateArgs{
			validatorChunkIndex: 0,
			kind:                kind,
		},
		updatedChunks,
	)
	require.NoError(t, err)
	// Check if the retrieved chunks match what we just saved to disk.
	received, err = s.loadChunks(ctx, &chunkUpdateArgs{
		validatorChunkIndex: 0,
		kind:                kind,
	}, []uint64{2, 4, 6})
	require.NoError(t, err)
	require.DeepEqual(t, updatedChunks, received)
}

func TestService_processQueuedAttestations(t *testing.T) {
	hook := logTest.NewGlobal()
	beaconDB := dbtest.SetupDB(t)
	s := &Service{
		params: DefaultParams(),
		serviceCfg: &ServiceConfig{
			Database:      beaconDB,
			StateNotifier: &mock.MockStateNotifier{},
		},
		attsQueue: newAttestationsQueue(),
	}
	s.attsQueue.extend([]*slashertypes.IndexedAttestationWrapper{
		createAttestationWrapper(t, 0, 1, []uint64{0, 1} /* indices */, nil /* signingRoot */),
	})
	ctx, cancel := context.WithCancel(context.Background())
	tickerChan := make(chan types.Slot)
	exitChan := make(chan struct{})
	go func() {
		s.processQueuedAttestations(ctx, tickerChan)
		exitChan <- struct{}{}
	}()

	// Send a value over the ticker.
	tickerChan <- 1
	cancel()
	<-exitChan
	assert.LogsContain(t, hook, "New slot, processing queued")
}

func createAttestationWrapper(t *testing.T, source, target types.Epoch, indices []uint64, signingRoot []byte) *slashertypes.IndexedAttestationWrapper {
	data := &ethpb.AttestationData{
		BeaconBlockRoot: bytesutil.PadTo(signingRoot, 32),
		Source: &ethpb.Checkpoint{
			Epoch: source,
			Root:  params.BeaconConfig().ZeroHash[:],
		},
		Target: &ethpb.Checkpoint{
			Epoch: target,
			Root:  params.BeaconConfig().ZeroHash[:],
		},
	}
	signRoot, err := data.HashTreeRoot()
	if err != nil {
		t.Fatal(err)
	}
	return &slashertypes.IndexedAttestationWrapper{
		IndexedAttestation: &ethpb.IndexedAttestation{
			AttestingIndices: indices,
			Data:             data,
			Signature:        params.BeaconConfig().EmptySignature[:],
		},
		SigningRoot: signRoot,
	}
}