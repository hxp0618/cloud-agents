package compatibility

// operationProfile is generated-only immutable metadata. Callers can choose a
// typed service method, but cannot author a profile, SQL function, capability,
// schema binding, or unknown-outcome policy.
type operationProfile struct {
	profileID      string
	profileDigest  string
	operationID    string
	sqlFunction    string
	serviceMethod  string
	mode           string
	capability     string
	unknownOutcome string
}

// Operation is an opaque generated operation selection. It carries no
// database, mutation, HTTP, provider, or external-side-effect authority.
type Operation struct{ profile operationProfile }

func (operation Operation) Valid() bool {
	for _, generated := range generatedOperationProfiles {
		if operation.profile == generated {
			return generated.profileID != "" && generated.profileDigest != "" &&
				generated.operationID != "" && generated.sqlFunction != "" &&
				generated.serviceMethod != "" && generated.capability != "" &&
				(generated.mode == "read_only" && generated.unknownOutcome == "not_applicable" ||
					generated.mode == "mutation" && generated.unknownOutcome == "reconcile_required_no_write_retry")
		}
	}
	return false
}

func (operation Operation) ProfileID() string      { return operation.profile.profileID }
func (operation Operation) ProfileDigest() string  { return operation.profile.profileDigest }
func (operation Operation) OperationID() string    { return operation.profile.operationID }
func (operation Operation) SQLFunction() string    { return operation.profile.sqlFunction }
func (operation Operation) ServiceMethod() string  { return operation.profile.serviceMethod }
func (operation Operation) Mode() string           { return operation.profile.mode }
func (operation Operation) Capability() string     { return operation.profile.capability }
func (operation Operation) UnknownOutcome() string { return operation.profile.unknownOutcome }
func (operation Operation) IsMutation() bool       { return operation.profile.mode == "mutation" }

func allGeneratedOperations() []Operation {
	operations := make([]Operation, len(generatedOperationProfiles))
	for index, profile := range generatedOperationProfiles {
		operations[index] = Operation{profile: profile}
	}
	return operations
}
