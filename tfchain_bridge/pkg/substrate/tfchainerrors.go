package substrate

// https://github.com/threefoldtech/tfchain_pallets/blob/bc9c5d322463aaf735212e428da4ea32b117dc24/pallet-smart-contract/src/lib.rs#L58
var smartContractModuleErrors = []string{
	"TwinNotExists",
	"NodeNotExists",
	"FarmNotExists",
	"FarmHasNotEnoughPublicIPs",
	"FarmHasNotEnoughPublicIPsFree",
	"FailedToReserveIP",
	"FailedToFreeIPs",
	"ContractNotExists",
	"TwinNotAuthorizedToUpdateContract",
	"TwinNotAuthorizedToCancelContract",
	"NodeNotAuthorizedToDeployContract",
	"NodeNotAuthorizedToComputeReport",
	"PricingPolicyNotExists",
	"ContractIsNotUnique",
	"NameExists",
	"NameNotValid",
}
