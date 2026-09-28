import { forwardRef, useState, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Eye, EyeOff, KeyRound, ArrowBigUp } from 'lucide-react';
import { Input, type InputProps } from './input';

type PasswordInputProps = Omit<InputProps, 'type' | 'trailing'> & { showIcon?: boolean };

/** Password field with show/hide toggle and a Caps Lock warning (desktop typing aid). */
export const PasswordInput = forwardRef<HTMLInputElement, PasswordInputProps>(
  ({ showIcon = true, onKeyDown, onKeyUp, onBlur, ...props }, ref) => {
    const { t } = useTranslation();
    const [visible, setVisible] = useState(false);
    const [capsLock, setCapsLock] = useState(false);

    const detectCaps = (e: KeyboardEvent<HTMLInputElement>) => {
      if (typeof e.getModifierState === 'function') setCapsLock(e.getModifierState('CapsLock'));
    };

    return (
      <div className="space-y-1.5">
        <Input
          ref={ref}
          type={visible ? 'text' : 'password'}
          leading={showIcon ? <KeyRound /> : undefined}
          onKeyDown={(e) => {
            detectCaps(e);
            onKeyDown?.(e);
          }}
          onKeyUp={(e) => {
            detectCaps(e);
            onKeyUp?.(e);
          }}
          onBlur={(e) => {
            setCapsLock(false);
            onBlur?.(e);
          }}
          trailing={
            <button
              type="button"
              onClick={() => setVisible((v) => !v)}
              className="grid size-8 place-items-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              aria-label={visible ? t('common.hidePassword') : t('common.showPassword')}
              aria-pressed={visible}
            >
              {visible ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
            </button>
          }
          {...props}
        />
        {capsLock ? (
          <p className="flex items-center gap-1.5 text-xs font-medium text-warning animate-fade-in" role="status">
            <ArrowBigUp className="size-3.5" aria-hidden />
            {t('common.capsLock')}
          </p>
        ) : null}
      </div>
    );
  }
);
PasswordInput.displayName = 'PasswordInput';

export type Strength = 0 | 1 | 2 | 3 | 4;

/** Heuristic strength score for guidance only; the server enforces the real policy. */
export function passwordStrength(pw: string): Strength {
  if (!pw) return 0;
  let score = 0;
  if (pw.length >= 8) score++;
  if (pw.length >= 12) score++;
  if (/[a-z]/.test(pw) && /[A-Z]/.test(pw)) score++;
  if (/\d/.test(pw) && /[^A-Za-z0-9]/.test(pw)) score++;
  if (pw.length < 8) score = Math.min(score, 1);
  return Math.max(1, Math.min(4, score)) as Strength;
}

export function StrengthMeter({ password }: { password: string }) {
  const { t } = useTranslation();
  const s = passwordStrength(password);
  const labels = ['', t('strength.weak'), t('strength.fair'), t('strength.good'), t('strength.strong')];
  const colors = ['bg-muted', 'bg-danger', 'bg-warning', 'bg-primary', 'bg-success'];
  return (
    <div className="space-y-1" aria-live="polite">
      <div className="flex gap-1" aria-hidden>
        {[1, 2, 3, 4].map((i) => (
          <span key={i} className={`h-1 flex-1 rounded-full transition-colors ${i <= s ? colors[s] : 'bg-muted'}`} />
        ))}
      </div>
      {password ? (
        <p className="text-xs text-muted-foreground">
          {t('strength.label')}: <span className="font-medium text-foreground">{labels[s]}</span>
        </p>
      ) : null}
    </div>
  );
}
