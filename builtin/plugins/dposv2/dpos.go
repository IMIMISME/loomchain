package dposv2

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"sort"

	loom "github.com/loomnetwork/go-loom"
	dtypes "github.com/loomnetwork/go-loom/builtin/types/dposv2"
	"github.com/loomnetwork/go-loom/plugin"
	contract "github.com/loomnetwork/go-loom/plugin/contractpb"
	types "github.com/loomnetwork/go-loom/types"
)

const (
	defaultPower               = int64(15)
)

var (
	secondsInYear              = loom.BigUInt{big.NewInt(31536000)}
	basisPoints                = loom.BigUInt{big.NewInt(10000)}
	blockRewardPercentage      = loom.BigUInt{big.NewInt(700)}
	doubleSignSlashPercentage  = loom.BigUInt{big.NewInt(500)}
	inactivitySlashPercentage  = loom.BigUInt{big.NewInt(100)}
	powerCorrection            = big.NewInt(1000000000)
	errCandidateNotRegistered  = errors.New("candidate is not registered")
	errValidatorNotFound       = errors.New("validator not found")
	errDistributionNotFound    = errors.New("distribution not found")
)

type (
	InitRequest                = dtypes.DPOSInitRequestV2
	DelegateRequest            = dtypes.DelegateRequestV2
	UnbondRequest              = dtypes.UnbondRequestV2
	ClaimDistributionRequest   = dtypes.ClaimDistributionRequestV2
	ClaimDistributionResponse  = dtypes.ClaimDistributionResponseV2
	CheckDelegationRequest     = dtypes.CheckDelegationRequestV2
	CheckDelegationResponse    = dtypes.CheckDelegationResponseV2
	RegisterCandidateRequest   = dtypes.RegisterCandidateRequestV2
	UnregisterCandidateRequest = dtypes.UnregisterCandidateRequestV2
	ListCandidateRequest       = dtypes.ListCandidateRequestV2
	ListCandidateResponse      = dtypes.ListCandidateResponseV2
	ListValidatorsRequest      = dtypes.ListValidatorsRequestV2
	ListValidatorsResponse     = dtypes.ListValidatorsResponseV2
	ElectDelegationRequest     = dtypes.ElectDelegationRequestV2
	Candidate                  = dtypes.CandidateV2
	Delegation                 = dtypes.DelegationV2
	Distribution               = dtypes.DistributionV2
	ValidatorStatistic         = dtypes.ValidatorStatisticV2
	Validator                  = types.Validator
	State                      = dtypes.StateV2
	Params                     = dtypes.ParamsV2
)

type DPOS struct {
}

func (c *DPOS) Meta() (plugin.Meta, error) {
	return plugin.Meta{
		Name:    "dposV2",
		Version: "2.0.0",
	}, nil
}

func (c *DPOS) Init(ctx contract.Context, req *InitRequest) error {
	fmt.Fprintf(os.Stderr, "Init DPOS Params %#v\n", req)
	params := req.Params

	if params.CoinContractAddress == nil {
		addr, err := ctx.Resolve("coin")
		if err != nil {
			return err
		}
		params.CoinContractAddress = addr.MarshalPB()
	}

	sortedValidators := sortValidators(req.Validators)
	state := &State{
		Params:           params,
		Validators:       sortedValidators,
		LastElectionTime: ctx.Now().Unix(),
	}

	return saveState(ctx, state)
}

func (c *DPOS) Delegate(ctx contract.Context, req *DelegateRequest) error {
	state, err := loadState(ctx)
	if err != nil {
		return err
	}
	coin := loadCoin(ctx, state.Params)

	delegator := ctx.Message().Sender
	dposContractAddress := ctx.ContractAddress()
	err = coin.TransferFrom(delegator, dposContractAddress, &req.Amount.Value)
	if err != nil {
		return err
	}

	delegations, err := loadDelegationList(ctx)
	if err != nil {
		return err
	}
	priorDelegation := delegations.Get(*req.ValidatorAddress, *delegator.MarshalPB())

	updatedAmount := loom.BigUInt{big.NewInt(0)}
	if priorDelegation != nil {
		updatedAmount.Add(&priorDelegation.Amount.Value, &req.Amount.Value)
	} else {
		updatedAmount = req.Amount.Value
	}

	delegation := &Delegation{
		Validator: req.ValidatorAddress,
		Delegator: delegator.MarshalPB(),
		Amount:    &types.BigUInt{Value: updatedAmount},
		Height:    uint64(ctx.Block().Height),
	}
	delegations.Set(delegation)

	return saveDelegationList(ctx, delegations)
}

