package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
)

var cliOpenBrowser = openCLILoginBrowser

func runCLILogin(args []string, stdout io.Writer) error {
	set := flag.NewFlagSet("cloud-agentsctl login", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var webEndpoint, controlPlaneEndpoint, caFile, application, profilePath string
	var timeout time.Duration
	set.StringVar(&webEndpoint, "web-endpoint", "", "Admin or User Web HTTPS origin")
	set.StringVar(&controlPlaneEndpoint, "control-plane-endpoint", "", "Control Plane HTTPS endpoint")
	set.StringVar(&caFile, "ca-file", "", "PEM CA bundle")
	set.StringVar(&application, "application", "", "login purpose (admin or user)")
	set.StringVar(&profilePath, "profile", "", "absolute CLI profile path")
	set.DurationVar(&timeout, "timeout", defaultRequestTimeout, "login timeout")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return errors.New("invalid login arguments")
	}
	if profilePath == "" {
		var err error
		profilePath, err = defaultCLIProfilePath()
		if err != nil {
			return err
		}
	}
	if timeout <= 0 || !validCredentialPath(profilePath) || !strictCLIEndpoint(webEndpoint) || !strictCLIEndpoint(controlPlaneEndpoint) ||
		application != "admin" && application != "user" || caFile != "" && !validCredentialPath(caFile) {
		return errors.New("invalid login configuration")
	}
	httpClient, err := newCLIHTTPClient(caFile)
	if err != nil {
		return err
	}
	identityClient, err := api.NewCLIIdentityHTTPClientWithClient(webEndpoint, httpClient)
	if err != nil {
		return errors.New("invalid Web endpoint")
	}
	loopback, err := newCLILoginLoopback()
	if err != nil {
		return err
	}
	defer loopback.Close()
	callback, ok := loopback.listener.Addr().(*net.TCPAddr)
	if !ok || callback.IP.String() != "127.0.0.1" || callback.Port < 1024 || callback.Port > 65535 {
		return errCLILoginCallbackRejected
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	requestID, err := newCLIRequestID()
	if err != nil {
		return err
	}
	login, err := identityClient.StartLogin(ctx, requestID, api.CLIAuthorizationStartRequest{
		Application: api.IdentityApplication(application), CallbackPort: callback.Port,
		State: loopback.state, CodeChallenge: loopback.codeChallenge,
	})
	if err != nil {
		return errors.New("CLI login could not start")
	}
	if err := cliOpenBrowser(ctx, login.VerificationURL); err != nil {
		return err
	}
	code, err := loopback.Await(ctx)
	if err != nil {
		return err
	}
	requestID, err = newCLIRequestID()
	if err != nil {
		return err
	}
	grant, err := identityClient.ExchangeGrant(ctx, requestID, api.CLIGrantExchangeRequest{
		AuthorizationID: login.AuthorizationID, AuthorizationCode: code, CodeVerifier: loopback.codeVerifier,
	})
	if err != nil || string(grant.Application) != application {
		return errors.New("CLI login exchange failed")
	}
	profile := cliProfile{Version: cliProfileVersion, WebEndpoint: webEndpoint, ControlPlaneEndpoint: controlPlaneEndpoint, CAFile: caFile,
		Application: application, CredentialKind: "cliGrant", Credential: grant.Credential}
	if err := chooseInitialCLIContext(ctx, identityClient, httpClient, &profile); err != nil {
		requestID, _ = newCLIRequestID()
		_ = identityClient.RevokeGrant(context.WithoutCancel(ctx), requestID, grant.Credential)
		return err
	}
	if err := writeCLIProfile(profilePath, profile); err != nil {
		requestID, _ = newCLIRequestID()
		_ = identityClient.RevokeGrant(context.WithoutCancel(ctx), requestID, grant.Credential)
		return err
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"application": application, "tenantId": profile.DefaultTenantID, "projectId": profile.DefaultProjectID, "profile": profilePath})
}

