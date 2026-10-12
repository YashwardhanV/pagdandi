import { useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Drawer } from "vaul";
import { useDesktop } from "../../lib/hooks";

export const SNAPS = [0.18, 0.52, 0.92] as const;
export type Snap = (typeof SNAPS)[number];

/**
 * Panel holds a screen's content: a bottom sheet with snap points 18 / 52 / 92 %
 * on phones (no backdrop, so the map stays usable), a 400 px left panel on
 * desktop (SPEC §10.2, §11.4).
 */
export function Panel({
  title,
  snap = 0.52,
  children,
  header,
  footer,
}: {
  title: string;
  snap?: Snap;
  children: ReactNode;
  header?: ReactNode;
  /** Primary actions: always on screen, in the thumb zone (SPEC §11.1). */
  footer?: ReactNode;
}) {
  const desktop = useDesktop();
  const [active, setActive] = useState<number | string | null>(snap);
  const [requested, setRequested] = useState(snap);
  if (requested !== snap) {
    setRequested(snap);
    setActive(snap);
  }

  if (desktop) {
    return (
      <aside
        aria-label={title}
        className="fixed inset-y-0 left-0 z-20 flex w-[400px] flex-col border-r border-line bg-surface"
      >
        {header}
        <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
        {footer && <div className="border-t border-line bg-surface">{footer}</div>}
      </aside>
    );
  }

  return (
    <Drawer.Root
      open
      modal={false}
      dismissible={false}
      snapPoints={[...SNAPS]}
      activeSnapPoint={active}
      setActiveSnapPoint={setActive}
    >
      <Drawer.Portal>
        <Drawer.Content
          aria-describedby={undefined}
          className="fixed inset-x-0 bottom-0 z-20 flex h-full max-h-[96%] flex-col rounded-t-lg bg-surface shadow-sheet outline-none"
        >
          <Drawer.Title className="sr-only">{title}</Drawer.Title>
          <div className="flex justify-center pt-2 pb-1" aria-hidden>
            <span className="h-1 w-9 rounded-full bg-line" />
          </div>
          {header}
          <div
            className="min-h-0 flex-1 overflow-y-auto overscroll-contain pb-[env(safe-area-inset-bottom)]"
            data-vaul-no-drag={active === SNAPS[2] ? true : undefined}
          >
            {children}
            {footer && <div className="h-28" aria-hidden />}
          </div>
        </Drawer.Content>
      </Drawer.Portal>
      {footer &&
        // Portaled like the drawer itself, so it stacks above the sheet.
        createPortal(
          <div className="fixed inset-x-0 bottom-0 z-30 border-t border-line bg-surface pb-[env(safe-area-inset-bottom)]">{footer}</div>,
          document.body,
        )}
    </Drawer.Root>
  );
}

export function SectionTitle({ children }: { children: ReactNode }) {
  return <h2 className="px-4 pt-4 pb-2 text-xs font-medium tracking-wide text-ink-2 uppercase">{children}</h2>;
}
