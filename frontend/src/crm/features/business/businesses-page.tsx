import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  ArrowRight,
  Building2,
  Check,
  Copy,
  LogOut,
  Plus,
  Users,
} from "lucide-react";
import {
  businessApi,
  onboardingApi,
  type BusinessSummary,
} from "@crm/api/endpoints";
import { isApiError } from "@crm/api/client";
import {
  isOwnerSession,
  meKey,
  useMe,
  useSignOut,
  workspaceHomePath,
} from "@crm/auth/session";
import { Button } from "@crm/components/ui/button";
import { Badge, Card, Alert } from "@crm/components/ui/card";
import { Field } from "@crm/components/ui/field";
import { Input } from "@crm/components/ui/input";
import { Checkbox, Select } from "@crm/components/ui/form-controls";
import { Skeleton } from "@crm/components/ui/spinner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@crm/components/ui/menu";
import { ErrorState } from "@crm/components/states";
import { useDocumentTitle } from "@crm/features/auth/login-pages";
import { businessLoginUrl, displayCode } from "@crm/lib/tenant";

export const businessesKey = ["businesses"] as const;

const industries = [
  "Retail",
  "Wholesale & distribution",
  "Automotive",
  "Manufacturing",
  "Agriculture",
  "Transport & logistics",
  "Services",
  "Real estate",
  "Construction",
  "Education",
  "Healthcare",
  "Hospitality",
  "Technology",
  "Finance",
  "Insurance",
  "Beauty & salon",
  "Other",
];

/**
 * /crm/businesses — every business the person belongs to, and "create a business".
 * A business is one CRM: its own leads, contacts, accounts, team, roles and reports.
 * Nothing is shared between two businesses, even for the same person.
 */
