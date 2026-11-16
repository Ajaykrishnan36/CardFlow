import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  ArrowDown,
  ArrowUp,
  Lock,
  Plus,
  RotateCcw,
  Save,
  ShieldCheck,
  Trash2,
} from "lucide-react";
import { isApiError } from "@crm/api/client";
import type { ObjectKey, StageSettingItem } from "@crm/api/types";
import { Alert, Badge, Card, CardHeader } from "@crm/components/ui/card";
import { Button } from "@crm/components/ui/button";
import { Input } from "@crm/components/ui/input";
import { Skeleton } from "@crm/components/ui/spinner";
import { ConfirmDialog, PageContainer, PageHeader } from "@crm/components/page";
import { ErrorState } from "@crm/components/states";
import { useDocumentTitle } from "@crm/features/auth/login-pages";
import { NoAccessPage } from "@crm/features/system/pages";
import { listHref, rulesHref, useRecordScope } from "./record-scope";
import { recordKeys } from "./use-object-meta";

/**
 * /setup/:object/stages — a business's own lead statuses or deal stages (D-182): rename,
 * reorder, add, remove (when no record is at it) and, for deals, the win probability of
 * each stage. The closing steps stay; they can be renamed.
 */
export function StagesPage({ object }: { object: ObjectKey }) {
  const scope = useRecordScope();
  if (!scope.canCustomize || (object !== "leads" && object !== "opportunities"))
    return <NoAccessPage />;
  return <StagesView object={object} />;
}

type Row = StageSettingItem & { id: string; isNew?: boolean };

let seq = 0;
const rowId = () => `s${++seq}`;
const keyFrom = (label: string) =>
  label
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .replace(/^([0-9])/, "s_$1")
    .slice(0, 40);

