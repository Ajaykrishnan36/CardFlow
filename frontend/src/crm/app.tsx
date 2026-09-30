import { lazy, Suspense, useEffect } from 'react';
import { QueryClient, QueryClientProvider, useQueryClient } from '@tanstack/react-query';
import { BrowserRouter, Navigate, Route, Routes, useLocation, useNavigate, useParams } from 'react-router-dom';
import { Toaster } from 'sonner';
import { isApiError, setUnauthorizedHandler } from '@crm/api/client';
import { homeFor, meKey, RedirectIfSignedIn, RequireOwner, RequireSession, useCapabilities, useMe } from '@crm/auth/session';
import { TooltipProvider } from '@crm/components/ui/menu';
import { FullPageLoader } from '@crm/components/states';
import { safeReturnTo } from '@crm/lib/utils';
import { useUI } from '@crm/lib/ui-store';
import { LoginPage, OwnerLoginPage } from '@crm/features/auth/login-pages';
import { ThemeController } from '@crm/features/shell/theme-toggle';
import { AppShell } from '@crm/features/shell/app-shell';
import { WorkflowEditorPage, WorkflowsPage } from './features/tools/workflows-page';
import { CampaignEditorPage, CampaignsPage } from './features/tools/campaigns-page';
import { MailboxesPage } from './features/tools/mailboxes-page';
import { TeamsPage } from './features/tools/teams-page';
import { SsoPage } from './features/tools/sso-page';
import { DeveloperPage } from './features/tools/developer-page';
import { SignupPage } from './features/auth/signup-page';
import { AppsPage } from './features/owner/apps-page';
import { ProductObjectDetailPage, ProductObjectsPage } from './features/objects/product-objects';
import { ComingSoonPage, NotFoundPage } from '@crm/features/system/pages';

