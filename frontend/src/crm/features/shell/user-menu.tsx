import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { ChevronsUpDown, LogOut, Monitor, Moon, Rows3, Rows4, Sun, UserRound } from 'lucide-react';
import type { Me } from '@crm/api/types';
import { useSignOut } from '@crm/auth/session';
import {
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuRadioGroup,
  MenuRadioItem,
  MenuSeparator,
  MenuTrigger
} from '@crm/components/ui/menu';
import { cn, initials } from '@crm/lib/utils';
import { useUI, type Density, type ThemePref } from '@crm/lib/ui-store';

export function Avatar({ name, className }: { name: string; className?: string }) {
  return (
    <span
      className={cn(
        'grid size-8 shrink-0 place-items-center rounded-full bg-gradient-to-br from-indigo-400 to-violet-500 text-[12px] font-semibold text-white shadow-sm',
        className
      )}
      aria-hidden
    >
      {initials(name)}
    </span>
  );
}

export function UserMenu({ me, collapsed }: { me: Me; collapsed?: boolean }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const signOut = useSignOut();
  const theme = useUI((s) => s.theme);
  const setTheme = useUI((s) => s.setTheme);
  const density = useUI((s) => s.density);
  const setDensity = useUI((s) => s.setDensity);
  const name = me.identity.displayName;

  return (
    <Menu>
      <MenuTrigger asChild>
        <button
          type="button"
          className={cn(
            'flex items-center gap-2.5 rounded-md p-1.5 text-left transition-colors hover:bg-muted data-[state=open]:bg-muted',
            collapsed ? 'justify-center' : 'w-full'
          )}
          aria-label={t('shell.profile')}
        >
          <Avatar name={name} />
          {!collapsed ? (
            <>
              <span className="min-w-0 flex-1 leading-tight">
                <span className="block truncate text-[13px] font-medium text-foreground">{name}</span>
                <span className="block truncate text-xs text-muted-foreground">{me.identity.email}</span>
              </span>
              <ChevronsUpDown className="size-4 shrink-0 text-muted-foreground" aria-hidden />
            </>
          ) : null}
        </button>
      </MenuTrigger>
      <MenuContent side={collapsed ? 'right' : 'top'} align={collapsed ? 'end' : 'start'} className="w-64">
        <div className="flex items-center gap-2.5 px-2 py-2">
          <Avatar name={name} />
          <div className="min-w-0 leading-tight">
            <p className="truncate text-[13px] font-semibold">{name}</p>
            <p className="truncate text-xs text-muted-foreground">{me.identity.email}</p>
          </div>
        </div>
        <MenuSeparator />
        <MenuItem onSelect={() => navigate('/crm/me')}>
          <UserRound /> {t('shell.profile')}
        </MenuItem>
        <MenuSeparator />
        <MenuLabel>{t('shell.appearance')}</MenuLabel>
        <MenuRadioGroup value={theme} onValueChange={(v) => setTheme(v as ThemePref)}>
          <MenuRadioItem value="light">
            <Sun className="text-muted-foreground" /> {t('shell.light')}
          </MenuRadioItem>
          <MenuRadioItem value="dark">
            <Moon className="text-muted-foreground" /> {t('shell.dark')}
          </MenuRadioItem>
          <MenuRadioItem value="system">
            <Monitor className="text-muted-foreground" /> {t('shell.system')}
          </MenuRadioItem>
        </MenuRadioGroup>
        <MenuLabel>{t('shell.density')}</MenuLabel>
        <MenuRadioGroup value={density} onValueChange={(v) => setDensity(v as Density)}>
          <MenuRadioItem value="comfortable">
            <Rows3 className="text-muted-foreground" /> {t('shell.comfortable')}
          </MenuRadioItem>
          <MenuRadioItem value="compact">
            <Rows4 className="text-muted-foreground" /> {t('shell.compact')}
          </MenuRadioItem>
        </MenuRadioGroup>
        <MenuSeparator />
        <MenuItem danger onSelect={() => void signOut()}>
          <LogOut /> {t('common.signOut')}
        </MenuItem>
      </MenuContent>
    </Menu>
  );
}
