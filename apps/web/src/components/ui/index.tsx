import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from "react";
import * as ToggleGroup from "@radix-ui/react-toggle-group";
import { AlertTriangle, RotateCw } from "lucide-react";
import { create } from "zustand";

const cx = (...c: (string | false | null | undefined)[]) => c.filter(Boolean).join(" ");
export { cx };

/** Lucide icons at the house size: 20 px, stroke 1.75 (SPEC §11.2). */
export const ICON = { size: 20, strokeWidth: 1.75 } as const;

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "ghost" | "danger";
  block?: boolean;
  icon?: ReactNode;
};

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = "secondary", block, icon, className, children, type = "button", ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      className={cx(
        "inline-flex h-12 items-center justify-center gap-2 rounded-md px-4 text-base font-medium lg:h-10 lg:text-sm",
        "transition-colors duration-[var(--dur-fast)] disabled:cursor-not-allowed disabled:opacity-50",
        variant === "primary" && "bg-accent text-accent-ink hover:brightness-95",
        variant === "secondary" && "border border-line bg-surface text-ink hover:bg-surface-2",
        variant === "ghost" && "text-ink-2 hover:bg-surface-2 hover:text-ink",
        variant === "danger" && "border border-line bg-surface text-danger hover:bg-surface-2",
        block && "w-full",
        className,
      )}
      {...rest}
    >
      {icon}
      {children}
    </button>
  );
});

type IconButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { label: string; floating?: boolean };

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { label, floating, className, children, type = "button", ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      aria-label={label}
      title={label}
      className={cx(
        "inline-flex h-12 w-12 shrink-0 items-center justify-center rounded-full text-ink transition-colors duration-[var(--dur-fast)]",
        floating ? "bg-surface shadow-float hover:bg-surface-2" : "hover:bg-surface-2",
        "disabled:opacity-50",
        className,
      )}
      {...rest}
    >
      {children}
    </button>
  );
});

export function Chip({
  children,
  tone = "neutral",
  icon,
  onClick,
  pressed,
  className,
}: {
  children: ReactNode;
  tone?: "neutral" | "ok" | "warn" | "danger" | "accent";
  icon?: ReactNode;
  onClick?: () => void;
  pressed?: boolean;
  className?: string;
}) {
  const toneCls = {
    neutral: "text-ink-2",
    ok: "text-ok",
    warn: "text-warn",
    danger: "text-danger",
    accent: "text-accent",
  }[tone];
  const cls = cx(
    "inline-flex min-h-8 items-center gap-1.5 rounded-full border border-line bg-surface px-3 text-xs font-medium whitespace-nowrap",
    toneCls,
    pressed && "border-ink bg-surface-2 text-ink",
    onClick && "min-h-12 px-4 text-sm hover:bg-surface-2",
    className,
  );
  if (onClick)
    return (
      <button type="button" className={cls} onClick={onClick} aria-pressed={pressed}>
        {icon}
        {children}
      </button>
    );
  return (
    <span className={cls}>
      {icon}
      {children}
    </span>
  );
}

export function SegmentedControl<V extends string>({
  value,
  onChange,
  options,
  label,
}: {
  value: V;
  onChange: (v: V) => void;
  options: { value: V; label: string; icon?: ReactNode }[];
  label: string;
}) {
  return (
    <ToggleGroup.Root
      type="single"
      value={value}
      onValueChange={(v) => v && onChange(v as V)}
      aria-label={label}
      className="flex w-full rounded-md bg-surface-2 p-1"
    >
      {options.map((o) => (
        <ToggleGroup.Item
          key={o.value}
          value={o.value}
          className={cx(
            "flex h-10 flex-1 items-center justify-center gap-1.5 rounded-sm px-2 text-sm font-medium text-ink-2 transition-colors",
            "data-[state=on]:bg-surface data-[state=on]:text-ink data-[state=on]:shadow-float",
          )}
        >
          {o.icon}
          {o.label}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup.Root>
  );
}

export function ListRow({
  icon,
  title,
  subtitle,
  trailing,
  onClick,
  href,
}: {
  icon?: ReactNode;
  title: ReactNode;
  subtitle?: ReactNode;
  trailing?: ReactNode;
  onClick?: () => void;
  href?: string;
}) {
  const inner = (
    <>
      {icon && <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-surface-2 text-ink-2">{icon}</span>}
      <span className="min-w-0 flex-1">
        <span className="block truncate text-base text-ink">{title}</span>
        {subtitle && <span className="block truncate text-sm text-ink-2">{subtitle}</span>}
      </span>
      {trailing && <span className="shrink-0 text-sm text-ink-2">{trailing}</span>}
    </>
  );
  const cls = "flex min-h-14 w-full items-center gap-3 px-4 py-2 text-left hover:bg-surface-2";
  if (href)
    return (
      <a className={cls} href={href}>
        {inner}
      </a>
    );
  if (onClick)
    return (
      <button type="button" className={cls} onClick={onClick}>
        {inner}
      </button>
    );
  return <div className={cls}>{inner}</div>;
}

export function Skeleton({ className, label = "Loading" }: { className?: string; label?: string }) {
  return <div className={cx("skeleton", className)} role="status" aria-label={label} />;
}

export function EmptyState({ icon, title, body, action }: { icon?: ReactNode; title: string; body?: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 px-6 py-8 text-center">
      {icon && <span className="text-ink-3">{icon}</span>}
      <p className="text-base font-medium text-ink">{title}</p>
      {body && <p className="text-sm text-ink-2">{body}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

export function ErrorState({ message, onRetry, retryLabel = "Try again" }: { message: string; onRetry?: () => void; retryLabel?: string }) {
  return (
    <div role="alert" className="flex flex-col items-center gap-3 px-6 py-8 text-center">
      <AlertTriangle {...ICON} className="text-warn" aria-hidden />
      <p className="text-sm text-ink-2">{message}</p>
      {onRetry && (
        <Button onClick={onRetry} icon={<RotateCw {...ICON} aria-hidden />}>
          {retryLabel}
        </Button>
      )}
    </div>
  );
}

export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="rounded-sm border border-line bg-surface-2 px-1.5 font-mono text-xs text-ink-2">{children}</kbd>;
}

/* Toasts ------------------------------------------------------------------ */

type ToastItem = { id: number; text: string; tone: "neutral" | "ok" | "danger" };
const useToasts = create<{ items: ToastItem[] }>(() => ({ items: [] }));
let nextId = 1;

export function toast(text: string, tone: ToastItem["tone"] = "neutral") {
  const id = nextId++;
  useToasts.setState((s) => ({ items: [...s.items, { id, text, tone }] }));
  setTimeout(() => useToasts.setState((s) => ({ items: s.items.filter((t) => t.id !== id) })), 4000);
}

export function Toaster() {
  const items = useToasts((s) => s.items);
  return (
    <div aria-live="polite" className="pointer-events-none fixed inset-x-0 top-4 z-50 flex flex-col items-center gap-2 px-4">
      {items.map((t) => (
        <div
          key={t.id}
          className={cx(
            "pointer-events-auto max-w-md rounded-md bg-ink px-4 py-3 text-sm text-bg shadow-float",
            t.tone === "danger" && "bg-danger text-accent-ink",
          )}
        >
          {t.text}
        </div>
      ))}
    </div>
  );
}