func (c *DPOS) Unbond(ctx contract.Context, req *UnbondRequest) error {
	delegations, err := loadDelegationList(ctx)
	if err != nil {
		return err
	}

	state, err := loadState(ctx)
	if err != nil {
		return err
	}
	coin := loadCoin(ctx, state.Params)

	delegator := ctx.Message().Sender

	delegation := delegations.Get(*req.ValidatorAddress, *delegator.MarshalPB())
	if delegation == nil {
		return errors.New(fmt.Sprintf("delegation not found: %s %s", req.ValidatorAddress, delegator.MarshalPB()))
	} else {
		if delegation.Amount.Value.Cmp(&req.Amount.Value) < 0 {
			return errors.New("unbond amount exceeds delegation amount")
		} else {
			err = coin.Transfer(delegator, &req.Amount.Value)
			updatedAmount := loom.BigUInt{big.NewInt(0)}
			updatedAmount.Sub(&delegation.Amount.Value, &req.Amount.Value)
			updatedDelegation := &Delegation{
				Delegator: delegator.MarshalPB(),
				Validator: req.ValidatorAddress,
				Amount:    &types.BigUInt{Value: updatedAmount},
				Height:    uint64(ctx.Block().Height),
			}
			delegations.Set(updatedDelegation)
		}
	}

	return saveDelegationList(ctx, delegations)
}

func (c *DPOS) CheckDelegation(ctx contract.StaticContext, req *CheckDelegationRequest) (*CheckDelegationResponse, error) {
	if req.ValidatorAddress == nil {
		return nil, errors.New("CheckDelegation called with req.ValidatorAddress == nil")
	}
	if req.DelegatorAddress == nil {
		return nil, errors.New("CheckDelegation called with req.DelegatorAddress == nil")
	}

	delegations, err := loadDelegationList(ctx)
	if err != nil {
		return nil, err
	}
	delegation := delegations.Get(*req.ValidatorAddress, *req.DelegatorAddress)
	if delegation == nil {
		return nil, errors.New(fmt.Sprintf("delegation not found: %s %s", req.ValidatorAddress, req.DelegatorAddress))
	} else {
		return &CheckDelegationResponse{Delegation: delegation}, nil
	}
}

func (c *DPOS) RegisterCandidate(ctx contract.Context, req *RegisterCandidateRequest) error {
	candidateAddress := ctx.Message().Sender
	candidates, err := loadCandidateList(ctx)
	if err != nil {
		return err
	}

	checkAddr := loom.LocalAddressFromPublicKey(req.PubKey)
	if candidateAddress.Local.Compare(checkAddr) != 0 {
		return errors.New("public key does not match address")
	}

	// TODO check if candidate exists & is validator, if so trigger automatic
	// undelegation this happens when an elected attempts to update their fee

	// TODO If not, this is a currently unregistered candidate which must make
	// a ~1.25M loom token deposit in order to run for validator.

	newCandidate := &dtypes.CandidateV2{
		PubKey:  req.PubKey,
		Address: candidateAddress.MarshalPB(),
		Fee: req.Fee,
	}
	candidates.Set(newCandidate)
	return saveCandidateList(ctx, candidates)
}

// TODO when UnregisterCandidate is called, all slashing must be applied and
// rewards distributed to delegators
// TODO this is good motivation to abstract this work into a function
func (c *DPOS) UnregisterCandidate(ctx contract.Context, req *dtypes.UnregisterCandidateRequestV2) error {
	candidateAddress := ctx.Message().Sender
	candidates, err := loadCandidateList(ctx)
	if err != nil {
		return err
	}

	cand := candidates.Get(candidateAddress)
	if cand == nil {
		return errCandidateNotRegistered
	}

	candidates.Delete(candidateAddress)
	// TODO return ~1.25M loom token deposit required of all candidates

	return saveCandidateList(ctx, candidates)
}

func (c *DPOS) ListCandidates(ctx contract.StaticContext, req *ListCandidateRequest) (*ListCandidateResponse, error) {
	candidates, err := loadCandidateList(ctx)
	if err != nil {
		return nil, err
	}

	return &ListCandidateResponse{
		Candidates: candidates,
	}, nil
}

func (c *DPOS) ElectByDelegation(ctx contract.Context, req *ElectDelegationRequest) error {
	return Elect(ctx)
}

