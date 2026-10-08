# ADR-0033: 内置身份服务、邀请制账号与租户登录（IDENTITY-V1）

- Date: 2026-10-08
- Status: Proposed；P0 文档待用户批准，尚未授权 P1–P5 运行时实施。
- Scope: `IDENTITY-V1`；密码、联合登录、账号、浏览器会话、邀请、租户发现与身份管理。
- Decision owner: hxp0618
- Gate effect: none；不追认已有部署或关闭历史 Gate。

## 背景与取代范围

产品要求用户登录后选择可访问的租户和项目，不再填写 tenant ID、project ID 或 token。Admin Web 与 User Web 使用各自的浏览器会话，账号只由管理员邀请创建。现有 CP 的 `SubjectRef`、资源 RBAC 与 tenant RLS 是必须保留的授权基础。

本提案将内置身份服务加入 [01 产品范围](../cloud-agents-platform/01-product-scope-and-authority.md)，补足原有外部 IdP/provisioning 边界，而不是把外部 email 或 OIDC subject 直接作为新的资源权限。扩展 [ADR-0025](0025-p1-offline-jwt-access-token-verifier-contract.md) 的部署边界：签发、登录、远程 JWKS 刷新和在线撤销校验由外围服务承担；离线密码学内核继续只验签，不发起网络请求。ADR-0025 是生成输入，原文、冻结 profile 与历史证据保持原字节；本提案获批也不允许改写这些输入。

现有配置式 verifier 已有 JWKS 加载和显式 reload 入口；不能将其当作完整的自动轮换、登录或会话撤销。24 小时是 v1 trust snapshot 的有效期上限，不是新的用户 token 寿命。本方案以自动、受信任的刷新替换人工续期流程，不延长失效快照。

## 决策

### 1. 服务职责与请求链路

| 部分 | 唯一职责 |
| --- | --- |
| Browser | 使用应用自己的 HttpOnly 会话 cookie；不接收或存储 CP/IdP access、refresh 或 ID token。显示名字与授权选择项，不让用户粘贴 ID/token。 |
| Admin / User Web server | 扩展现有 `server.mjs` 同源代理；设置/清除自己的 cookie，校验会话、Origin 和应用用途，从请求路径取得 tenant/project，向身份服务申请对应短期 token 后调用 CP。 |
| Identity service | 独立进程，复用现有 Go/数据库基础；拥有账号、登录方式、密码凭据、会话、邀请、邮箱策略、平台管理员绑定及签发记录。签发租户 token，发布 JWKS，提供受认证的会话/账号/token 活性检查。 |
| Control Plane | 继续作为资源权限执行点和 tenant/membership/RoleBinding writer；校验签名、路径、用途、在线撤销状态和当前数据库权限后，在一个 tenant context 内操作。 |

身份服务是 durable login session 的唯一 owner；Web server 只拥有 cookie 交互和可丢弃的服务端 token 缓存，不建立第二套撤销真相。Web server 到身份服务使用显式服务身份；不能仅提交客户端自称的 user ID 或 tenant ID 来换取 token。缓存按 session、应用、tenant、可选 project 和权限版本隔离，每次转发仍检查活性。

租户上下文来自每个请求的规范路径，并与 token 一致，不使用全局可变的“当前租户”重写请求。切换租户重新授权并签发新 token，清理页面旧请求、数据和游标；不同 tab 的请求不会串租户。项目下拉使用当前租户内有权访问的项目，服务端再次校验归属。

“浏览器无 token”指服务凭据不下发；OAuth 回调 code、CSRF proof、邀请/重置的一次性 proof 仍可能短暂到达浏览器，必须限时、单次、no-store 并从地址栏清除，不能兑换为可被前端读取的 bearer token。

### 2. 一个账号，多种登录方式

- 每个账号有不可变内部 ID；CP subject 固定为 `(kind=user, issuer=<身份服务固定 issuer>, value=user-<id>)`。密码、OIDC 和其他 OAuth 登录均映射到同一个账号。邮箱和显示名不参与 subject identity；更换登录方式不改已有绑定。
- 外部登录以 provider 配置/issuer 与 provider subject 的精确组合唯一绑定。不能因 email 相同自动合并账号、转移绑定或创建账号。已有账号须重新认证后主动关联；解绑不能删掉最后一种可用登录方式。
- 无自助注册、无按域名加入。未知外部登录且没有有效邀请时拒绝。邀请关联租户、目标邮箱、角色、邀请人、到期时间及一次性 proof hash；原始链接只生成一次，邮件发送仅在配置 SMTP 时启用。
- 新用户接受邀请时设置密码，或使用与邀请邮箱匹配的已验证外部邮箱。密码流程也须有邮箱归属证明：向该地址可信送达的邀请，或管理员记录的邮箱核验；复制链接本身不意味着任意持有人已验证邮箱。没有 SMTP 不放宽此条件。
- 已有账号加入另一租户时，用该账号登录并匹配其已验证邮箱；邀请不能改写已有密码、重置账号或替他人关联登录。接受动作重新检查邀请有效性、邀请人当前授予权限、邮箱策略和账号状态。