export function BusinessesHomePage() {
  const { data: me } = useMe();
  const navigate = useNavigate();
  const signOut = useSignOut();
  const [params, setParams] = useSearchParams();
  const q = useQuery({
    queryKey: businessesKey,
    queryFn: businessApi.list,
    staleTime: 30_000,
  });
  const [creating, setCreating] = useState(false);
  useDocumentTitle("Your businesses");
  // Someone who is already a Super Admin adds businesses; the first-time form is for new people (D-158).
  const superAdmin = (me?.memberships ?? []).some(
    (m) =>
      m.status === "active" &&
      !m.isPlatformWorkspace &&
      m.roleKey === "SUPER_ADMIN",
  );
  const mode = superAdmin || params.get("add") ? "add" : "first";

  // First sign-in with no business: go straight to the form.
  useEffect(() => {
    if (params.get("new") && q.data?.canCreate) setCreating(true);
    else if (q.data && q.data.data.length === 0 && q.data.canCreate)
      setCreating(true);
  }, [params, q.data]);

  if (!me) return null;
  const firstName = me.identity.displayName.split(" ")[0] ?? "";
  const list = q.data?.data ?? [];

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-8 sm:px-6 lg:py-12">
      <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[22px] font-semibold tracking-tight sm:text-2xl">
            Your businesses
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {firstName && firstName !== "New" ? `Hi ${firstName}. ` : ""}Each
            business has its own URL and its own CRM. Records never mix between
            them.
          </p>
        </div>
        <div className="flex gap-2">
          {isOwnerSession(me) ? (
            <Button asChild variant="outline" size="sm">
              <Link to="/crm/owner/dashboard">Owner console</Link>
            </Button>
          ) : null}
          {q.data?.canCreate && list.length > 0 ? (
            <Button size="sm" onClick={() => setCreating(true)}>
              <Plus /> {mode === "add" ? "Add business" : "New business"}
            </Button>
          ) : null}
        </div>
      </div>

      {superAdmin && list.length > 0 ? (
        <Alert tone="info" title="You already have an account" className="mb-4">
          Your team signs in at each business’s own URL below — save them. If
          something is wrong, contact support.
        </Alert>
      ) : null}

      {q.isError ? (
        <Card>
          <ErrorState
            title="Couldn't load your businesses"
            message={isApiError(q.error) ? q.error.message : undefined}
            onRetry={() => void q.refetch()}
          />
        </Card>
      ) : q.isPending ? (
        <div className="space-y-3">
          {[0, 1].map((i) => (
            <Card key={i} className="p-4">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="mt-2 h-3 w-24" />
            </Card>
          ))}
        </div>
      ) : list.length === 0 ? (
        <Card className="p-8 text-center">
          <span className="mx-auto grid size-11 place-items-center rounded-full bg-primary-soft text-primary">
            <Building2 className="size-5" aria-hidden />
          </span>
          <h2 className="mt-3 text-base font-semibold">
            Create your first business
          </h2>
          <p className="mx-auto mt-1 max-w-sm text-sm text-muted-foreground">
            It takes a minute. You become its Super Admin, get its own URL, and
            can add your team afterwards.
          </p>
          {q.data?.canCreate ? (
            <Button className="mt-4" onClick={() => setCreating(true)}>
              <Plus /> Create a business
            </Button>
          ) : (
            <p className="mt-4 text-sm text-muted-foreground">
              Creating a business isn’t open right now. Ask your administrator
              for an invitation.
            </p>
          )}
        </Card>
      ) : (
        <ul className="space-y-3">
          {list.map((b) => (
            <li
              key={b.id}
              className="rounded-xl border bg-card transition-colors hover:border-primary/50"
            >
              <Link
                to={workspaceHomePath(b.code)}
                className="group block rounded-t-xl p-4 hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <div className="flex items-center gap-3">
                  <span className="grid size-10 shrink-0 place-items-center rounded-lg bg-primary-soft text-sm font-semibold text-primary">
                    {b.name.slice(0, 2).toUpperCase()}
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-[15px] font-semibold text-foreground">
                      {b.isPlatform ? "Platform CRM" : b.name}
                    </p>
                    <p className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                      <Badge tone="primary">{b.roleName || "Member"}</Badge>
                      <span className="flex items-center gap-1">
                        <Users className="size-3.5" aria-hidden /> {b.members}{" "}
                        {b.members === 1 ? "person" : "people"}
                      </span>
                      {b.profile?.city ? <span>{b.profile.city}</span> : null}
                    </p>
                  </div>
                  <ArrowRight
                    className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-primary"
                    aria-hidden
                  />
                </div>
              </Link>
              {b.tenantSlug && !b.isPlatform ? (
                <div className="border-t px-4 py-2.5">
                  <BusinessUrl slug={b.tenantSlug} />
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      )}

      {q.data && !q.data.canCreate && list.length > 0 ? (
        <p className="mt-4 text-xs text-muted-foreground">
          You’ve created {q.data.created} of {q.data.limit} businesses this
          account can create.
        </p>
      ) : null}

      <div className="mt-8 flex justify-center gap-2">
        <Button asChild variant="ghost" size="sm">
          <Link to="/crm/me">Profile &amp; security</Link>
        </Button>
        <Button variant="ghost" size="sm" onClick={() => void signOut()}>
          <LogOut /> Sign out
        </Button>
      </div>

      <CreateBusinessDialog
        open={creating}
        mode={mode}
        onOpenChange={(open) => {
          setCreating(open);
          if (!open && (params.get("new") || params.get("add")))
            setParams({}, { replace: true });
        }}
        onCreated={(slug) => navigate(`/${slug}/home`)}
      />
    </div>
  );
}

/** A business's own sign-in URL with its grouped code and a copy button (D-158). */
export function BusinessUrl({
  slug,
  className,
}: {
  slug: string;
  className?: string;
}) {
  const url = businessLoginUrl(slug);
  return (
    <div className={`flex items-center gap-2 ${className ?? ""}`}>
      <div className="min-w-0 flex-1">
        <p
          className="truncate font-mono text-[13px] text-foreground"
          title={url}
        >
          {url}
        </p>
        <p className="text-[11px] text-muted-foreground">
          Business code {displayCode(slug)}
        </p>
      </div>
      <Button
        type="button"
        size="sm"
        variant="outline"
        onClick={() => {
          void navigator.clipboard?.writeText(url);
          toast.success("Business URL copied");
        }}
      >
        <Copy /> Copy
      </Button>
    </div>
  );
}

/** Every business URL of the signed-in person (My Profile). */
export function MyBusinessUrls() {
  const q = useQuery({
    queryKey: businessesKey,
    queryFn: businessApi.list,
    staleTime: 30_000,
  });
  const list = (q.data?.data ?? []).filter(
    (b: BusinessSummary) => !b.isPlatform && b.tenantSlug,
  );
  if (q.isPending) return <Skeleton className="h-12 w-full" />;
  if (list.length === 0)
    return (
      <p className="text-sm text-muted-foreground">
        You don’t belong to a business yet.
      </p>
    );
  return (
    <ul className="divide-y">
      {list.map((b: BusinessSummary) => (
        <li key={b.id} className="py-3 first:pt-0 last:pb-0">
          <p className="mb-1 text-sm font-medium">
            {b.name}{" "}
            <span className="font-normal text-muted-foreground">
              · {b.roleName || "Member"}
            </span>
          </p>
          <BusinessUrl slug={b.tenantSlug} />
        </li>
      ))}
    </ul>
  );
}

const teamSizes = ["Just me", "2-10", "11-50", "51-200", "201-500", "500+"];
const taxIdKinds = [
  { value: "gstin", label: "GSTIN", placeholder: "33ABCDE1234F1Z5", max: 15 },
  { value: "pan", label: "PAN", placeholder: "ABCDE1234F", max: 10 },
  { value: "tan", label: "TAN", placeholder: "ABCD12345E", max: 10 },
] as const;
type TaxKind = (typeof taxIdKinds)[number]["value"];

/**
 * The first-time form (D-158): your name and email, then — below a line — the business:
 * name, team size, industry, phone and email (each "same as mine" by default) and one tax
 * ID: GSTIN, PAN or TAN. The business gets its own generated URL, shown at the end; address,
 * website, WhatsApp and card photos are filled in later in Business profile. "add" (Add
 * business, for someone who already has one) skips the part about you.
 */
function CreateBusinessDialog({
  open,
  mode,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  mode: "first" | "add";
  onOpenChange: (open: boolean) => void;
  onCreated: (tenantSlug: string) => void;
}) {
  const qc = useQueryClient();
  const details = useQuery({
    queryKey: ["me", "details"],
    queryFn: onboardingApi.details,
    enabled: open,
    staleTime: 60_000,
  });
  const [you, setYou] = useState({ displayName: "", personalEmail: "" });
  const [biz, setBiz] = useState({
    name: "",
    teamSize: "",
    industry: "",
    phone: "",
    email: "",
    taxIdType: "gstin" as TaxKind,
    taxId: "",
  });
  const [samePhone, setSamePhone] = useState(true);
  const [sameEmail, setSameEmail] = useState(true);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [alert, setAlert] = useState<string | null>(null);
  const [registered, setRegistered] = useState<Array<{
    name: string;
    slug: string;
  }> | null>(null);
  const [done, setDone] = useState<{ name: string; tenantSlug: string } | null>(
    null,
  );
  const myPhone = details.data?.phone ?? "";
  const adding = mode === "add";

  // Fill in what we already know about the person, once per opening.
  const filled = useRef(false);
  useEffect(() => {
    if (!open) {
      filled.current = false;
      return;
    }
    const d = details.data;
    if (!d || filled.current) return;
    filled.current = true;
    setYou({ displayName: d.displayName, personalEmail: d.personalEmail });
  }, [open, details.data]);

  const create = useMutation({
    mutationFn: () =>
      businessApi.create({
        mode,
        name: biz.name.trim(),
        teamSize: biz.teamSize,
        industry: biz.industry,
        phone: samePhone ? myPhone : biz.phone.trim(),
        email: sameEmail ? you.personalEmail.trim() : biz.email.trim(),
        taxIdType: biz.taxIdType,
        taxId: biz.taxId.trim(),
        displayName: adding ? undefined : you.displayName.trim(),
        personalEmail: adding ? undefined : you.personalEmail.trim(),
      }),
    onSuccess: async (res) => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: businessesKey }),
        qc.invalidateQueries({ queryKey: meKey }),
        qc.invalidateQueries({ queryKey: ["me", "details"] }),
      ]);
      qc.removeQueries({ queryKey: ["capabilities"] });
      setDone({
        name: res.business.name,
        tenantSlug: res.business.tenantSlug || res.business.code,
      });
    },
    onError: (e) => {
      if (isApiError(e) && e.code === "already_registered") {
        const list =
          (e.details?.businesses as
            Array<{ name: string; slug: string }> | undefined) ?? [];
        setRegistered(list);
        setAlert(e.message);
        return;
      }
      if (isApiError(e)) {
        const fe = { ...e.fieldErrors };
        if (fe.gstin && !fe.taxId) fe.taxId = fe.gstin;
        setErrors(fe);
        setAlert(
          Object.keys(fe).length
            ? "Please fix the highlighted fields."
            : e.message,
        );
      } else setAlert("Something went wrong. Please try again.");
    },
  });

  const close = (o: boolean) => {
    if (!o && done) {
      const { tenantSlug } = done;
      setDone(null);
      onOpenChange(false);
      onCreated(tenantSlug);
      return;
    }
    onOpenChange(o);
  };
  const kind =
    taxIdKinds.find((k) => k.value === biz.taxIdType) ?? taxIdKinds[0];

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="top-[3vh] flex max-h-[94vh] max-w-2xl flex-col p-0 sm:top-[5vh] sm:max-h-[90vh]">
        {done ? (
          <div className="p-6">
            <span className="grid size-11 place-items-center rounded-full bg-success/15 text-success">
              <Check className="size-5" aria-hidden />
            </span>
            <DialogTitle className="mt-3 text-lg font-semibold">
              {done.name} is ready
            </DialogTitle>
            <DialogDescription className="mt-1 text-sm text-muted-foreground">
              This is your business’s own URL. Save it and share it with your
              team: only people you add can sign in there. You can always find
              it in My Profile.
            </DialogDescription>
            <div className="mt-4 rounded-lg border bg-muted/40 p-3">
              <BusinessUrl slug={done.tenantSlug} />
            </div>
            <div className="mt-6 flex justify-end">
              <Button onClick={() => close(false)}>
                Open {done.name} <ArrowRight />
              </Button>
            </div>
          </div>
        ) : registered ? (
          <div className="p-6">
            <DialogTitle className="text-lg font-semibold">
              You already have an account
            </DialogTitle>
            <DialogDescription className="mt-1 text-sm text-muted-foreground">
              Sign in at your business URL below, or contact support. To start
              another business, use Add business from inside.
            </DialogDescription>
            <ul className="mt-4 space-y-3">
              {registered.map((b) => (
                <li key={b.slug} className="rounded-lg border p-3">
                  <p className="mb-1 text-sm font-medium">{b.name}</p>
                  <BusinessUrl slug={b.slug} />
                </li>
              ))}
            </ul>
            <div className="mt-6 flex justify-end">
              <Button
                variant="outline"
                onClick={() => {
                  setRegistered(null);
                  setAlert(null);
                  onOpenChange(false);
                }}
              >
                Close
              </Button>
            </div>
          </div>
        ) : (
          <>
            <div className="border-b px-5 py-4 pr-12">
              <DialogTitle className="text-base font-semibold">
                {adding ? "Add a business" : "Set up your business"}
              </DialogTitle>
              <DialogDescription className="mt-1 text-sm text-muted-foreground">
                {adding
                  ? "It gets its own URL, its own team and its own records. You are its Super Admin."
                  : "You become its Super Admin and get its own URL. Only the starred fields are needed."}
              </DialogDescription>
            </div>
            <form
              className="flex min-h-0 flex-1 flex-col"
              noValidate
              onSubmit={(e) => {
                e.preventDefault();
                setErrors({});
                setAlert(null);
                const fe: Record<string, string> = {};
                if (!adding && you.displayName.trim().length < 2)
                  fe.displayName = "Enter your name.";
                if (biz.name.trim().length < 2)
                  fe.name = "Enter your business or brand name.";
                if (!samePhone && !biz.phone.trim())
                  fe.phone =
                    "Enter the business phone, or tick “same as my mobile”.";
                if (!biz.taxId.trim())
                  fe.taxId = "Enter your GSTIN, PAN or TAN.";
                if (Object.keys(fe).length) {
                  setErrors(fe);
                  setAlert("Please fix the highlighted fields.");
                  return;
                }
                create.mutate();
              }}
            >
              <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-5 py-5">
                {alert ? <Alert tone="danger">{alert}</Alert> : null}

                {adding ? null : (
                  <>
                    <div className="grid gap-4 sm:grid-cols-2">
                      <Field label="Your name *" error={errors.displayName}>
                        <Input
                          value={you.displayName}
                          onChange={(e) =>
                            setYou({ ...you, displayName: e.target.value })
                          }
                          placeholder="Ajay Krishnan"
                          maxLength={120}
                          autoComplete="name"
                          autoFocus
                        />
                      </Field>
                      <Field label="Email" error={errors.personalEmail}>
                        <Input
                          type="email"
                          inputMode="email"
                          value={you.personalEmail}
                          onChange={(e) =>
                            setYou({ ...you, personalEmail: e.target.value })
                          }
                          placeholder="you@gmail.com"
                          autoComplete="email"
                        />
                      </Field>
                    </div>
                    <hr className="border-border" />
                  </>
                )}

                <div className="grid gap-4 sm:grid-cols-2">
                  <Field
                    label="Business / brand name *"
                    error={errors.name}
                    className="sm:col-span-2"
                  >
                    <Input
                      value={biz.name}
                      onChange={(e) => setBiz({ ...biz, name: e.target.value })}
                      placeholder="Ajay Traders"
                      maxLength={120}
                      autoFocus={adding}
                    />
                  </Field>
                  <Field label="Size of team" error={errors.teamSize}>
                    <Select
                      value={biz.teamSize}
                      onChange={(e) =>
                        setBiz({ ...biz, teamSize: e.target.value })
                      }
                      placeholder="Choose…"
                      options={teamSizes.map((t) => ({
                        value: t,
                        label: t === "Just me" ? t : `${t} people`,
                      }))}
                    />
                  </Field>
                  <Field label="Industry type" error={errors.industry}>
                    <Select
                      value={biz.industry}
                      onChange={(e) =>
                        setBiz({ ...biz, industry: e.target.value })
                      }
                      placeholder="Choose…"
                      options={industries.map((i) => ({ value: i, label: i }))}
                    />
                  </Field>
                  <div>
                    <Field label="Business phone" error={errors.phone}>
                      <Input
                        value={samePhone ? myPhone : biz.phone}
                        onChange={(e) =>
                          setBiz({ ...biz, phone: e.target.value })
                        }
                        type="tel"
                        inputMode="tel"
                        disabled={samePhone}
                        placeholder="+91 98765 43210"
                      />
                    </Field>
                    <Checkbox
                      className="mt-2"
                      checked={samePhone}
                      onCheckedChange={setSamePhone}
                      label="Same as my mobile"
                    />
                  </div>
                  <div>
                    <Field label="Business email" error={errors.email}>
                      <Input
                        value={sameEmail ? you.personalEmail : biz.email}
                        onChange={(e) =>
                          setBiz({ ...biz, email: e.target.value })
                        }
                        type="email"
                        inputMode="email"
                        disabled={sameEmail}
                        placeholder="sales@yourbusiness.com"
                      />
                    </Field>
                    <Checkbox
                      className="mt-2"
                      checked={sameEmail}
                      onCheckedChange={setSameEmail}
                      label="Same as my email"
                    />
                  </div>
                  <Field
                    label="GSTIN, PAN or TAN *"
                    error={errors.taxId}
                    className="sm:col-span-2"
                    hint="One is enough. Its last 4 characters are part of your business URL."
                  >
                    <div className="flex gap-2">
                      <Select
                        className="w-28 shrink-0"
                        aria-label="Which ID"
                        value={biz.taxIdType}
                        onChange={(e) =>
                          setBiz({
                            ...biz,
                            taxIdType: e.target.value as TaxKind,
                          })
                        }
                        options={taxIdKinds.map((k) => ({
                          value: k.value,
                          label: k.label,
                        }))}
                      />
                      <Input
                        value={biz.taxId}
                        onChange={(e) =>
                          setBiz({
                            ...biz,
                            taxId: e.target.value
                              .toUpperCase()
                              .replace(/\s/g, ""),
                          })
                        }
                        placeholder={kind.placeholder}
                        maxLength={kind.max}
                        className="font-mono"
                        aria-label={kind.label}
                      />
                    </div>
                  </Field>
                </div>
              </div>
              <div className="flex justify-end gap-2 border-t bg-muted/30 px-5 py-3">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => onOpenChange(false)}
                  disabled={create.isPending}
                >
                  Cancel
                </Button>
                <Button type="submit" loading={create.isPending}>
                  {adding ? "Add business" : "Create business"}
                </Button>
              </div>
            </form>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
