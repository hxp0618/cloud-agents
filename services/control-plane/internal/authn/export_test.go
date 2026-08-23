package authn

import (
	"testing"
	"time"
)

// TestPrincipalFixture configures the package-external Slice C conformance
// helper. It is compiled only into tests and cannot mint production authority.
type TestPrincipalFixture struct {
	SubjectKind   string
	SubjectIssuer string
	SubjectValue  string
	TenantID      string
	ResourceLevel string
	ResourceID    string
	Permission    string
	ProjectID     string
}

// TestPrincipalHandle exposes only the opaque proof plus lifecycle controls
// needed by cross-package conformance tests.
type TestPrincipalHandle struct {
	Principal  *VerifiedPrincipal
	Now        time.Time
	Invalidate func() error
}

func NewTestVerifiedPrincipal(testingT *testing.T, fixture TestPrincipalFixture) TestPrincipalHandle {
	testingT.Helper()
	context, lineage := contextAndLineage(testingT, nil, nil)
	claims := validClaims()
	if fixture.SubjectIssuer != "" {
		lineage = newTrustLineage()
		if err := lineage.replace(snapshotCandidate{
			issuer: fixture.SubjectIssuer, audience: "https://api.example", generation: 1,
			securityEpoch: 7, notBefore: testNow - 100, expiresAt: testNow + 1000,
			keys: []snapshotKeyCandidate{{
				jwk: jwkFor(testingT, testPrivateKey(testingT), "key-1"), enabled: true,
				notBefore: testNow - 1000, notAfter: testNow + 1000,
			}},
		}); err != nil {
			testingT.Fatalf("construct test issuer lineage: %v", err)
		}
		context.lineage = lineage
		claims["iss"] = fixture.SubjectIssuer
	}
	if fixture.SubjectKind != "" {
		claims[claimSubjectKind] = fixture.SubjectKind
	}
	if fixture.SubjectValue != "" {
		claims["sub"] = fixture.SubjectValue
	}
	if fixture.TenantID != "" {
		context.targetTenantID = fixture.TenantID
		claims[claimTenantID] = fixture.TenantID
	}
	switch fixture.ResourceLevel {
	case "", "tenant":
		context.targetResourceLevel = targetTenant
	case "organization":
		context.targetResourceLevel = targetOrganization
	case "project":
		context.targetResourceLevel = targetProject
	default:
		testingT.Fatalf("unsupported test resource level %q", fixture.ResourceLevel)
	}
	if fixture.ResourceID != "" {
		context.targetResourceID = fixture.ResourceID
	} else {
		context.targetResourceID = context.targetTenantID
	}
	if fixture.Permission != "" {
		context.requiredPermission = fixture.Permission
		claims["scope"] = fixture.Permission
	}
	if fixture.ProjectID != "" {
		claims[claimProjectID] = fixture.ProjectID
	}
	principal, err := verifyAccessToken(context, tokenFor(testingT, testPrivateKey(testingT), validHeader(), claims))
	if err != nil {
		testingT.Fatalf("construct test verified principal: %v", err)
	}
	return TestPrincipalHandle{
		Principal: principal,
		Now:       time.Unix(testNow, 999_999_999),
		Invalidate: func() error {
			lineage.invalidate()
			return nil
		},
	}
}