### 3. 角色、租户发现与 RLS

| 身份 | Admin Web 可见租户 | User Web 可见租户 | 管理边界 |
| --- | --- | --- | --- |
| `platform.admin` | 全部租户 | 自己有 active membership 的租户 | 平台级账号/登录提供方配置、租户管理；选中租户后仍按单租户执行。 |
| `tenant.admin` | 当前有效绑定允许管理的租户 | 自己有 active membership 的租户 | 本租户成员、邀请、允许授予的角色和邮箱策略。 |
| 普通成员 | 不可进入管理面 | 自己有 active membership 的租户 | 当前项目与资源权限。 |

`platform.admin` 是独立于任何 tenant 的显式数据库绑定，不是在每个租户伪造一条 membership。其选中租户后的管理权限必须在契约中列出有限的 permission 集，CP 每次重新检查平台绑定；不使用 wildcard、空 tenant、BYPASSRLS 或 token 内一个布尔值获得权限。平台管理员不会因此获得对话、源码、文件或 Secret 内容读取权。

跨租户账号数据位于独立 identity schema，只有范围明确的数据库函数可读写：users、linked logins、password credentials、sessions、invitations、platform admins、tenant email suffixes，以及签发 token 的 `jti` 关联。tenant metadata 及 membership/RoleBinding 仍属于 CP。只对“我的租户”和“平台管理员列出租户”等明确用途开放有限字段、分页与授权检查，不开放任意跨租户查询。

跨租户函数使用独立的无登录 owner、固定 `search_path`、schema-qualified 对象及最小 EXECUTE 授权，撤销 PUBLIC 权限；请求数据库角色不拥有身份表直读或 RLS bypass 权限。所有 tenant-owned 表继续 FORCE RLS、tenant-scoped 外键和事务内 tenant context，缺失/错误 context 拒绝；池连接复用不得残留上次租户。平台管理员业务操作也遵守这一点。

邀请消费、成员创建和角色绑定须原子且幂等：同一个 PostgreSQL 事务内调用身份与 CP 各自拥有的受限写函数，失败全部回滚，不能留下“邀请已耗尽但无成员”或“成员存在但未通过邀请”的中间状态；identity 不成为 membership 表的第二个任意 writer。

租户管理员只能暂停本租户 membership、移除本租户绑定，不能全局禁用一个多租户账号或重置其密码。全局账号禁用与管理员重置归平台管理员；账号本人可重新认证后修改密码。邀请、角色修改和邮箱策略不能授予超过操作者当前范围的权限，也不能通过普通邀请生成 `platform.admin`。安装时一次性建立首个稳定账号和平台管理员绑定，替换固定 subject bootstrap；不得内置默认密码或开放可重复执行的网络 bootstrap。

### 4. 邮箱后缀只限制加入

每个 tenant 可配置多个允许域；空列表允许任意已验证邮箱，同一域可由多个 tenant 配置，无域所有权审批。按规范化后的邮箱 domain 精确相等比较，不做字符串 endsWith、通配或隐式子域匹配；域名规范化和拒绝样例在契约中统一，不能改变 `SubjectRef` 的精确比较规则。

邀请创建与接受/新增 membership 都检查当前列表；策略变化后，旧邀请按新规则重新验证。列表不能赋予 membership/角色，不追溯删除已有成员或否定其登录；管理员需要停用时显式暂停该 membership。平台管理员自身管理访问豁免，不替其邀请的普通成员豁免。

### 5. 短期 token、撤销与密钥刷新

租户 token 默认 15 分钟，由 Web server 自动续签。沿用 v1 RS256、`at+jwt`、精确 issuer/audience/subject、tenant/project、scope/security-epoch/token-profile 与边界校验；scope 从当前数据库权限和应用允许的权限交集派生，不能信任浏览器/IdP 传入的角色或 scope。项目切换若 token 有 project binding 则重新签发；没有 project claim 也不免除项目 RBAC。

沿用分别配置的 Admin/User resource audience；`client_id` 还须绑定具体应用用途。身份服务签发记录将 `jti` 关联 user、session、应用、tenant、可选 project 和 expiry；Admin/User 会话与签发权限相互隔离。CP 保留当前数据库 RBAC 重检，不把签发时权限快照当永久授权。

