import { api } from './client';
import type {
  AppBusiness,
  AppUser,
  AppUserCounts,
  AppUserDetail,
  AppUserPatch,
  BusinessCounts,
  BusinessPatch,
  RecordPage,
  IntegrationInfo,
  SupportTicket,
  TicketCounts,
  TicketStatus,
  RoleBody,
  WorkspaceAdminMember,
  WorkspaceAdminOptions,
  WorkspaceInviteBody,
  WorkspaceRole,
  AccessCatalog,
  AccessWorkspaceOption,
  AddMembershipBody,
  CreateUserBody,
  CreateUserResult,
  GiveLoginBody,
  GiveLoginResult,
  MembershipAccessBody,
  PermissionSet,
  PermissionSetBody,
  WorkspaceContext,
  WorkspaceDashboard,
  AuditEntry,
  AuthStep,
  Capabilities,
  ConvertBody,
  ConvertResult,
  FieldCreateBody,
  FieldUpdateBody,
  Invitation,
  InvitationPreview,
  InviteBody,
  Layout,
  LookupTarget,
  LookupValue,
  ObjectKey,
  ObjectMeta,
  Page,
  ProductCreateBody,
  ProductDetail,
  ProductSummary,
  ProductUpdateBody,
  ProvisionBody,
  ProvisionResult,
  RecordDetail,
  RecordListParams,
  RecordRow,
  UserDetail,
  UserSummary,
  UserUpdateBody,
  WorkspaceDetail,
  WorkspaceSummary,
  WorkspaceUpdateBody,
  Me,
  MfaConfirmation,
  MfaEnrollment,
  NextStep,
  OwnerDashboard,
  SessionInfo
} from './types';

export interface LoginBody {
  identifier: string;
  password: string;
  audience: 'owner' | 'workspace';
  workspaceCode?: string;
}

export const authApi = {
  login: (body: LoginBody) => api<AuthStep>('/auth/login', { method: 'POST', body }),
  /** Email a 6-digit sign-in code. Always "sent" (never reveals whether the address has an account). */
  requestOtp: (body: { identifier: string; audience: 'owner' | 'workspace' }) =>
    api<{ sent: boolean; expiresIn: number; channel: 'email'; devCode?: string }>('/auth/otp/request', { method: 'POST', body }),
  verifyOtp: (body: { identifier: string; code: string; audience: 'owner' | 'workspace' }) =>
    api<AuthStep>('/auth/otp/verify', { method: 'POST', body }),
  logout: () => api<void>('/auth/logout', { method: 'POST' }),
  logoutAll: () => api<void>('/auth/logout-all', { method: 'POST' }),
  verifyMfa: (code: string) => api<NextStep>('/auth/mfa/verify', { method: 'POST', body: { code } }),
  enrollMfa: () => api<MfaEnrollment>('/auth/mfa/enroll', { method: 'POST', body: {} }),
  confirmMfa: (code: string) => api<MfaConfirmation>('/auth/mfa/confirm', { method: 'POST', body: { code } }),
  forgotPassword: (identifier: string) => api<{ message: string }>('/auth/password/forgot', { method: 'POST', body: { identifier } }),
  resetPassword: (token: string, password: string) =>
    api<{ message: string }>('/auth/password/reset', { method: 'POST', body: { token, password } }),
  changePassword: (currentPassword: string, newPassword: string) =>
    api<NextStep>('/auth/password/change', { method: 'POST', body: { currentPassword, newPassword } })
};

