package predeploy

import (
	"errors"
	"fmt"
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/contracts/staking"
	"github.com/0xPolygon/polygon-edge/helper/predeployment"
	"github.com/0xPolygon/polygon-edge/types"
	"math/big"
	"os"
	"strings"
)

const (
	chainFlag            = "chain"
	predeployAddressFlag = "predeploy-address"
	artifactsPathFlag    = "artifacts-path"
	constructorArgsPath  = "constructor-args"
)

var (
	errInvalidPredeployAddress  = errors.New("invalid predeploy address provided")
	errAddressTaken             = errors.New("the provided predeploy address is taken")
	errReservedPredeployAddress = errors.New("the provided predeploy address is reserved")
	errInvalidAddress           = errors.New("the provided predeploy address must be >= 0x01100")
)

var (
	predeployAddressMin = "0x0000000000000000000000000000000000001100"
)

var (
	params = &predeployParams{}
)

type predeployParams struct {
	addressRaw         string
	constructorArgsRaw []string
	genesisPath        string

	address         types.Address
	artifactsPath   string
	constructorArgs []interface{}

	genesisConfig *chain.Chain
}

func (p *predeployParams) getRequiredFlags() []string {
	return []string{
		predeployAddressFlag,
		artifactsPathFlag,
	}
}

func (p *predeployParams) initRawParams() error {
	if err := p.initPredeployAddress(); err != nil {
		return err
	}

	if err := p.verifyMinAddress(); err != nil {
		return err
	}

	if err := p.initChain(); err != nil {
		return err
	}

	p.convertConstructorArgs()

	return nil
}

func (p *predeployParams) initPredeployAddress() error {
	if len(p.addressRaw) < 1 {
		return errInvalidPredeployAddress
	}

	address := types.StringToAddress(p.addressRaw)
	if address == staking.AddrStakingContract {
		return errReservedPredeployAddress
	}

	p.address = address

	return nil
}

func (p *predeployParams) convertConstructorArgs() {
	if len(p.constructorArgsRaw) < 1 {
		p.constructorArgs = []interface{}{}

		return
	}

	constructorArgs := make([]interface{}, len(p.constructorArgsRaw))
	for i, v := range p.constructorArgsRaw {
		constructorArgs[i] = v
	}

	p.constructorArgs = constructorArgs
}

func (p *predeployParams) verifyMinAddress() error {
	address, ok := big.NewInt(0).SetString(strings.TrimPrefix(p.address.String(), "0x"), 16)
	if !ok {
		return errors.New("unable to convert hex number")
	}

	addressMin, ok := big.NewInt(0).SetString(strings.TrimPrefix(predeployAddressMin, "0x"), 16)
	if !ok {
		return errors.New("unable to convert hex number")
	}

	if address.Cmp(addressMin) < 0 {
		return errInvalidAddress
	}

	return nil
}

func (p *predeployParams) initChain() error {
	cc, err := chain.Import(p.genesisPath)
	if err != nil {
		return fmt.Errorf(
			"failed to load chain config from %s: %w",
			p.genesisPath,
			err,
		)
	}

	p.genesisConfig = cc

	return nil
}

func (p *predeployParams) updateGenesisConfig() error {
	if p.genesisConfig.Genesis.Alloc[p.address] != nil {
		return errAddressTaken
	}

	predeployAccount, err := predeployment.GenerateGenesisAccountFromFile(
		p.artifactsPath,
		p.constructorArgs,
	)
	if err != nil {
		return err
	}

	p.genesisConfig.Genesis.Alloc[p.address] = predeployAccount

	return nil
}

func (p *predeployParams) overrideGenesisConfig() error {
	// Remove the current genesis configuration from disk
	if err := os.Remove(p.genesisPath); err != nil {
		return err
	}

	// Save the new genesis configuration
	if err := helper.WriteGenesisConfigToDisk(
		p.genesisConfig,
		p.genesisPath,
	); err != nil {
		return err
	}

	return nil
}

func (p *predeployParams) getResult() command.CommandResult {
	return &GenesisPredeployResult{
		Address: p.address.String(),
	}
}