**离线 JWT 与短 TTL 不能实现立即撤销。** CP 在每次新请求授权前，通过受认证的身份服务检查 token 关联的账号、会话与签发记录仍有效，且与已验签 claims 一致；不缓存允许结果，服务/数据库不可用时 fail closed。禁用用户、撤销会话、密码重置提交之后，新请求与续签立即拒绝，即使原 token 仍未过期。已获准且在执行中的事务不承诺回滚；SSE/WebSocket 等长连接在新授权动作前及最长 15 秒心跳时重检，失败即关闭。

CP 外围刷新器只访问配置中固定 issuer 对应的受信任 HTTPS JWKS 地址；保留 TLS、超时、响应尺寸、key grammar、永久 `kid`→公钥绑定、generation/epoch 和快照时效检查，禁止 token 的 `jku`/`x5u` 等决定地址。定时刷新和受限的未知 `kid` 单次刷新支持轮换；正常轮换保留旧公钥至其已发 token 到期，紧急撤销优先。JWKS 公钥不能单独证明账号/会话活性，也不能凭重取同一文档无限延长无可信有效期的 snapshot。P1 须在既有配置适配器中明确可信 key interval、generation/epoch、撤销与有效期的来源和恢复方式，不修改冻结 v1 语义。

### 6. Web 与第三方登录

Admin/User 分别部署 HTTPS origin，cookie 使用独立 `__Host-` 名称、Secure、HttpOnly、Path=/、无 Domain 和 SameSite；应用用途在服务端强制绑定，不能仅依赖 cookie 名或端口。变更请求严格校验 Origin，外部登录回调走有状态的一次性校验。HTTP-on-LAN 必须在 P2 前提供可信 TLS；不能等到 P5 打包才放宽 Secure cookie。

| 顺序 | 登录提供方 | 必需边界 |
| --- | --- | --- |
| P4.1 | 标准 OIDC | Discovery、authorization code + PKCE、state/nonce、ID token issuer/audience/signature/time 检查；Keycloak 真实 E2E。 |
| P4.2 | GitHub、GitLab | 按各自部署的协议与邮箱 API 映射稳定 subject；标准 OIDC 校验适用于其 OIDC 模式，不能把 OAuth 响应误当 ID token。 |
| P4.3 | 飞书、钉钉、企业微信 | 各自适配 OAuth 和组织身份；默认不把缺失/未验证邮箱视为 verified。 |

登录提供方由平台管理员配置，client secret 只保存 secret reference。每个 provider 的“信任其邮箱”须显式开启，并限定受信任的企业/组织与配置来源；不能把调用者任填邮箱提升为已验证身份。没有合格邮箱时不能接受邀请；已有显式关联的 provider subject 可按账号状态登录。关联/解绑、未知身份拒绝、邀请接受分别验收；不声称这些提供方全部实现标准 OIDC。

会话期限、CSRF、密码存储、限流/锁定、一次性链接和审计的具体规则见 [SECURITY.md](../../../SECURITY.md#identity-v1-proposed)，契约必须可表达这些拒绝原因且不泄露账号是否存在或任何秘密。

## 交付与审批

唯一执行顺序见 [04 的 IDENTITY-V1](../cloud-agents-platform/04-extraction-and-migration.md#identity-v1-plan)，验收见 [05](../cloud-agents-platform/05-gates-and-acceptance.md#identity-v1)，页面与范围路由见 [07](../cloud-agents-platform/07-admin-web-requirements-and-design.md#identity-v1)。P0 只修改文档；用户批准本文及对应规范后才开始 P1，不能从此前 BASE 或 P1 standing approval 推导本次身份范围变更已批准。

契约、数据库和身份核心先行，接着 Web 登录、租户管理、第三方登录、CLI/自动化与部署。密码路径在 P2 先完成，P4 才声称联合登录可用；新 E2E 成功后再删除旧粘贴 token 覆盖。TOTP/WebAuthn、SCIM、会话列表/撤销页面延后，后端会话撤销本期必需。

现有指定部署的登录切换在 P5 另行获得部署与已有数据处理授权；不把私有主机地址写入公共计划，不设计双轨旧登录兼容层。旧部署数据与绑定不自动转换或删除，切换前须核对当前账号/角色归属与备份恢复边界。

## 后果与主要风险

- 登录与资源授权分层，但在线撤销检查使身份服务成为请求可用性依赖；故障时拒绝优先于使用过期授权。
- 平台级数据函数是新增跨租户边界，必须用非 owner 的真实数据库角色验证拒绝、分页、连接复用和权限撤销。
- 可信邮箱策略会影响邀请能否被接受；域限制不能替代身份验证，企业 OAuth 缺失邮箱需真实负例验收。
- HTTPS、签名密钥和会话密钥是登录环境前置条件；文档或 mock 不能替代真实登录、轮换、撤销和 TLS 验收。
