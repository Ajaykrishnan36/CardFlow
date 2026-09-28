import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Monitor, Moon, Sun } from 'lucide-react';
import { Button } from '@crm/components/ui/button';
import { Menu, MenuContent, MenuLabel, MenuRadioGroup, MenuRadioItem, MenuTrigger } from '@crm/components/ui/menu';
import { resolveDark, useUI, type ThemePref } from '@crm/lib/ui-store';

/** Keeps <html class="dark"> and the density class in sync with preferences + OS setting. */
export function ThemeController() {
  const theme = useUI((s) => s.theme);
  const density = useUI((s) => s.density);

  useEffect(() => {
    const apply = () => document.documentElement.classList.toggle('dark', resolveDark(theme));
    apply();
    if (theme !== 'system') return;
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    mq.addEventListener('change', apply);
    return () => mq.removeEventListener('change', apply);
  }, [theme]);

  useEffect(() => {
    document.querySelector('.crm-root')?.classList.toggle('density-compact', density === 'compact');
  }, [density]);

  return null;
}

export function ThemeToggle() {
  const { t } = useTranslation();
  const theme = useUI((s) => s.theme);
  const setTheme = useUI((s) => s.setTheme);
  const Icon = theme === 'dark' ? Moon : theme === 'light' ? Sun : Monitor;
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="subtle" size="icon-sm" aria-label={t('shell.appearance')}>
          <Icon />
        </Button>
      </MenuTrigger>
      <MenuContent align="end" className="min-w-[10rem]">
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
      </MenuContent>
    </Menu>
  );
}
