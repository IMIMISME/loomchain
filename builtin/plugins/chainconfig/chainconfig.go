package chainconfig

import (
	"github.com/gogo/protobuf/proto"
	loom "github.com/loomnetwork/go-loom"
	cctypes "github.com/loomnetwork/go-loom/builtin/types/chainconfig"
	dpostypes "github.com/loomnetwork/go-loom/builtin/types/dposv2"
	"github.com/loomnetwork/go-loom/plugin"
	contract "github.com/loomnetwork/go-loom/plugin/contractpb"
	"github.com/loomnetwork/go-loom/util"
	"github.com/pkg/errors"
)

type (
	InitRequest          = cctypes.InitRequest
	ListFeaturesRequest  = cctypes.ListFeaturesRequest
	ListFeaturesResponse = cctypes.ListFeaturesResponse

	GetFeatureRequest  = cctypes.GetFeatureRequest
	GetFeatureResponse = cctypes.GetFeatureResponse
	AddFeatureRequest  = cctypes.AddFeatureRequest
	AddFeatureResponse = cctypes.AddFeatureResponse
	Feature            = cctypes.Feature
	FeatureInfo        = cctypes.FeatureInfo
	Config             = cctypes.Config

	UpdateFeatureRequest  = cctypes.UpdateFeatureRequest
	EnableFeatureRequest  = cctypes.EnableFeatureRequest
	EnableFeatureResponse = cctypes.EnableFeatureResponse
)

var (
	// ErrrNotAuthorized indicates that a contract method failed because the caller didn't have
	// the permission to execute that method.
	ErrNotAuthorized = errors.New("[ChainConfig] not authorized")
	// ErrInvalidRequest is a generic error that's returned when something is wrong with the
	// request message, e.g. missing or invalid fields.
	ErrInvalidRequest = errors.New("[ChainConfig] invalid request")
	// ErrOwnerNotSpecified returned if init request does not have owner address
	ErrOwnerNotSpecified = errors.New("[ChainConfig] owner not specified")
	// ErrFeatureFound returned if an owner try to set an existing feature
	ErrFeatureAlreadyExists = errors.New("[ChainConfig] feature already exists")

	configPrefix  = "config-"
	featurePrefix = "feature-"
	ownerRole     = "owner"

	submitKnownFeaturePerm = []byte("submit-known-feature")

	FeaturePending  = cctypes.Feature_PENDING
	FeatureEnabled  = cctypes.Feature_ENABLED
	FeatureDisabled = cctypes.Feature_DISABLED
)

func configKey(addr loom.Address) []byte {
	return util.PrefixKey([]byte(configPrefix), addr.Bytes())
}

func featureKey(featureName string) []byte {
	return util.PrefixKey([]byte(featurePrefix), []byte(featureName))
}

type ChainConfig struct {
}

func (c *ChainConfig) Meta() (plugin.Meta, error) {
	return plugin.Meta{
		Name:    "chainconfig",
		Version: "1.0.0",
	}, nil
}

func (c *ChainConfig) Init(ctx contract.Context, req *InitRequest) error {
	if req.Owner == nil {
		return ErrOwnerNotSpecified
	}
	ownerAddr := loom.UnmarshalAddressPB(req.Owner)
	ctx.GrantPermissionTo(ownerAddr, submitKnownFeaturePerm, ownerRole)
	return nil
}

//TODO: require manual sign by all the validators, its fairly safe, for now we allow one way movement
//worst thing would be a consensus issue

//TODO: first pass only has features, which are a subset of configs
//that are only boolean

// Enable Feature
func (c *ChainConfig) EnableFeature(ctx contract.Context, req *EnableFeatureRequest) error {
	// check if this is a called from validator
	contractAddr, err := ctx.Resolve("dposV2")
	if err != nil {
		return err
	}
	valsreq := &dpostypes.ListValidatorsRequestV2{}
	var resp dpostypes.ListValidatorsResponseV2
	err = contract.StaticCallMethod(ctx, contractAddr, "ListValidators", valsreq, &resp)
	if err != nil {
		return err
	}

	validators := resp.Statistics
	sender := ctx.Message().Sender

	found := false
	for _, v := range validators {
		if sender.Local.Compare(v.Address.Local) == 0 {
			found = true
		}
	}
	if !found {
		return ErrNotAuthorized
	}

	if err := enableFeature(ctx, req.Name, &sender); err != nil {
		return err
	}

	return nil
}