// Route-level code splitting (PRD §11 performance): only the sign-in screens ship eagerly.
const MfaVerifyPage = lazy(() => import('@crm/features/auth/mfa-pages').then((m) => ({ default: m.MfaVerifyPage })));
const MfaSetupPage = lazy(() => import('@crm/features/auth/mfa-pages').then((m) => ({ default: m.MfaSetupPage })));
const ForgotPasswordPage = lazy(() => import('@crm/features/auth/password-pages').then((m) => ({ default: m.ForgotPasswordPage })));
const ResetPasswordPage = lazy(() => import('@crm/features/auth/password-pages').then((m) => ({ default: m.ResetPasswordPage })));
const ChangePasswordPage = lazy(() => import('@crm/features/auth/password-pages').then((m) => ({ default: m.ChangePasswordPage })));
const OwnerDashboardPage = lazy(() => import('@crm/features/owner/dashboard-page').then((m) => ({ default: m.OwnerDashboardPage })));
const WorkspaceHomePage = lazy(() => import('@crm/features/workspace/home-page').then((m) => ({ default: m.WorkspaceHomePage })));
const WorkspaceLayout = lazy(() => import('@crm/features/workspace/workspace-layout').then((m) => ({ default: m.WorkspaceLayout })));
const WorkspaceIndexRedirect = lazy(() => import('@crm/features/workspace/workspace-layout').then((m) => ({ default: m.WorkspaceIndexRedirect })));
const WorkspaceAccessPage = lazy(() => import('@crm/features/workspace/admin/access-page').then((m) => ({ default: m.WorkspaceAccessPage })));
const TicketToCase = lazy(() => import('@crm/features/workspace/support/ticket-to-case').then((m) => ({ default: m.TicketToCase })));
const ReportsPage = lazy(() => import('@crm/features/reports/reports-page').then((m) => ({ default: m.ReportsPage })));
const ReportBuilderPage = lazy(() => import('@crm/features/reports/report-builder-page').then((m) => ({ default: m.ReportBuilderPage })));
const DashboardsPage = lazy(() => import('@crm/features/reports/dashboards-page').then((m) => ({ default: m.DashboardsPage })));
const DashboardPage = lazy(() => import('@crm/features/reports/dashboards-page').then((m) => ({ default: m.DashboardPage })));
const ObjectsPage = lazy(() => import('@crm/features/objects/objects-page').then((m) => ({ default: m.ObjectsPage })));
const ObjectDetailPage = lazy(() => import('@crm/features/objects/object-detail-page').then((m) => ({ default: m.ObjectDetailPage })));
const AppUsersPage = lazy(() => import('@crm/features/workspace/app/app-users-page').then((m) => ({ default: m.AppUsersPage })));
const AppUserDetailPage = lazy(() => import('@crm/features/workspace/app/app-user-detail-page').then((m) => ({ default: m.AppUserDetailPage })));
const BusinessesPage = lazy(() => import('@crm/features/workspace/app/businesses-page').then((m) => ({ default: m.BusinessesPage })));
const BusinessDetailPage = lazy(() => import('@crm/features/workspace/app/business-detail-page').then((m) => ({ default: m.BusinessDetailPage })));
const WorkspaceDashboardPage = lazy(() => import('@crm/features/workspace/dashboard-page').then((m) => ({ default: m.WorkspaceDashboardPage })));
const ProfilePage = lazy(() => import('@crm/features/me/profile-page').then((m) => ({ default: m.ProfilePage })));
const AcceptInvitePage = lazy(() => import('@crm/features/auth/accept-invite-page').then((m) => ({ default: m.AcceptInvitePage })));
const ProductsPage = lazy(() => import('@crm/features/products/products-page').then((m) => ({ default: m.ProductsPage })));
const ProductDetailPage = lazy(() => import('@crm/features/products/product-detail-page').then((m) => ({ default: m.ProductDetailPage })));
const WorkspacesPage = lazy(() => import('@crm/features/workspaces/workspaces-page').then((m) => ({ default: m.WorkspacesPage })));
const ProvisionWorkspacePage = lazy(() => import('@crm/features/workspaces/provision-page').then((m) => ({ default: m.ProvisionWorkspacePage })));
const WorkspaceDetailPage = lazy(() => import('@crm/features/workspaces/workspace-detail-page').then((m) => ({ default: m.WorkspaceDetailPage })));
const RecordListPage = lazy(() => import('@crm/features/records/record-list-page').then((m) => ({ default: m.RecordListPage })));
const RecordDetailPage = lazy(() => import('@crm/features/records/record-detail-page').then((m) => ({ default: m.RecordDetailPage })));
const LayoutEditorPage = lazy(() => import('@crm/features/records/layout-editor-page').then((m) => ({ default: m.LayoutEditorPage })));
const UsersPage = lazy(() => import('@crm/features/users/users-page').then((m) => ({ default: m.UsersPage })));
const UserDetailPage = lazy(() => import('@crm/features/users/user-detail-page').then((m) => ({ default: m.UserDetailPage })));
const AuditPage = lazy(() => import('@crm/features/audit/audit-page').then((m) => ({ default: m.AuditPage })));
const IntegrationsPage = lazy(() => import('@crm/features/integrations/integrations-page').then((m) => ({ default: m.IntegrationsPage })));

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      // Don't retry answers (4xx); retry transient failures twice.
      retry: (count, err) => !(isApiError(err) && err.status >= 400 && err.status < 500) && count < 2
    }
  }
});

/** 401 from any authenticated call → drop the session and return here after sign-in (PRD FE-02). */
function UnauthorizedBridge() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  useEffect(() => {
    setUnauthorizedHandler(() => {
      qc.clear();
      qc.setQueryData(meKey, null);
      const returnTo = safeReturnTo(location.pathname + location.search);
      navigate(`/crm/login?expired=1${returnTo ? `&returnTo=${encodeURIComponent(returnTo)}` : ''}`, { replace: true });
    });
    return () => setUnauthorizedHandler(null);
  }, [qc, navigate, location]);
  return null;
}

