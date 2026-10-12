import * as Dialog from "@radix-ui/react-dialog";
import { MapPin } from "lucide-react";
import { create } from "zustand";
import { needsExplanation } from "../lib/geolocation";
import { useT } from "../lib/i18n";
import { Button, ICON } from "./ui";

const useExplainer = create<{ open: boolean; resolve: ((ok: boolean) => void) | null }>(() => ({ open: false, resolve: null }));

/** Explain why we need location before the browser prompt appears (SPEC §10.3). */
export async function confirmLocation(): Promise<boolean> {
  if (!(await needsExplanation())) return true;
  return new Promise((resolve) => useExplainer.setState({ open: true, resolve }));
}

export function LocateExplainer() {
  const t = useT();
  const { open, resolve } = useExplainer();
  const close = (ok: boolean) => {
    resolve?.(ok);
    useExplainer.setState({ open: false, resolve: null });
  };
  return (
    <Dialog.Root open={open} onOpenChange={(o) => !o && close(false)}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-[var(--scrim)]" />
        <Dialog.Content className="fixed inset-x-4 bottom-4 z-50 mx-auto max-w-sm rounded-lg bg-surface p-5 shadow-float lg:top-1/3 lg:bottom-auto">
          <div className="mb-3 flex items-center gap-2 text-ink">
            <MapPin {...ICON} aria-hidden />
            <Dialog.Title className="text-lg font-semibold">{t("home.locate")}</Dialog.Title>
          </div>
          <Dialog.Description className="mb-5 text-sm text-ink-2">{t("home.locate_why")}</Dialog.Description>
          <div className="flex gap-2">
            <Button block onClick={() => close(false)}>
              {t("common.cancel")}
            </Button>
            <Button block variant="primary" onClick={() => close(true)}>
              {t("common.yes")}
            </Button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
