package authn

// verifierErrorCategory is the stable, redacted verifier failure category. The exact
// values are generated profile identity; verifier errors intentionally do not
// retain or unwrap parser, token, signature, or key material.
type verifierErrorCategory string

const (
	errorAudienceMismatch     verifierErrorCategory = "audience_mismatch"
	errorEpochMismatch        verifierErrorCategory = "epoch_mismatch"
	errorInternalFailure      verifierErrorCategory = "internal_failure"
	errorInvalidSignature     verifierErrorCategory = "invalid_signature"
	errorIssuerMismatch       verifierErrorCategory = "issuer_mismatch"
	errorMalformed            verifierErrorCategory = "malformed"
	errorProjectMismatch      verifierErrorCategory = "project_mismatch"
	errorRevokedKey           verifierErrorCategory = "revoked_key"
	errorRevokedToken         verifierErrorCategory = "revoked_token"
	errorScopeMismatch        verifierErrorCategory = "scope_mismatch"
	errorTenantMismatch       verifierErrorCategory = "tenant_mismatch"
	errorTimeInvalid          verifierErrorCategory = "time_invalid"
	errorUnknownKey           verifierErrorCategory = "unknown_key"
	errorUnsupportedAlgorithm verifierErrorCategory = "unsupported_algorithm"
	errorUnsupportedProfile   verifierErrorCategory = "unsupported_profile"
)

// verificationError exposes only a stable category. It has deliberately no
// Unwrap method and no raw cause field.
type verificationError struct {
	category verifierErrorCategory
}

func (e *verificationError) Error() string {
	if e == nil {
		return string(errorInternalFailure)
	}
	return string(e.category)
}

func (e *verificationError) categoryValue() verifierErrorCategory {
	if e == nil {
		return errorInternalFailure
	}
	return e.category
}

func verifierError(category verifierErrorCategory) error {
	return &verificationError{category: category}
}
