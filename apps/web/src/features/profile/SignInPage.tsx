import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ArrowLeft, Mail } from "lucide-react";
import { ApiError, api } from "../../lib/api";
import { useT } from "../../lib/i18n";
import { Button, ICON, IconButton } from "../../components/ui";
import { Panel } from "../../components/ui/Sheet";

export default function SignInPage() {
  const t = useT();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const redirect = params.get("redirect") ?? "/me";
  const [email, setEmail] = useState("");
  const providers = useQuery({ queryKey: ["providers"], queryFn: api.providers, staleTime: Infinity });
  const send = useMutation({ mutationFn: () => api.magicLink(email.trim(), redirect) });

  const header = (
    <div className="flex items-center gap-1 px-2 pb-2">
      <IconButton label={t("common.back")} onClick={() => navigate(-1)}>
        <ArrowLeft {...ICON} />
      </IconButton>
      <h1 className="text-lg font-semibold text-ink">{t("signin.title")}</h1>
    </div>
  );

  return (
    <Panel title={t("signin.title")} snap={0.92} header={header}>
      <div className="flex flex-col gap-4 px-4 pt-2 pb-8">
        {params.get("error") === "expired" && (
          <p role="alert" className="text-sm text-danger">
            {t("signin.expired")}
          </p>
        )}
        {send.isSuccess ? (
          <p role="status" className="rounded-md bg-surface-2 p-4 text-sm text-ink">
            {t("signin.sent")}
          </p>
        ) : (
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              send.mutate();
            }}
          >
            <label className="flex flex-col gap-1">
              <span className="text-sm font-medium text-ink">{t("signin.email")}</span>
              <input
                type="email"
                required
                autoComplete="email"
                inputMode="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="h-12 rounded-md border border-line bg-surface px-3 text-base text-ink"
              />
            </label>
            {send.isError && <p className="text-sm text-danger">{(send.error as ApiError).message}</p>}
            <Button type="submit" variant="primary" block disabled={send.isPending} icon={<Mail {...ICON} aria-hidden />}>
              {t("signin.send")}
            </Button>
          </form>
        )}
        {providers.data?.google && (
          <a href={api.googleStartUrl(redirect)} className="inline-flex h-12 items-center justify-center rounded-md border border-line bg-surface text-base text-ink">
            {t("signin.google")}
          </a>
        )}
      </div>
    </Panel>
  );
}
