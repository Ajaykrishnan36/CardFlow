import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { signInApi } from '@crm/api/endpoints';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { CopyButton } from '@crm/features/tools/tool-utils';

const PROVIDERS = [
  { key: 'google', env: ['CRM_GOOGLE_CLIENT_ID', 'CRM_GOOGLE_CLIENT_SECRET'], console: 'Google Cloud Console → APIs & Services → Credentials → OAuth client (Web)' },
  { key: 'microsoft', env: ['CRM_MICROSOFT_CLIENT_ID', 'CRM_MICROSOFT_CLIENT_SECRET', 'CRM_MICROSOFT_TENANT'], console: 'Microsoft Entra admin center → App registrations → New registration (Web)' },
  { key: 'linkedin', env: ['CRM_LINKEDIN_CLIENT_ID', 'CRM_LINKEDIN_CLIENT_SECRET'], console: 'LinkedIn Developers → My apps → Auth (Sign In with LinkedIn using OpenID Connect)' }
] as const;

/** Google / Microsoft / LinkedIn: whether the server has credentials, and how to add them (D-64, D-65). */
export function SignInProviders() {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: ['auth', 'providers'], queryFn: () => signInApi.providers(), staleTime: 60_000 });
  const origin = window.location.origin;
  return (
    <Card>
      <CardHeader title={t('integrations.providers.title')} description={t('integrations.providers.body')} />
      <ul className="divide-y">
        {PROVIDERS.map((p) => {
          const on = Boolean(q.data?.[p.key]);
          const redirect = `${origin}/api/crm/v1/oauth/${p.key}/callback`;
          return (
            <li key={p.key} className="px-5 py-4">
              <div className="flex flex-wrap items-center gap-2">
                <p className="text-[14px] font-semibold">{t(`integrations.providers.${p.key}`)}</p>
                <Badge tone={on ? 'success' : 'neutral'}>{on ? t('integrations.providers.connected') : t('integrations.providers.notSetUp')}</Badge>
              </div>
              <p className="mt-0.5 text-xs text-muted-foreground">{t(`integrations.providers.${p.key}Use`)}</p>
              {!on ? (
                <div className="mt-3 space-y-2 text-xs text-muted-foreground">
                  <p>{t('integrations.providers.step1', { where: p.console })}</p>
                  <div className="flex flex-wrap items-center gap-2">
                    <span>{t('integrations.providers.step2')}</span>
                    <code className="rounded border bg-muted/40 px-1.5 py-0.5 font-mono text-[11px] text-foreground">{redirect}</code>
                    <CopyButton value={redirect} />
                  </div>
                  <p>
                    {t('integrations.providers.step3')}{' '}
                    {p.env.map((e) => (
                      <code key={e} className="mr-1 rounded border bg-muted/40 px-1.5 py-0.5 font-mono text-[11px] text-foreground">
                        {e}
                      </code>
                    ))}
                  </p>
                </div>
              ) : null}
            </li>
          );
        })}
        <li className="px-5 py-4">
          <p className="text-[14px] font-semibold">{t('integrations.providers.sso')}</p>
          <p className="mt-0.5 text-xs text-muted-foreground">{t('integrations.providers.ssoUse')}</p>
        </li>
      </ul>
    </Card>
  );
}