// electing and settling rewards settlement
func Elect(ctx contract.Context) error {
	state, err := loadState(ctx)
	if err != nil {
		return err
	}
	params := state.Params

	// Check if enough time has elapsed to start new validator election
	if params.ElectionCycleLength < (state.LastElectionTime - ctx.Now().Unix()) {
		return nil
	}

	delegations, err := loadDelegationList(ctx)
	if err != nil {
		return err
	}

	// When there are no token delegations, quit the function early
	// and leave the validators as they are
	// TODO make this delegation
	if len(delegations) == 0 {
		return nil
	}

	candidates, err := loadCandidateList(ctx)
	if err != nil {
		return err
	}
	distributions, err := loadDistributionList(ctx)
	if err != nil {
		return err
	}
	statistics, err := loadValidatorStatisticList(ctx)
	if err != nil {
		return err
	}

	formerValidatorTotals := make(map[string]loom.BigUInt)
	validatorRewards := make(map[string]*loom.BigUInt)
	for _, validator := range state.Validators {

		// get candidate record to lookup fee
		candidate := candidates.GetByPubKey(validator.PubKey)

		if candidate != nil {
			candidateAddress := loom.UnmarshalAddressPB(candidate.Address)
			validatorKey := candidateAddress.String()
			//get validator statistics
			statistic := statistics.Get(candidateAddress)

			if statistic == nil {
				validatorRewards[validatorKey] = &loom.BigUInt{big.NewInt(0)}
				formerValidatorTotals[validatorKey] = loom.BigUInt{big.NewInt(0)}
			} else {
				if statistic.SlashTotal.Value.Cmp(&loom.BigUInt{big.NewInt(0)}) == 0 {
					// if there is no slashing to be applied, reward validator
					cycleSeconds := state.Params.ElectionCycleLength
					reward := calculateDistributionShare(blockRewardPercentage, statistic.DelegationTotal.Value)
					// when election cycle = 0, estimate block time at 2 sec
					if cycleSeconds == 0 {
						cycleSeconds = 2
					}
					reward.Mul(&reward, &loom.BigUInt{big.NewInt(cycleSeconds)})
					reward.Div(&reward, &secondsInYear)
					updatedAmount := loom.BigUInt{big.NewInt(0)}
					updatedAmount.Add(&statistic.DistributionTotal.Value, &reward)
					statistic.DistributionTotal = &types.BigUInt{Value: updatedAmount}
				} else {
					// these delegation totals will be added back up again when we calculate new delegation totals below
					for _, delegation := range delegations {
						// check the it's a delegation that belongs to the validator
						if delegation.Validator.Local.Compare(candidateAddress.Local) == 0 {
							// TODO rename slash total slash percentage
							toSlash := calculateDistributionShare(statistic.SlashTotal.Value, delegation.Amount.Value)
							updatedAmount := loom.BigUInt{big.NewInt(0)}
							updatedAmount.Sub(&delegation.Amount.Value, &toSlash)
							delegation.Amount = &types.BigUInt{Value: updatedAmount}
							// reset slash total
							statistic.SlashTotal = &types.BigUInt{Value: loom.BigUInt{big.NewInt(0)}}
						}
					}
				}

				validatorShare := calculateDistributionShare(loom.BigUInt{big.NewInt(int64(candidate.Fee))}, statistic.DistributionTotal.Value)

				// increase validator's delegation
				distributions.IncreaseDistribution(*candidate.Address, validatorShare)

				// delegatorsShare is the amount to all delegators in proportion
				// to the amount that they've delegatored
				delegatorsShare := validatorShare.Sub(&statistic.DistributionTotal.Value, &validatorShare)
				validatorRewards[validatorKey] = delegatorsShare

				// Zeroing out validator's distribution total since it will be transfered
				// to the distributions storage during this `Elect` call.
				// Validators and Delegators both can claim their rewards in the
				// same way when this is true.
				statistic.DistributionTotal = &types.BigUInt{Value: loom.BigUInt{big.NewInt(0)}}
				formerValidatorTotals[validatorKey] = statistic.DelegationTotal.Value
			}
		}
	}

	// this loop has two goals 1) distribute a validator's rewards to each of
	// the delegators and 2) calculate the new delegation totals
	newDelegationTotals := make(map[string]*loom.BigUInt)
	for _, delegation := range delegations {
		validatorKey := loom.UnmarshalAddressPB(delegation.Validator).String()

		if newDelegationTotals[validatorKey] != nil {
			newDelegationTotals[validatorKey].Add(newDelegationTotals[validatorKey], &delegation.Amount.Value)
		} else {
			newDelegationTotals[validatorKey] = &delegation.Amount.Value
		}

		// allocating validator distributions to delegators
		// based on former validator delegation totals
		delegationTotal := formerValidatorTotals[validatorKey]
		rewardsTotal := validatorRewards[validatorKey]
		if rewardsTotal != nil {
			delegatorDistribution := calculateShare(delegation.Amount.Value, delegationTotal, *rewardsTotal)
			// increase a delegator's distribution
			distributions.IncreaseDistribution(*delegation.Delegator, delegatorDistribution)
		}
	}

	saveDistributionList(ctx, distributions)

	delegationResults := make([]*DelegationResult, 0, len(newDelegationTotals))
	for validator := range newDelegationTotals {
		delegationResults = append(delegationResults, &DelegationResult{
			ValidatorAddress: loom.MustParseAddress(validator),
			DelegationTotal:  *newDelegationTotals[validator],
		})
	}
	sort.Sort(byDelegationTotal(delegationResults))

	// TODO new delegations should probably be integrated at this point

	validatorCount := int(params.ValidatorCount)
	if len(delegationResults) < validatorCount {
		validatorCount = len(delegationResults)
	}

	validators := make([]*Validator, 0)
	for _, res := range delegationResults[:validatorCount] {
		candidate := candidates.Get(res.ValidatorAddress)
		if candidate != nil {
			var power big.Int
			// making sure that the validator power can fit into a int64
			power.Div(res.DelegationTotal.Int, powerCorrection)
			validatorPower := power.Int64()
			delegationTotal := &types.BigUInt{Value: res.DelegationTotal}
			validators = append(validators, &Validator{
				PubKey: candidate.PubKey,
				Power:  validatorPower,
			})
			// TODO abstract into function
			statistic := statistics.Get(loom.UnmarshalAddressPB(candidate.Address))
			if statistic == nil {
				statistics = append(statistics, &ValidatorStatistic{
					Address: res.ValidatorAddress.MarshalPB(),
					PubKey: candidate.PubKey,
					DistributionTotal: &types.BigUInt{Value: loom.BigUInt{big.NewInt(0)}},
					DelegationTotal: delegationTotal,
					SlashTotal: &types.BigUInt{Value: loom.BigUInt{big.NewInt(0)}},
				})
			} else {
				statistic.DelegationTotal = delegationTotal
			}
		}
	}

	saveValidatorStatisticList(ctx, statistics)
	state.Validators = sortValidators(validators)
	saveDelegationList(ctx, delegations)
	state.LastElectionTime = ctx.Now().Unix()
	return saveState(ctx, state)
}

