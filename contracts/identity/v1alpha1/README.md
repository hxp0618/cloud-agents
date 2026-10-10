# Identity API v1alpha1

This directory is the editable wire authority for the first built-in identity slice.

`GET /.well-known/jwks.json` is public and contains only closed RS256 public JWKs.
`keys` is the active interoperability projection, while `cloudAgentsAuthority`
publishes the exact issuer, bounded validity, monotonic decimal-string revision and
security epoch, and at most 32 immutable-lifetime lineage records. An emergency
revoke-all document has an empty `keys` array and a non-empty disabled lineage.
Private or symmetric key material is forbidden.

The trust manager, rather than the wire codec, requires `keys` to equal the enabled
lineage projection, pins the exact issuer and Admin/User audiences from local
configuration, and enforces key material immutability across revisions. Re-fetching
the same revision cannot extend its validity; a higher revision may jump but must
pass the persisted continuity checks. This contract generates data codecs only and
does not provide an HTTP fetch or refresh manager.

The browser-facing Web proxy mirrors the password login, current session, logout,
current user, and authorized tenant paths on its own origin. Browser clients
use same-origin cookies and never receive an Identity Service or Control Plane bearer
token. These mirrored browser calls are not direct Identity Service authentication.

Every Web-server-to-Identity-Service call uses the Web server's configured service
identity. That authenticated service identity, and only that identity, fixes the
Admin or User application purpose; Origin, cookie contents, and JSON fields cannot
select or elevate it. For password login the Web server also supplies the canonical
single client address in `X-Cloud-Agents-Client-IP`, derived from the accepted socket
or an explicitly configured trusted TLS proxy only after service authentication. It
must strip any browser-supplied forwarded-address headers; the Identity Service does
not use `X-Forwarded-For` and rejects lists, ports, zones, whitespace, or noncanonical
addresses. Login returns the new opaque durable-session handle only in the
private `X-Cloud-Agents-Session` response header. The Identity Service persists only
the handle hash. The Web server writes the opaque value to its own `__Host-` HttpOnly
cookie and must strip the private header from the browser response. Later session,
logout, `me`, tenant discovery, and tenant-token calls read that cookie and send the
handle in `X-Cloud-Agents-Session` together with service authentication. The handle
never appears in a JSON response.

Tenant discovery is a deterministic cursor page with at most 200 tenants. Login and
session responses contain only the first page and may include `nextPageToken`;
`GET /v1/identity/me/tenants` continues it with `pageSize` and the opaque token.
Cursors are bound to the durable session and application, and every page rechecks
current authorization. A super admin reaches every tenant through continuation,
without an unbounded response. Project discovery is not duplicated here: after a
tenant is selected, the Web server uses its fresh tenant token with the existing
paginated Control Plane `my-projects` projection for User sessions and the Admin project list for Admin sessions. Tenant and project projections remain display
and selection data, not authorization authority.

`POST /v1/identity/tenant-token` and `POST /v1/identity/token-status` are server-only.
They require configured Web/Control Plane service authentication and are never
authorized by a browser cookie alone. Tenant-token issuance also requires the opaque
durable-session handle supplied by the Web server. Its request contains no role or
scope input; the service derives scopes through the Control Plane-owned authorization
function. Token status receives only the SHA-256 digest of the exact token plus the
expected application and tenant/project context, so the caller does not disclose or
replace the verified principal.

`POST /v1/identity/authorize-tenant-token` is the separate Identity Service to
Control Plane authorization boundary. It uses a dedicated service credential and
accepts only application, the canonical SHA-256 digest of the durable session,
tenant, and optional project. The Control Plane re-reads the session, stable user
subject, active account and current bindings; the caller cannot provide a user or
scope. Its response binds the same application/tenant/project to the stable user ID,
issuer, and one to 64 unique, strictly sorted scopes. This operation is absent from
the browser client.

`GET` and `PUT /v1/identity/tenants/{tenantId}/email-policy` are Admin-purpose
operations. They require the private durable-session handle, and updates also
require that session's CSRF proof. The Identity Service rechecks current
platform-admin or selected-tenant tenant-admin authority. Allowed domains are
unique sorted canonical IDNA A-labels matched exactly against verified email
domains; an empty list permits any verified email. The policy grants no authority
and does not remove existing members.

Admin-purpose invitation collection endpoints list bounded pages and create a
single role/scope invitation. Creation and revocation require CSRF and current
tenant-administrator authority. The create response returns the random
`invitationCode` once; list, audit and later reads never return it or its digest.
The UI delivers it in a same-origin acceptance link fragment, so it is not sent
in request URLs or Referer headers. `scopeId` is required at every scope level;
for tenant scope it must equal the selected tenant.

`POST /v1/identity/invitations/accept` requires a Web service identity. A new
account supplies the code, password and display name and can use only an
`admin-attested` invitation. An existing account instead supplies its private
session header and CSRF, with neither password nor display name; its verified
email must exactly match. Acceptance creates no implicit login session, never
resets an existing password, and consumes the invitation atomically with the
CP-owned membership and role binding. The code expires after 24 hours; repeat
acceptance rejects. Revoking a pending invitation is idempotent without duplicate
audit; accepted and expired invitations reject revocation. `provider-required`
invitations remain unavailable to password acceptance pending the provider flow.

Global account listing, disable, reset issuance, and global identity-audit listing
require a live Admin-purpose platform-administrator session. Tenant account and
identity-audit pages instead recheck selected-tenant administrator authority and
return only that tenant's membership-backed accounts or tenant-tagged identity
events. Account disable revokes all sessions immediately after commit and cannot
remove the last active platform administrator.

Self-service password change requires the current password and CSRF proof. A
successful change revokes every session, including the caller. Administrator reset
codes are returned once, stored only as hashes, expire after 30 minutes, and create
no session when accepted. Reset acceptance uses the same trusted canonical client
address boundary as password login, with distributed IP and per-proof attempt
limits. Audit pages expose actor, target, scope and decision metadata without
passwords, session handles, reset codes, or their hashes.

Identity linking and provider configuration are deferred to subsequent
implementation slices.
