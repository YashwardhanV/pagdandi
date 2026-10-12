import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Footprints } from "lucide-react";
import { api } from "../../lib/api";
import { useT } from "../../lib/i18n";
import { useMe } from "../../lib/queries";
import { Button, ICON, toast } from "../../components/ui";
import { useMapStore } from "../map/mapStore";

/**
 * Desire-path prompt (SPEC §7.7): nearby signed-in riders are asked whether a
 * path many riders seem to use is usable. Two "yes" answers promote it.
 */
export function CandidatePrompt({ center }: { center: { lat: number; lng: number } | null }) {
  const t = useT();
  const qc = useQueryClient();
  const me = useMe();
  const setMap = useMapStore((s) => s.set);
  const d = 0.03;
  const q = useQuery({
    queryKey: ["candidates", center && `${center.lat.toFixed(2)},${center.lng.toFixed(2)}`],
    enabled: !!center && !!me.data,
    queryFn: () => api.candidates([center!.lng - d, center!.lat - d, center!.lng + d, center!.lat + d]),
    staleTime: 5 * 60 * 1000,
  });
  const c = q.data?.candidates[0];

  useEffect(() => {
    if (!c) return;
    setMap({ highlight: { polyline: c.polyline, kind: "candidate" } });
    return () => setMap({ highlight: null });
  }, [c, setMap]);

  const answer = useMutation({
    mutationFn: (yes: boolean) => api.answerCandidate(c!.id, yes),
    onSuccess: () => {
      toast(t("candidate.thanks"));
      void qc.invalidateQueries({ queryKey: ["candidates"] });
    },
    onError: (e: Error) => toast(e.message),
  });

  if (!c) return null;
  return (
    <div className="mx-4 mb-2 flex flex-col gap-2 rounded-md border border-line bg-surface-2 p-3" role="region" aria-label={t("candidate.prompt")}>
      <p className="flex items-start gap-2 text-sm text-ink">
        <Footprints {...ICON} className="shrink-0 text-warn" aria-hidden />
        {t("candidate.prompt")}
      </p>
      <div className="flex gap-2">
        <Button block onClick={() => answer.mutate(false)} disabled={answer.isPending}>
          {t("common.no")}
        </Button>
        <Button block variant="primary" onClick={() => answer.mutate(true)} disabled={answer.isPending}>
          {t("common.yes")}
        </Button>
      </div>
    </div>
  );
}
