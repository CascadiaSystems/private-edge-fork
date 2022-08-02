package predeployment

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/contracts/staking"
	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/state"
	itrie "github.com/0xPolygon/polygon-edge/state/immutable-trie"
	"github.com/0xPolygon/polygon-edge/state/runtime"
	"github.com/0xPolygon/polygon-edge/state/runtime/evm"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/umbracle/ethgo/abi"
	"io/ioutil"
	"math"
	"math/big"
	"os"
	"strings"
)

var (
	errABINotFound              = errors.New("abi field not found in specified JSON")
	errBytecodeNotFound         = errors.New("bytecode field not found in specified JSON")
	errDeployedBytecodeNotFound = errors.New("deployed bytecode field not found in specified JSON")
)

type contractArtifact struct {
	ABI              []byte // the ABI of the Smart Contract
	Bytecode         []byte // the raw bytecode of the Smart Contract
	DeployedBytecode []byte // the deployed bytecode of the Smart Contract
}

// generateContractArtifact generates contract artifacts based on the
// passed in Smart Contract JSON ABI
func generateContractArtifact(filepath string) (*contractArtifact, error) {
	var (
		artifact *contractArtifact
		err      error
	)

	// Read from the ABI from the JSON file
	jsonFile, err := os.Open(filepath)
	if err != nil {
		return artifact, err
	}

	jsonRaw, err := ioutil.ReadAll(jsonFile)
	if err != nil {
		return artifact, err
	}

	// Fill out the fields in the JSON file
	var jsonResult map[string]interface{}
	if err = json.Unmarshal(jsonRaw, &jsonResult); err != nil {
		return artifact, err
	}

	// Parse the ABI
	abiRaw, ok := jsonResult["abi"]
	if !ok {
		return nil, errABINotFound
	}

	abiBytes, err := json.Marshal(abiRaw)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal ABI to JSON, %w", err)
	}

	// Parse the bytecode
	bytecode, ok := jsonResult["bytecode"].(string)
	if !ok {
		return nil, errBytecodeNotFound
	}

	hexBytecode, err := hex.DecodeString(strings.TrimPrefix(bytecode, "0x"))
	if err != nil {
		return nil, fmt.Errorf("unable to decode bytecode, %w", err)
	}

	// Parse deployed bytecode
	deployedBytecode, ok := jsonResult["deployedBytecode"].(string)
	if !ok {
		return nil, errDeployedBytecodeNotFound
	}

	hexDeployedBytecode, err := hex.DecodeString(strings.TrimPrefix(deployedBytecode, "0x"))
	if err != nil {
		return nil, fmt.Errorf("unable to decode deployed bytecode, %w", err)
	}

	return &contractArtifact{
		ABI:              abiBytes,
		Bytecode:         hexBytecode,
		DeployedBytecode: hexDeployedBytecode,
	}, nil
}

// GenerateGenesisAccountFromFile generates an account that is going to be directly
// inserted into state
func GenerateGenesisAccountFromFile(
	filepath string,
	constructorParams []interface{},
) (*chain.GenesisAccount, error) {
	// Create the artifact from JSON
	artifact, err := generateContractArtifact(filepath)
	if err != nil {
		return nil, err
	}

	// Generate the contract ABI object
	contractABI, err := abi.NewABI(string(artifact.ABI))
	if err != nil {
		return nil, fmt.Errorf("unable to create contract ABI, %w", err)
	}

	// Encode the constructor params
	constructor, err := abi.Encode(
		constructorParams,
		contractABI.Constructor.Inputs,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to encode constructor arguments, %w", err)
	}

	finalBytecode := append(artifact.Bytecode, constructor...)

	// Create an instance of the state
	st := itrie.NewState(itrie.NewMemoryStorage())

	// Create a snapshot
	snapshot := st.NewSnapshot()

	// Create a radix
	radix := state.NewTxn(st, snapshot)

	// Create the contract object for the EVM
	contract := runtime.NewContractCreation(
		1,
		types.ZeroAddress,
		types.ZeroAddress,
		staking.AddrStakingContract,
		big.NewInt(0),
		math.MaxInt64,
		finalBytecode,
	)

	// Enable all forks
	config := chain.ForksInTime{
		Homestead:      true,
		Byzantium:      true,
		Constantinople: true,
		Petersburg:     true,
		Istanbul:       true,
		EIP150:         true,
		EIP158:         true,
		EIP155:         true,
	}

	// Create a transition
	transition := state.NewTransition(config, radix)

	// Run the transition through the EVM
	res := evm.NewEVM().Run(contract, transition, &config)
	if res.Err != nil {
		return nil, fmt.Errorf("EVM predeployment failed, %w", res.Err)
	}

	// After the execution finishes,
	// the state needs to be walked to collect all touched
	// storage slots
	storageMap := make(map[types.Hash]types.Hash)

	radix.GetRadix().Root().Walk(func(k []byte, v interface{}) bool {
		if types.BytesToAddress(k) != staking.AddrStakingContract {
			// Ignore all addresses that are not the one the predeployment
			// is meant to run for
			return false
		}

		obj, _ := v.(*state.StateObject)
		obj.Txn.Root().Walk(func(k []byte, v interface{}) bool {
			val, _ := v.([]byte)
			storageMap[types.BytesToHash(k)] = types.BytesToHash(val)

			return false
		})

		return true
	})

	transition.Commit()

	return &chain.GenesisAccount{
		Balance: transition.GetBalance(staking.AddrStakingContract),
		Nonce:   transition.GetNonce(staking.AddrStakingContract),
		Code:    artifact.DeployedBytecode,
		Storage: storageMap,
	}, nil
}