//This method can be called by contract owner only to set known features
func (c *ChainConfig) AddFeature(ctx contract.Context, req *AddFeatureRequest) error {
	if ok, _ := ctx.HasPermission(submitKnownFeaturePerm, []string{ownerRole}); !ok {
		return ErrNotAuthorized
	}

	if found := ctx.Has([]byte(featurePrefix + req.Name)); found {
		return ErrFeatureAlreadyExists
	}

	feature := Feature{
		Name:   req.Name,
		Status: cctypes.Feature_PENDING,
	}

	if err := ctx.Set([]byte(featurePrefix+req.Name), &feature); err != nil {
		return err
	}

	return nil

}

func FeatureList(ctx contract.StaticContext) ([]*FeatureInfo, error) {
	featureRange := ctx.Range([]byte(featurePrefix))
	featureInfos := make([]*FeatureInfo, 0)

	for _, m := range featureRange {
		var feature Feature
		if err := proto.Unmarshal(m.Value, &feature); err != nil {
			return nil, errors.Wrap(err, "unmarshal feature")
		}
		featureInfo, err := getFeatureInfo(ctx, feature.Name)
		if err != nil {
			return nil, err
		}
		featureInfos = append(featureInfos, featureInfo)
	}
	return featureInfos, nil
}

func UpdateFeature(ctx contract.Context, feature *Feature) error {
	if err := ctx.Set([]byte(featurePrefix+feature.Name), feature); err != nil {
		return err
	}
	return nil
}

func (c *ChainConfig) ListFeatures(ctx contract.StaticContext, req *ListFeaturesRequest) (*ListFeaturesResponse, error) {
	featureRange := ctx.Range([]byte(featurePrefix))
	listFeaturesResponse := ListFeaturesResponse{
		FeatureInfos: []*FeatureInfo{},
	}

	for _, m := range featureRange {
		var feature Feature
		if err := proto.Unmarshal(m.Value, &feature); err != nil {
			return nil, errors.Wrap(err, "unmarshal feature")
		}
		featureInfo, err := getFeatureInfo(ctx, feature.Name)
		if err != nil {
			return nil, err
		}
		listFeaturesResponse.FeatureInfos = append(listFeaturesResponse.FeatureInfos, featureInfo)
	}

	return &listFeaturesResponse, nil
}

func (c *ChainConfig) GetFeature(ctx contract.StaticContext, req *GetFeatureRequest) (*GetFeatureResponse, error) {
	featureInfo, err := getFeatureInfo(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	getFeatureResponse := GetFeatureResponse{
		FeatureInfo: featureInfo,
	}
	return &getFeatureResponse, nil
}

func getFeatureInfo(ctx contract.StaticContext, name string) (*FeatureInfo, error) {
	var feature Feature
	if err := ctx.Get([]byte(featurePrefix+name), &feature); err != nil {
		return nil, err
	}

	// Calculate percentage of validators that enable this feature (only for pending feature)
	contractAddr, err := ctx.Resolve("dposV2")
	if err != nil {
		return nil, err
	}
	valsreq := &dpostypes.ListValidatorsRequestV2{}
	var resp dpostypes.ListValidatorsResponseV2
	if err = contract.StaticCallMethod(ctx, contractAddr, "ListValidators", valsreq, &resp); err != nil {
		return nil, err
	}

	validators := resp.Statistics
	validatorsCount := len(validators)
	enabledValidatorsCount := 0
	validatorsHashMap := map[string]bool{}

	for _, v := range validators {
		validatorsHashMap[v.Address.Local.String()] = false
	}
	for _, v := range feature.Validators {
		validatorsHashMap[v.Local.String()] = true
	}
	for _, v := range validatorsHashMap {
		if v {
			enabledValidatorsCount++
		}
	}
	percentage := uint64((enabledValidatorsCount * 100) / validatorsCount)

	featureInfo := &FeatureInfo{
		Feature:    &feature,
		Percentage: percentage,
	}

	return featureInfo, nil
}

func enableFeature(ctx contract.Context, name string, validator *loom.Address) error {
	var feature Feature
	if err := ctx.Get([]byte(featurePrefix+name), &feature); err != nil {
		return err
	}

	found := false
	for _, v := range feature.Validators {
		if validator.Compare(loom.UnmarshalAddressPB(v)) == 0 {
			found = true
		}
	}

	if !found {
		feature.Validators = append(feature.Validators, validator.MarshalPB())
	}

	if err := ctx.Set([]byte(featurePrefix+name), &feature); err != nil {
		return err
	}

	return nil
}

var Contract plugin.Contract = contract.MakePluginContract(&ChainConfig{})