func runCLIProfile(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("profile action is required")
	}
	action := args[0]
	set := flag.NewFlagSet("cloud-agentsctl profile "+action, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var profilePath, tenantID, projectID string
	var webEndpoint, controlPlaneEndpoint, caFile, application, credentialFile string
	var timeout time.Duration
	set.StringVar(&profilePath, "profile", "", "absolute CLI profile path")
	set.StringVar(&tenantID, "tenant", "", "default tenant identifier")
	set.StringVar(&projectID, "project", "", "default project identifier")
	set.StringVar(&webEndpoint, "web-endpoint", "", "Admin or User Web HTTPS origin")
	set.StringVar(&controlPlaneEndpoint, "control-plane-endpoint", "", "Control Plane HTTPS endpoint")
	set.StringVar(&caFile, "ca-file", "", "PEM CA bundle")
	set.StringVar(&application, "application", "", "service account purpose (admin or user)")
	set.StringVar(&credentialFile, "credential-file", "", "absolute 0600 service-account credential file")
	set.DurationVar(&timeout, "timeout", defaultRequestTimeout, "request timeout")
	if err := set.Parse(args[1:]); err != nil || set.NArg() != 0 || timeout <= 0 {
		return errors.New("invalid profile arguments")
	}
	if profilePath == "" {
		var err error
		profilePath, err = defaultCLIProfilePath()
		if err != nil {
			return err
		}
	}
	configurationProvided := webEndpoint != "" || controlPlaneEndpoint != "" || caFile != "" || application != "" || credentialFile != ""
	if action == "configure-service-account" {
		if !strictCLIEndpoint(webEndpoint) || !strictCLIEndpoint(controlPlaneEndpoint) ||
			application != "admin" && application != "user" || !validCredentialPath(credentialFile) ||
			!validCLIIdentifier(tenantID) || projectID != "" && !validCLIIdentifier(projectID) ||
			caFile != "" && !validCredentialPath(caFile) {
			return errors.New("invalid service-account profile configuration")
		}
		contents, err := readPrivateCredentialFile(credentialFile)
		if err != nil {
			return errCLIProfile
		}
		credential := strings.TrimSuffix(strings.TrimSuffix(string(contents), "\n"), "\r")
		profile := cliProfile{
			Version: cliProfileVersion, WebEndpoint: webEndpoint, ControlPlaneEndpoint: controlPlaneEndpoint,
			CAFile: caFile, Application: application, CredentialKind: "serviceAccount", Credential: credential,
			DefaultTenantID: tenantID, DefaultProjectID: projectID,
		}
		if !validCLIProfile(profile) {
			return errCLIProfile
		}
		client, err := newProfileCLIClient(profile)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if _, err := issueProfileTenantToken(ctx, client, profile, tenantID, projectID); err != nil {
			return errors.New("service-account credential validation failed")
		}
		if err := writeCLIProfile(profilePath, profile); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]string{"application": application, "tenantId": tenantID, "projectId": projectID, "profile": profilePath})
	}
	profile, err := readCLIProfile(profilePath)
	if err != nil {
		return err
	}
	switch action {
	case "show":
		if tenantID != "" || projectID != "" || configurationProvided {
			return errors.New("profile show does not accept tenant or project")
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"application": profile.Application, "credentialKind": profile.CredentialKind, "tenantId": profile.DefaultTenantID, "projectId": profile.DefaultProjectID, "webEndpoint": profile.WebEndpoint, "controlPlaneEndpoint": profile.ControlPlaneEndpoint})
	case "use":
		if configurationProvided || !validCLIIdentifier(tenantID) || projectID != "" && !validCLIIdentifier(projectID) {
			return errors.New("invalid profile context")
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := validateCLIContext(ctx, profile, tenantID, projectID); err != nil {
			return err
		}
		if err := updateCLIProfileContext(profilePath, tenantID, projectID); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]string{"tenantId": tenantID, "projectId": projectID})
	case "logout":
		if tenantID != "" || projectID != "" || configurationProvided {
			return errors.New("profile logout does not accept tenant or project")
		}
		if profile.CredentialKind == "cliGrant" {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			client, err := newProfileCLIClient(profile)
			if err != nil {
				return err
			}
			requestID, err := newCLIRequestID()
			if err != nil {
				return err
			}
			if err := client.RevokeGrant(ctx, requestID, profile.Credential); err != nil {
				return errors.New("CLI logout failed")
			}
		}
		if err := os.Remove(profilePath); err != nil {
			return errCLIProfile
		}
		return json.NewEncoder(stdout).Encode(map[string]bool{"loggedOut": true})
	default:
		return errors.New("unknown profile action")
	}
}