function StagesView({ object }: { object: ObjectKey }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const scope = useRecordScope();
  const deals = object === "opportunities";
  const key = [...recordKeys.meta(scope.prefix, object), "stages"];
  const q = useQuery({
    queryKey: key,
    queryFn: () => scope.api.stages(object),
  });
  useDocumentTitle(
    t(deals ? "records.stages.titleDeals" : "records.stages.titleLeads"),
  );
  const [rows, setRows] = useState<Row[]>([]);
  const [error, setError] = useState("");
  const [resetting, setResetting] = useState(false);

  useEffect(() => {
    if (q.data) setRows(q.data.stages.map((s) => ({ ...s, id: rowId() })));
  }, [q.data]);

  const adopt = (next: { stages: StageSettingItem[] }) => {
    qc.setQueryData(key, next);
    void qc.invalidateQueries({
      queryKey: recordKeys.meta(scope.prefix, object),
    });
    void qc.invalidateQueries({
      queryKey: recordKeys.all(scope.prefix, object),
    });
  };
  const fail = (e: unknown) => {
    const msg = isApiError(e)
      ? (Object.values(e.fieldErrors ?? {})[0] ?? e.message)
      : t("records.stages.saveFailed");
    setError(String(msg));
  };
  const save = useMutation({
    mutationFn: () =>
      scope.api.saveStages(
        object,
        rows.map((r) => ({
          value: r.isNew ? keyFrom(r.label) : r.value,
          label: r.label.trim(),
          probability: deals ? Number(r.probability ?? 0) : undefined,
        })),
      ),
    onSuccess: (next) => {
      adopt(next);
      setError("");
      toast.success(t("records.stages.saved"));
    },
    onError: fail,
  });
  const reset = useMutation({
    mutationFn: () => scope.api.saveStages(object, []),
    onSuccess: (next) => {
      adopt(next);
      setResetting(false);
      setError("");
      toast.success(t("records.stages.resetDone"));
    },
    onError: (e) => {
      setResetting(false);
      fail(e);
    },
  });

  if (q.isError) {
    return (
      <PageContainer>
        <ErrorState
          title={t("records.common.loadError")}
          onRetry={() => void q.refetch()}
        />
      </PageContainer>
    );
  }
  if (!q.data) {
    return (
      <PageContainer>
        <Skeleton className="h-6 w-64" />
        <Skeleton className="mt-4 h-72 w-full" />
      </PageContainer>
    );
  }

  const patch = (id: string, change: Partial<Row>) =>
    setRows((rs) => rs.map((r) => (r.id === id ? { ...r, ...change } : r)));
  const move = (i: number, d: -1 | 1) =>
    setRows((rs) => {
      const j = i + d;
      if (j < 0 || j >= rs.length) return rs;
      const next = [...rs];
      [next[i], next[j]] = [next[j]!, next[i]!];
      return next;
    });
  // New stages go before the closing steps.
  const add = () =>
    setRows((rs) => {
      const at = rs.findIndex((r) => r.fixed);
      const row: Row = {
        id: rowId(),
        value: "",
        label: "",
        probability: deals ? 50 : undefined,
        isNew: true,
        inUse: 0,
      };
      return at < 0 ? [...rs, row] : [...rs.slice(0, at), row, ...rs.slice(at)];
    });
  const dirty =
    JSON.stringify(
      rows.map(({ value, label, probability }) => [value, label, probability]),
    ) !==
    JSON.stringify(
      q.data.stages.map(({ value, label, probability }) => [
        value,
        label,
        probability,
      ]),
    );
  const invalid = rows.some(
    (r) =>
      !r.label.trim() ||
      (deals &&
        !r.fixed &&
        (Number(r.probability) < 0 || Number(r.probability) > 99)),
  );

  return (
    <PageContainer>
      <PageHeader
        title={t(
          deals ? "records.stages.titleDeals" : "records.stages.titleLeads",
        )}
        description={t(
          deals
            ? "records.stages.subtitleDeals"
            : "records.stages.subtitleLeads",
        )}
        crumbs={[
          {
            label: t(
              deals ? "records.stages.crumbDeals" : "records.stages.crumbLeads",
            ),
            to: listHref(scope, object),
          },
          { label: t("records.stages.crumb") },
        ]}
        actions={
          <Button variant="outline" asChild>
            <Link to={rulesHref(scope, object)}>
              <ShieldCheck /> {t("records.common.rulesAndDefaults")}
            </Link>
          </Button>
        }
      />

      {error ? (
        <Alert tone="danger" className="mt-4">
          {error}
        </Alert>
      ) : null}

      <Card className="mt-4 overflow-hidden">
        <CardHeader
          title={t(
            deals ? "records.stages.listDeals" : "records.stages.listLeads",
          )}
          description={
            q.data.custom
              ? t("records.stages.customNote")
              : t("records.stages.standardNote")
          }
          actions={
            <Button
              size="sm"
              variant="outline"
              onClick={add}
              disabled={rows.length >= 30}
            >
              <Plus />{" "}
              {t(deals ? "records.stages.addDeal" : "records.stages.addLead")}
            </Button>
          }
        />
        <ol className="divide-y">
          {rows.map((r, i) => (
            <li
              key={r.id}
              className="flex flex-wrap items-center gap-2 px-4 py-2.5 sm:flex-nowrap"
            >
              <span className="w-6 shrink-0 text-right text-xs tabular-nums text-muted-foreground">
                {i + 1}
              </span>
              <div className="min-w-0 flex-1">
                <Input
                  value={r.label}
                  maxLength={40}
                  onChange={(e) => patch(r.id, { label: e.target.value })}
                  aria-label={t("records.stages.nameLabel", { n: i + 1 })}
                  placeholder={t("records.stages.namePlaceholder")}
                  invalid={!r.label.trim()}
                />
              </div>
              {deals ? (
                <label className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
                  <Input
                    type="number"
                    inputMode="numeric"
                    min={0}
                    max={r.fixed ? 100 : 99}
                    className="w-20 text-right tabular-nums"
                    value={r.probability ?? 0}
                    disabled={r.fixed}
                    onChange={(e) =>
                      patch(r.id, {
                        probability: Math.max(
                          0,
                          Math.min(99, Number(e.target.value) || 0),
                        ),
                      })
                    }
                    aria-label={t("records.stages.probLabel", {
                      name: r.label || i + 1,
                    })}
                  />
                  %
                </label>
              ) : null}
              <div className="flex shrink-0 items-center gap-1">
                <div className="flex w-44 shrink-0 items-center justify-end gap-1">
                  {r.fixed ? (
                    <Badge tone="neutral" title={t("records.stages.fixedHint")}>
                      <Lock className="size-3" aria-hidden />{" "}
                      {t("records.stages.fixed")}
                    </Badge>
                  ) : null}
                  {r.inUse ? (
                    <Badge tone="primary">
                      {t("records.stages.inUse", { count: r.inUse })}
                    </Badge>
                  ) : null}
                </div>
                <Button
                  variant="subtle"
                  size="icon-sm"
                  onClick={() => move(i, -1)}
                  disabled={i === 0}
                  aria-label={t("records.stages.up", { name: r.label })}
                >
                  <ArrowUp />
                </Button>
                <Button
                  variant="subtle"
                  size="icon-sm"
                  onClick={() => move(i, 1)}
                  disabled={i === rows.length - 1}
                  aria-label={t("records.stages.down", { name: r.label })}
                >
                  <ArrowDown />
                </Button>
                <Button
                  variant="subtle"
                  size="icon-sm"
                  onClick={() =>
                    setRows((rs) => rs.filter((x) => x.id !== r.id))
                  }
                  disabled={r.fixed || Boolean(r.inUse)}
                  title={r.inUse ? t("records.stages.inUseHint") : undefined}
                  aria-label={t("records.stages.remove", { name: r.label })}
                >
                  <Trash2 />
                </Button>
              </div>
            </li>
          ))}
        </ol>
        <div className="flex flex-wrap items-center justify-between gap-2 border-t bg-muted/30 px-4 py-3">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setResetting(true)}
            disabled={!q.data.custom || reset.isPending}
          >
            <RotateCcw /> {t("records.stages.reset")}
          </Button>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                q.data &&
                setRows(q.data.stages.map((s) => ({ ...s, id: rowId() })))
              }
              disabled={!dirty}
            >
              {t("records.stages.discard")}
            </Button>
            <Button
              size="sm"
              onClick={() => save.mutate()}
              disabled={!dirty || invalid || save.isPending}
            >
              <Save /> {t("records.stages.save")}
            </Button>
          </div>
        </div>
      </Card>

      <ConfirmDialog
        open={resetting}
        onOpenChange={setResetting}
        title={t("records.stages.resetTitle")}
        body={t("records.stages.resetBody")}
        confirmLabel={t("records.stages.reset")}
        onConfirm={() => reset.mutate()}
        loading={reset.isPending}
      />
    </PageContainer>
  );
}