function RootRedirect() {
  const { data: me, isPending } = useMe();
  if (isPending) return <FullPageLoader />;
  if (!me) return <Navigate to="/crm/login" replace />;
  return <Navigate to={me.next.startsWith('/crm/mfa') || me.next === '/crm/change-password' ? me.next : homeFor(me)} replace />;
}

/** Nav entries whose module isn't built yet land here, labelled from /capabilities. */
function NavComingSoon({ backTo }: { backTo: string }) {
  const { data: me } = useMe();
  const { data: caps } = useCapabilities(Boolean(me));
  const location = useLocation();
  // Most specific match wins, so /crm/home/leads resolves to Leads rather than Home.
  const item = caps?.navigation
    .filter((n) => location.pathname === n.path || location.pathname.startsWith(n.path + '/'))
    .sort((a, b) => b.path.length - a.path.length)[0];
  if (caps && (!item || item.available)) return <NotFoundPage />;
  return <ComingSoonPage name={item?.label ?? '…'} backTo={backTo} />;
}

function AppRoutes() {
  return (
    <Suspense fallback={<FullPageLoader />}>
      <Routes>
        <Route path="/crm" element={<RootRedirect />} />

        <Route element={<RedirectIfSignedIn />}>
          <Route path="/crm/login" element={<LoginPage />} />
          <Route path="/crm/signup" element={<SignupPage />} />
          <Route path="/crm/owner/login" element={<OwnerLoginPage />} />
        </Route>
        <Route path="/crm/forgot-password" element={<ForgotPasswordPage />} />
        <Route path="/crm/reset-password" element={<ResetPasswordPage />} />
        <Route path="/crm/accept-invite" element={<AcceptInvitePage />} />

        <Route element={<RequireSession step />}>
          <Route path="/crm/mfa/verify" element={<MfaVerifyPage />} />
          <Route path="/crm/mfa/setup" element={<MfaSetupPage />} />
          <Route path="/crm/change-password" element={<ChangePasswordPage />} />
        </Route>

        <Route element={<RequireSession />}>
          <Route element={<AppShell />}>
            <Route path="/crm/me" element={<ProfilePage />} />
            <Route path="/crm/home" element={<WorkspaceHomePage />} />
            <Route path="/crm/home/*" element={<WorkspaceHomePage />} />
            {/* Member workspace app: navigation and permissions come from GET /w/{code}/context. */}
            <Route path="/crm/w/:ws" element={<WorkspaceLayout />}>
              <Route index element={<WorkspaceIndexRedirect />} />
              <Route path="home" element={<WorkspaceDashboardPage />} />
              <Route path="settings/access" element={<WorkspaceAccessPage />} />
              {/* App support tickets are Cases now (D-72); old links still work. */}
              <Route path="support" element={<Navigate to="../cases" relative="path" replace />} />
              <Route path="support/:id" element={<TicketToCase />} />
              <Route path="app-users" element={<AppUsersPage />} />
              <Route path="app-users/:id" element={<AppUserDetailPage />} />
              <Route path="businesses" element={<BusinessesPage />} />
              <Route path="businesses/:id" element={<BusinessDetailPage />} />
              <Route path="reports" element={<ReportsPage />} />
              <Route path="reports/new" element={<ReportBuilderPage />} />
              <Route path="reports/:id" element={<ReportBuilderPage />} />
              <Route path="dashboards" element={<DashboardsPage />} />
              <Route path="dashboards/:id" element={<DashboardPage />} />
              <Route path="workflows" element={<WorkflowsPage />} />
              <Route path="workflows/:id" element={<WorkflowEditorPage />} />
              <Route path="campaigns" element={<CampaignsPage />} />
              <Route path="campaigns/:id" element={<CampaignEditorPage />} />
              <Route path="settings/email" element={<MailboxesPage />} />
              <Route path="settings/teams" element={<TeamsPage />} />
              <Route path="settings/sso" element={<SsoPage />} />
              <Route path="settings/developer" element={<DeveloperPage />} />
              <Route path="settings/objects" element={<ProductObjectsPage />} />
              <Route path="settings/objects/:key" element={<ProductObjectDetailPage />} />
              {(['leads', 'accounts', 'contacts'] as const).map((object) => [
                <Route key={`w-${object}`} path={object} element={<RecordListPage key={object} object={object} />} />,
                <Route key={`w-${object}-detail`} path={`${object}/:id`} element={<RecordDetailPage key={object} object={object} />} />,
                <Route key={`w-${object}-layout`} path={`setup/${object}/layout`} element={<LayoutEditorPage key={object} object={object} />} />
              ])}
              {/* Objects defined as data (opportunities, tasks, custom objects…): same pages, keyed by the URL. */}
              <Route path=":object" element={<ObjectRoute page="list" />} />
              <Route path=":object/:id" element={<ObjectRoute page="detail" />} />
              <Route path="setup/:object/layout" element={<ObjectRoute page="layout" />} />
              <Route path="*" element={<NotFoundPage />} />
            </Route>
            <Route element={<RequireOwner />}>
              <Route path="/crm/owner" element={<Navigate to="/crm/owner/dashboard" replace />} />
              <Route path="/crm/owner/dashboard" element={<OwnerDashboardPage />} />
              <Route path="/crm/owner/products" element={<ProductsPage />} />
              <Route path="/crm/owner/products/:id" element={<ProductDetailPage />} />
              <Route path="/crm/owner/apps" element={<AppsPage />} />
              <Route path="/crm/owner/workspaces" element={<WorkspacesPage />} />
              <Route path="/crm/owner/workspaces/new" element={<ProvisionWorkspacePage />} />
              <Route path="/crm/owner/workspaces/:id" element={<WorkspaceDetailPage />} />
              <Route path="/crm/owner/users" element={<UsersPage />} />
              <Route path="/crm/owner/users/:id" element={<UserDetailPage />} />
              {(['leads', 'accounts', 'contacts'] as const).map((object) => [
                <Route key={object} path={`/crm/owner/${object}`} element={<RecordListPage key={object} object={object} />} />,
                <Route key={`${object}-detail`} path={`/crm/owner/${object}/:id`} element={<RecordDetailPage key={object} object={object} />} />,
                <Route key={`${object}-layout`} path={`/crm/owner/setup/${object}/layout`} element={<LayoutEditorPage key={object} object={object} />} />
              ])}
              <Route path="/crm/owner/objects" element={<ObjectsPage />} />
              <Route path="/crm/owner/objects/:key" element={<ObjectDetailPage />} />
              <Route path="/crm/owner/audit" element={<AuditPage />} />
              <Route path="/crm/owner/integrations" element={<IntegrationsPage />} />
              <Route path="/crm/owner/*" element={<NavComingSoon backTo="/crm/owner/dashboard" />} />
            </Route>
          </Route>
        </Route>

        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </Suspense>
  );
}

/** Record pages for an object named in the URL (keyed so switching objects resets state). */
function ObjectRoute({ page }: { page: 'list' | 'detail' | 'layout' }) {
  const { object = '' } = useParams();
  if (!/^[a-z][a-z0-9_]{1,40}$/.test(object)) return <NotFoundPage />;
  if (page === 'list') return <RecordListPage key={object} object={object} />;
  if (page === 'detail') return <RecordDetailPage key={object} object={object} />;
  return <LayoutEditorPage key={object} object={object} />;
}

export function CrmApp() {
  const resolvedDark = useUI((s) => s.theme);
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={250}>
        <BrowserRouter>
          <ThemeController />
          <UnauthorizedBridge />
          <AppRoutes />
        </BrowserRouter>
        <Toaster position="bottom-right" richColors closeButton theme={resolvedDark === 'system' ? 'system' : resolvedDark} />
      </TooltipProvider>
    </QueryClientProvider>
  );
}