func chooseInitialCLIContext(ctx context.Context, identityClient *api.CLIIdentityClient, httpClient *http.Client, profile *cliProfile) error {
	requestID, err := newCLIRequestID()
	if err != nil {
		return err
	}
	page, err := identityClient.ListTenants(ctx, requestID, profile.Credential, 200, "")
	if err != nil || len(page.Tenants) == 0 {
		return errors.New("CLI login has no available tenant")
	}
	profile.DefaultTenantID = page.Tenants[0].ID
	token, err := issueProfileTenantToken(ctx, identityClient, *profile, profile.DefaultTenantID, "")
	if err != nil {
		return err
	}
	controlPlane, err := api.NewHTTPClientWithClient(profile.ControlPlaneEndpoint, token.AccessToken, httpClient)
	if err != nil {
		return errors.New("invalid Control Plane endpoint")
	}
	requestID, err = newCLIRequestID()
	if err != nil {
		return err
	}
	projectID, err := discoverInitialCLIProject(ctx, controlPlane, profile.Application, profile.DefaultTenantID, requestID)
	if err != nil {
		return errors.New("CLI project discovery failed")
	}
	profile.DefaultProjectID = projectID
	return nil
}

func discoverInitialCLIProject(ctx context.Context, controlPlane *api.Client, application, tenantID, requestID string) (string, error) {
	if application == "user" {
		projects, err := controlPlane.ListMyProjects(ctx, tenantID, requestID, 1, "")
		if err != nil || len(projects.Value.Projects) == 0 {
			return "", err
		}
		return projects.Value.Projects[0].Metadata.UID, nil
	}
	organizationPageToken := ""
	for {
		organizations, err := controlPlane.ListAdminOrganizations(ctx, tenantID, requestID, 200, organizationPageToken)
		if err != nil {
			return "", err
		}
		for _, organization := range organizations.Value.Organizations {
			projectPageToken := ""
			for {
				projects, projectErr := controlPlane.ListAdminProjects(ctx, tenantID, organization.Metadata.UID, requestID, 1, projectPageToken)
				if projectErr != nil {
					return "", projectErr
				}
				if len(projects.Value.Projects) != 0 {
					return projects.Value.Projects[0].Metadata.UID, nil
				}
				if projects.Value.NextPageToken == "" {
					break
				}
				projectPageToken = projects.Value.NextPageToken
			}
		}
		if organizations.Value.NextPageToken == "" {
			return "", nil
		}
		organizationPageToken = organizations.Value.NextPageToken
	}
}