func (c *DPOS) ListValidators(ctx contract.StaticContext, req *ListValidatorsRequest) (*ListValidatorsResponse, error) {
	return ValidatorList(ctx)
}

func ValidatorList(ctx contract.StaticContext) (*ListValidatorsResponse, error) {
	state, err := loadState(ctx)
	if err != nil {
		return nil, err
	}

	return &ListValidatorsResponse{
		Validators: state.Validators,
	}, nil
}

func (c *DPOS) ClaimDistribution(ctx contract.Context, req *ClaimDistributionRequest) (*ClaimDistributionResponse, error) {
	distributions, err := loadDistributionList(ctx)
	if err != nil {
		return nil, err
	}

	delegator := ctx.Message().Sender

	distribution := distributions.Get(*delegator.MarshalPB())
	if distribution == nil {
		return nil, errors.New(fmt.Sprintf("distribution not found: %s", delegator))
	}

	state, err := loadState(ctx)
	if err != nil {
		return nil, err
	}
	coin := loadCoin(ctx, state.Params)

	// send distribution to delegator
	err = coin.Transfer(loom.UnmarshalAddressPB(req.WithdrawalAddress), &distribution.Amount.Value)
	if err != nil {
		return nil, err
	}

	claimedAmount := *distribution.Amount
	resp := &ClaimDistributionResponse{Amount: &claimedAmount}

	err = distributions.ResetTotal(*delegator.MarshalPB())
	if err != nil {
		return nil, err
	}

	err = saveDistributionList(ctx, distributions)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// only called for validators, never delegators
func SlashInactivity(ctx contract.Context, validatorAddr []byte) error {
	return Slash(ctx, validatorAddr, inactivitySlashPercentage)
}

func SlashDoubleSign(ctx contract.Context, validatorAddr []byte) error {
	return Slash(ctx, validatorAddr, doubleSignSlashPercentage)
}

func Slash(ctx contract.Context, validatorAddr []byte, slashPercentage loom.BigUInt) error {
	statistics, err := loadValidatorStatisticList(ctx)
	if err != nil {
		return err
	}
	stat := statistics.GetV2(validatorAddr)
	updatedAmount := loom.BigUInt{big.NewInt(0)}
	updatedAmount.Add(&stat.SlashTotal.Value, &slashPercentage)
	stat.SlashTotal = &types.BigUInt{Value: updatedAmount}
	return saveValidatorStatisticList(ctx, statistics)
}

var Contract plugin.Contract = contract.MakePluginContract(&DPOS{})

// UTILITIES

func loadCoin(ctx contract.Context, params *Params) *ERC20 {
	coinAddr := loom.UnmarshalAddressPB(params.CoinContractAddress)
	return &ERC20{
		Context:         ctx,
		ContractAddress: coinAddr,
	}
}