export const meApi = {
  me: () => api<Me>('/me'),
  capabilities: () => api<Capabilities>('/capabilities'),
  sessions: () => api<{ data: SessionInfo[] }>('/me/sessions').then((r) => r.data),
  revokeSession: (id: string) => api<void>(`/me/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' })
};

function qs(params: object = {}): string {
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') sp.set(k, String(v));
  }
  const s = sp.toString();
  return s ? `?${s}` : '';
}

const enc = encodeURIComponent;

export const platformApi = {
  dashboard: () => api<OwnerDashboard>('/platform/dashboard')
};

export const productsApi = {
  list: (params: { q?: string; status?: string } = {}) => api<Page<ProductSummary>>(`/platform/products${qs(params)}`),
  get: (id: string) => api<ProductDetail>(`/platform/products/${enc(id)}`),
  create: (body: ProductCreateBody) => api<ProductDetail>('/platform/products', { method: 'POST', body }),
  update: (id: string, body: ProductUpdateBody) => api<ProductDetail>(`/platform/products/${enc(id)}`, { method: 'PATCH', body }),
  publish: (id: string) => api<ProductDetail>(`/platform/products/${enc(id)}/publish`, { method: 'POST', body: {} }),
  archive: (id: string) => api<ProductDetail>(`/platform/products/${enc(id)}/archive`, { method: 'POST', body: {} }),
  restore: (id: string) => api<ProductDetail>(`/platform/products/${enc(id)}/restore`, { method: 'POST', body: {} })
};

export const workspacesApi = {
  list: (params: { q?: string; status?: string } = {}) => api<Page<WorkspaceSummary>>(`/platform/workspaces${qs(params)}`),
  get: (id: string) => api<WorkspaceDetail>(`/platform/workspaces/${enc(id)}`),
  /** Idempotent (PRD OWN-02): the same key never creates a second workspace. */
  provision: (body: ProvisionBody, idempotencyKey: string) =>
    api<ProvisionResult>('/platform/workspaces', { method: 'POST', body, headers: { 'Idempotency-Key': idempotencyKey } }),
  update: (id: string, body: WorkspaceUpdateBody) => api<WorkspaceDetail>(`/platform/workspaces/${enc(id)}`, { method: 'PATCH', body }),
  assignProduct: (id: string, productId: string) =>
    api<WorkspaceDetail>(`/platform/workspaces/${enc(id)}/products`, { method: 'POST', body: { productId } }),
  updateProduct: (id: string, productId: string, body: { status?: 'active' | 'suspended'; configVersion?: number }) =>
    api<WorkspaceDetail>(`/platform/workspaces/${enc(id)}/products/${enc(productId)}`, { method: 'PATCH', body }),
  invite: (id: string, body: InviteBody) => api<Invitation>(`/platform/workspaces/${enc(id)}/invitations`, { method: 'POST', body }),
  resendInvite: (id: string, invitationId: string) =>
    api<Invitation>(`/platform/workspaces/${enc(id)}/invitations/${enc(invitationId)}/resend`, { method: 'POST', body: {} }),
  revokeInvite: (id: string, invitationId: string) =>
    api<void>(`/platform/workspaces/${enc(id)}/invitations/${enc(invitationId)}/revoke`, { method: 'POST', body: {} })
};

export const invitationsApi = {
  preview: (token: string) => api<InvitationPreview>(`/invitations/preview${qs({ token })}`),
  accept: (body: { token: string; password: string; displayName?: string }) =>
    api<AuthStep>('/invitations/accept', { method: 'POST', body })
};

export const usersApi = {
  list: (params: { q?: string; status?: string; limit?: number; offset?: number } = {}) => api<Page<UserSummary>>(`/platform/users${qs(params)}`),
  get: (id: string) => api<UserDetail>(`/platform/users/${enc(id)}`),
  update: (id: string, body: UserUpdateBody) => api<UserDetail>(`/platform/users/${enc(id)}`, { method: 'PATCH', body }),
  sendPasswordReset: (id: string) =>
    api<{ message: string; devResetUrl?: string }>(`/platform/users/${enc(id)}/password-reset`, { method: 'POST', body: {} }),
  revokeSessions: (id: string) => api<UserDetail>(`/platform/users/${enc(id)}/revoke-sessions`, { method: 'POST', body: {} }),
  resetMfa: (id: string) => api<UserDetail>(`/platform/users/${enc(id)}/reset-mfa`, { method: 'POST', body: {} }),
  updateMembership: (id: string, membershipId: string, body: MembershipAccessBody) =>
    api<UserDetail>(`/platform/users/${enc(id)}/memberships/${enc(membershipId)}`, { method: 'PATCH', body }),
  addMembership: (id: string, body: AddMembershipBody) => api<UserDetail>(`/platform/users/${enc(id)}/memberships`, { method: 'POST', body }),
  /** Owner creates a user directly (temporary password or invitation). */
  create: (body: CreateUserBody) => api<CreateUserResult>('/platform/users', { method: 'POST', body })
};

export const accessApi = {
  catalog: () => api<AccessCatalog>('/platform/access/catalog'),
  /** Every workspace (platform first) with its products and permission sets — feeds the pickers. */
  workspaces: () => api<{ data: AccessWorkspaceOption[] }>('/platform/access/workspaces').then((r) => r.data),
  permissionSets: (workspaceId: string) =>
    api<{ data: PermissionSet[] }>(`/platform/workspaces/${enc(workspaceId)}/permission-sets`).then((r) => r.data),
  createPermissionSet: (workspaceId: string, body: PermissionSetBody) =>
    api<PermissionSet>(`/platform/workspaces/${enc(workspaceId)}/permission-sets`, { method: 'POST', body }),
  updatePermissionSet: (id: string, body: Partial<PermissionSetBody>) =>
    api<PermissionSet>(`/platform/permission-sets/${enc(id)}`, { method: 'PATCH', body }),
  deletePermissionSet: (id: string) => api<void>(`/platform/permission-sets/${enc(id)}`, { method: 'DELETE' }),
  roles: (workspaceId: string) => api<{ data: WorkspaceRole[] }>(`/platform/workspaces/${enc(workspaceId)}/roles`).then((r) => r.data),
  createRole: (workspaceId: string, body: RoleBody) =>
    api<WorkspaceRole>(`/platform/workspaces/${enc(workspaceId)}/roles`, { method: 'POST', body }),
  updateRole: (id: string, body: Partial<RoleBody>) => api<WorkspaceRole>(`/platform/roles/${enc(id)}`, { method: 'PATCH', body }),
  /** Custom roles only; 409 role_in_use while assigned. */
  deleteRole: (id: string) => api<void>(`/platform/roles/${enc(id)}`, { method: 'DELETE' }),
  /** Built-in roles only: back to the default permissions. */
  resetRole: (id: string) => api<WorkspaceRole>(`/platform/roles/${enc(id)}/reset`, { method: 'POST', body: {} }),
  /** Give the person behind a lead / account / contact a login (uses the record's name and email). */
  giveLogin: (object: ObjectKey, id: string, body: GiveLoginBody) =>
    api<GiveLoginResult>(`/platform/crm/${object}/${enc(id)}/login`, { method: 'POST', body })
};

/** The member-facing workspace app at /crm/w/:ws. */
export const workspaceApi = {
  context: (code: string) => api<WorkspaceContext>(`/w/${enc(code)}/context`),
  dashboard: (code: string) => api<WorkspaceDashboard>(`/w/${enc(code)}/dashboard`)
};

/**
 * Record engine client. `prefix` is '/platform' for the owner's Platform CRM or
 * `/w/<code>` for a workspace member (server enforces their permissions).
 */
export function recordsApiFor(prefix: string) {
  return {
  meta: (object: ObjectKey) => api<ObjectMeta>(`${prefix}/crm/meta/${object}`),
  saveLayout: (object: ObjectKey, layout: Layout) => api<ObjectMeta>(`${prefix}/crm/meta/${object}/layout`, { method: 'PUT', body: layout }),
  resetLayout: (object: ObjectKey) => api<ObjectMeta>(`${prefix}/crm/meta/${object}/layout/reset`, { method: 'POST', body: {} }),
  createField: (object: ObjectKey, body: FieldCreateBody) => api<ObjectMeta>(`${prefix}/crm/meta/${object}/fields`, { method: 'POST', body }),
  updateField: (object: ObjectKey, key: string, body: FieldUpdateBody) =>
    api<ObjectMeta>(`${prefix}/crm/meta/${object}/fields/${enc(key)}`, { method: 'PATCH', body }),
  deleteField: (object: ObjectKey, key: string) => api<ObjectMeta>(`${prefix}/crm/meta/${object}/fields/${enc(key)}`, { method: 'DELETE' }),

  list: (object: ObjectKey, params: RecordListParams = {}) => api<RecordPage>(`${prefix}/crm/${object}${qs(params)}`),
  get: (object: ObjectKey, id: string) => api<RecordDetail>(`${prefix}/crm/${object}/${enc(id)}`),
  create: (object: ObjectKey, values: Record<string, unknown>) => api<RecordRow>(`${prefix}/crm/${object}`, { method: 'POST', body: { values } }),
  update: (object: ObjectKey, id: string, values: Record<string, unknown>, expectedVersion: number) =>
    api<RecordRow>(`${prefix}/crm/${object}/${enc(id)}`, { method: 'PATCH', body: { values, expectedVersion } }),
  remove: (object: ObjectKey, id: string) => api<void>(`${prefix}/crm/${object}/${enc(id)}`, { method: 'DELETE' }),
  convertLead: (id: string, body: ConvertBody, idempotencyKey: string) =>
    api<ConvertResult>(`${prefix}/crm/leads/${enc(id)}/convert`, { method: 'POST', body, headers: { 'Idempotency-Key': idempotencyKey } }),
  lookup: (target: LookupTarget, q = '') => api<{ data: LookupValue[] }>(`${prefix}/lookup/${target}${qs({ q })}`).then((r) => r.data)
  };
}

export const recordsApi = recordsApiFor('/platform');
export type RecordsApi = ReturnType<typeof recordsApiFor>;

export const auditApi = {
  list: (params: { limit?: number; before?: number; entityId?: string } = {}) =>
    api<{ data: AuditEntry[]; nextBefore?: number }>(`/platform/audit${qs(params)}`)
};


/**
 * Delegated administration inside a workspace (/crm/w/:ws/settings/access). The server
 * rejects anything beyond the caller's own access with 403 code `exceeds_your_access`.
 */
export function workspaceAdminApi(code: string) {
  const base = `/w/${enc(code)}/admin`;
  return {
    options: () => api<WorkspaceAdminOptions>(`${base}/options`),
    members: () => api<{ data: WorkspaceAdminMember[]; invitations: Invitation[] }>(`${base}/members`),
    invite: (body: WorkspaceInviteBody) =>
      api<{ identityId: string; membershipId: string; invitation?: Invitation; existingLogin?: boolean }>(`${base}/members`, { method: 'POST', body }),
    updateMember: (membershipId: string, body: MembershipAccessBody) =>
      api<WorkspaceAdminMember>(`${base}/members/${enc(membershipId)}`, { method: 'PATCH', body }),
    createRole: (body: RoleBody) => api<WorkspaceRole>(`${base}/roles`, { method: 'POST', body }),
    updateRole: (id: string, body: Partial<RoleBody>) => api<WorkspaceRole>(`${base}/roles/${enc(id)}`, { method: 'PATCH', body }),
    deleteRole: (id: string) => api<void>(`${base}/roles/${enc(id)}`, { method: 'DELETE' }),
    createPermissionSet: (body: PermissionSetBody) => api<PermissionSet>(`${base}/permission-sets`, { method: 'POST', body }),
    updatePermissionSet: (id: string, body: Partial<PermissionSetBody>) =>
      api<PermissionSet>(`${base}/permission-sets/${enc(id)}`, { method: 'PATCH', body }),
    deletePermissionSet: (id: string) => api<void>(`${base}/permission-sets/${enc(id)}`, { method: 'DELETE' })
  };
}
export type WorkspaceAdminApi = ReturnType<typeof workspaceAdminApi>;

/** Support tickets of a workspace connected to an app (members need ticket read / update). */
export function workspaceSupportApi(code: string) {
  const base = `/w/${enc(code)}/support/tickets`;
  return {
    list: (params: { q?: string; status?: TicketStatus | '' } = {}) =>
      api<{ data: SupportTicket[]; counts: TicketCounts }>(`${base}${qs(params)}`),
    get: (id: string) => api<SupportTicket>(`${base}/${enc(id)}`),
    /** Reply and/or change status; the app user sees the reply in the app right away. */
    update: (id: string, body: { status?: TicketStatus; reply?: string }) =>
      api<SupportTicket>(`${base}/${enc(id)}`, { method: 'PATCH', body })
  };
}

/** The connected app's own data (Business Card Snap), edited in place. */
export function workspaceAppApi(code: string) {
  const base = `/w/${enc(code)}/app`;
  return {
    users: (params: { q?: string; filter?: string } = {}) => api<{ data: AppUser[]; counts: AppUserCounts }>(`${base}/users${qs(params)}`),
    user: (id: string) => api<AppUserDetail>(`${base}/users/${enc(id)}`),
    updateUser: (id: string, body: AppUserPatch) => api<AppUserDetail>(`${base}/users/${enc(id)}`, { method: 'PATCH', body }),
    deleteUser: (id: string) => api<void>(`${base}/users/${enc(id)}`, { method: 'DELETE' }),
    businesses: (params: { q?: string; filter?: string } = {}) =>
      api<{ data: AppBusiness[]; counts: BusinessCounts }>(`${base}/businesses${qs(params)}`),
    business: (id: string) => api<AppBusiness>(`${base}/businesses/${enc(id)}`),
    updateBusiness: (id: string, body: BusinessPatch) => api<AppBusiness>(`${base}/businesses/${enc(id)}`, { method: 'PATCH', body }),
    deleteBusiness: (id: string) => api<void>(`${base}/businesses/${enc(id)}`, { method: 'DELETE' }),
    categories: () => api<{ data: Array<{ id: string; name: string }> }>(`${base}/categories`).then((r) => r.data)
  };
}

export const integrationsApi = {
  list: () => api<{ data: IntegrationInfo[] }>('/platform/integrations').then((r) => r.data),
  sync: (key: string) => api<IntegrationInfo>(`/platform/integrations/${enc(key)}/sync`, { method: 'POST', body: {} })
};