func validateCLIContext(ctx context.Context, profile cliProfile, tenantID, projectID string) error {
	identityClient, err := newProfileCLIClient(profile)
	if err != nil {
		return err
	}
	if profile.CredentialKind == "serviceAccount" {
		if tenantID != profile.DefaultTenantID {
			return errors.New("service-account tenant is fixed")
		}
		if projectID == "" {
			_, err = issueProfileTenantToken(ctx, identityClient, profile, tenantID, "")
			if err != nil {
				return errors.New("CLI tenant is unavailable")
			}
			return nil
		}
	} else {
		found := false
		pageToken := ""
		for {
			requestID, requestErr := newCLIRequestID()
			if requestErr != nil {
				return requestErr
			}
			page, pageErr := identityClient.ListTenants(ctx, requestID, profile.Credential, 200, pageToken)
			if pageErr != nil {
				return errors.New("CLI tenant discovery failed")
			}
			for _, tenant := range page.Tenants {
				found = found || tenant.ID == tenantID
			}
			if page.NextPageToken == "" || found {
				break
			}
			pageToken = page.NextPageToken
		}
		if !found {
			return errors.New("CLI tenant is unavailable")
		}
		if projectID == "" {
			return nil
		}
	}
	httpClient, err := newCLIHTTPClient(profile.CAFile)
	if err != nil {
		return err
	}
	token, err := issueProfileTenantToken(ctx, identityClient, profile, tenantID, projectID)
	if err != nil {
		return errors.New("CLI project is unavailable")
	}
	controlPlane, err := api.NewHTTPClientWithClient(profile.ControlPlaneEndpoint, token.AccessToken, httpClient)
	if err != nil {
		return errors.New("invalid Control Plane endpoint")
	}
	requestID, err := newCLIRequestID()
	if err != nil {
		return err
	}
	if profile.Application == "admin" {
		_, err = controlPlane.GetAdminProject(ctx, tenantID, projectID, requestID)
	} else {
		_, err = controlPlane.GetProject(ctx, tenantID, projectID, requestID)
	}
	if err != nil {
		return errors.New("CLI project is unavailable")
	}
	return nil
}

func issueProfileTenantToken(ctx context.Context, client *api.CLIIdentityClient, profile cliProfile, tenantID, projectID string) (api.TenantToken, error) {
	requestID, err := newCLIRequestID()
	if err != nil {
		return api.TenantToken{}, err
	}
	request := api.TenantTokenIssueRequest{TenantID: tenantID, ProjectID: projectID}
	switch profile.CredentialKind {
	case "cliGrant":
		return client.IssueTenantToken(ctx, requestID, profile.Credential, request)
	case "serviceAccount":
		return client.IssueAutomationTenantToken(ctx, requestID, profile.Credential, request)
	default:
		return api.TenantToken{}, errCLIProfile
	}
}

func newProfileCLIClient(profile cliProfile) (*api.CLIIdentityClient, error) {
	httpClient, err := newCLIHTTPClient(profile.CAFile)
	if err != nil {
		return nil, err
	}
	client, err := api.NewCLIIdentityHTTPClientWithClient(profile.WebEndpoint, httpClient)
	if err != nil {
		return nil, errors.New("invalid Web endpoint")
	}
	return client, nil
}

func newCLIHTTPClient(caFile string) (*http.Client, error) {
	if caFile == "" {
		return &http.Client{}, nil
	}
	contents, err := os.ReadFile(caFile)
	if err != nil || len(contents) == 0 || len(contents) > maxCAFileBytes {
		return nil, errors.New("cannot read CA file")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(contents) {
		return nil, errors.New("CA file contains no certificates")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport}, nil
}

func writeCLIProfile(path string, profile cliProfile) error {
	contents, err := encodeCLIProfile(profile)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return createPrivateCredentialFile(path, contents)
	} else if err != nil {
		return errCLIProfile
	}
	return replacePrivateCredentialFile(path, contents)
}

func newCLIRequestID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", errors.New("CLI request ID unavailable")
	}
	return "cli-" + hex.EncodeToString(random[:]), nil
}

func loadCLIProfile(path string) (cliProfile, string, error) {
	if path == "" {
		var err error
		path, err = defaultCLIProfilePath()
		if err != nil {
			return cliProfile{}, "", err
		}
	}
	profile, err := readCLIProfile(path)
	return profile, path, err
}
