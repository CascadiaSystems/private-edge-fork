package signer

import (
	"crypto/ecdsa"
	"reflect"
	"testing"

	"github.com/0xPolygon/polygon-edge/consensus/ibft/validators"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/helper/tests"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/assert"
)

// useIstanbulHeaderHash is a helper function so that test use istanbulHeaderHash during the test
func useIstanbulHeaderHash(t *testing.T, signer Signer) {
	t.Helper()

	originalHashCalc := types.HeaderHash
	types.HeaderHash = func(h *types.Header) types.Hash {
		hash, err := signer.CalculateHeaderHash(h)
		if err != nil {
			return types.ZeroHash
		}

		return hash
	}

	t.Cleanup(func() {
		types.HeaderHash = originalHashCalc
	})
}

func TestExtraEncoding(t *testing.T) {
	seal1 := types.StringToHash("1").Bytes()
	seal2 := types.StringToHash("2").Bytes()

	cases := []struct {
		from *IstanbulExtra
		to   *IstanbulExtra
	}{
		{
			from: &IstanbulExtra{
				Validators: &validators.ECDSAValidatorSet{
					types.StringToAddress("1"),
				},
				Seal: seal1,
				CommittedSeal: &SerializedSeal{
					seal1,
				},
				ParentCommittedSeal: &SerializedSeal{
					seal2,
				},
			},
			to: &IstanbulExtra{
				Validators:          &validators.ECDSAValidatorSet{},
				Seal:                seal1,
				CommittedSeal:       &SerializedSeal{},
				ParentCommittedSeal: &SerializedSeal{},
			},
		},
	}

	for _, c := range cases {
		data := c.from.MarshalRLPTo(nil)

		if err := c.to.UnmarshalRLP(data); err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(c.from, c.to) {
			t.Fatal("bad")
		}
	}
}

func generateKeysAndAddresses(t *testing.T, num int) ([]*ecdsa.PrivateKey, []types.Address) {
	t.Helper()

	keys := make([]*ecdsa.PrivateKey, num)
	addrs := make([]types.Address, num)

	for i := range keys {
		pk, addr := tests.GenerateKeyAndAddr(t)
		keys[i] = pk
		addrs[i] = addr
	}

	return keys, addrs
}

func createIBFTHeader(
	t *testing.T,
	signer Signer,
	num uint64,
	parentHeader *types.Header,
	validators validators.ValidatorSet,
) *types.Header {
	t.Helper()

	header := &types.Header{
		Number:     num,
		ParentHash: parentHeader.Hash,
	}

	assert.NoError(t, signer.InitIBFTExtra(header, parentHeader, validators))

	return header
}

// Test Scenario
// 1. 4 IBFT Validators create headers
// 2. A faulty node scans the past headers and appends new committed seal
// 3. Check if each hash of the headers is wrong
func TestAppendECDSACommittedSeal(t *testing.T) {
	var (
		numHeaders          = 5
		numNormalValidators = 4
		numFaultyValidators = 1

		headers       = make([]*types.Header, 0, numHeaders)
		faultyHeaders = make([]*types.Header, 0, numHeaders)
		parentHeader  = &types.Header{}

		keys, addresses     = generateKeysAndAddresses(t, numNormalValidators+numFaultyValidators)
		normalValidatorKeys = keys[:numNormalValidators]
		faultyValidatorKey  = keys[numNormalValidators]

		signerA = &SignerImpl{
			NewECDSAKeyManagerFromKey(keys[0]),
		}
		validators = validators.ECDSAValidatorSet(addresses)

		err error
	)

	useIstanbulHeaderHash(t, signerA)

	// create headers by normal validators
	for i := 0; i < numHeaders; i++ {
		header := createIBFTHeader(t, signerA, uint64(i+1), parentHeader, &validators)

		// write seal
		header, err = signerA.WriteSeal(header)
		assert.NoError(t, err)

		// write committed seal
		committedSeal := make(map[types.Address][]byte, len(normalValidatorKeys))

		for _, key := range normalValidatorKeys {
			signer := NewSigner(NewECDSAKeyManagerFromKey(key))
			seal, err := signer.CreateCommittedSeal(header)

			assert.NoError(t, err)

			committedSeal[crypto.PubKeyToAddress(&key.PublicKey)] = seal
		}

		header, err = signerA.WriteCommittedSeals(header, committedSeal)

		assert.NoError(t, err)

		header = header.ComputeHash()

		headers = append(headers, header)

		parentHeader = header
	}

	// faulty node scans the past headers and try to inject new committed seal
	for i, h := range headers {
		header := h.Copy()

		// update parent hash & committed seal
		if i > 0 {
			parentHeader := faultyHeaders[i-1]

			// update parent hash
			header.ParentHash = parentHeader.Hash

			// get parent committed seal
			extra, err := signerA.GetIBFTExtra(parentHeader)
			assert.NoError(t, err)

			parentCommittedSeal := extra.ParentCommittedSeal

			// update ParentCommittedSeal forcibly
			err = signerA.packFieldIntoIbftExtra(header, func(extra *IstanbulExtra) {
				extra.ParentCommittedSeal = parentCommittedSeal
			})
			assert.NoError(t, err)
		}

		// create new committed seal
		faultySigner := NewSigner(NewECDSAKeyManagerFromKey(faultyValidatorKey))
		fx, err := faultySigner.CreateCommittedSeal(header)
		assert.NoError(t, err)

		// append new committed seal
		err = signerA.packFieldIntoIbftExtra(header, func(extra *IstanbulExtra) {
			sseal, _ := extra.CommittedSeal.(*SerializedSeal)
			ssealSlice := [][]byte(*sseal)

			ssealSlice = append(ssealSlice, fx)

			nsseal := SerializedSeal(ssealSlice)

			extra.CommittedSeal = &nsseal
		})
		assert.NoError(t, err)

		header = header.ComputeHash()
		faultyHeaders = append(faultyHeaders, header)
	}

	// Check hashes are different
	for i := range headers {
		header, faultyHeader := headers[i], faultyHeaders[i]

		if i == 0 {
			// hashes should be same because first header doesn't have parent committed seal
			assert.Equal(t, header.Hash, faultyHeader.Hash)
		} else {
			assert.NotEqual(t, header.Hash, faultyHeader.Hash)
		}
	}
}