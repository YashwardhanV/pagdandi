import type { SVGProps } from "react";

// One hand-drawn line icon set (24×24, 1.8 stroke) so vehicles and UI read as a family.
const P: Record<string, string> = {
  // vehicles
  bike: "M5 17a3.2 3.2 0 1 0 0-6.4 3.2 3.2 0 0 0 0 6.4ZM19 17a3.2 3.2 0 1 0 0-6.4 3.2 3.2 0 0 0 0 6.4ZM5 13.8l3.6-4.3h4.2l2.4 2.6M19 13.8l-2.6-6.3h-2.6M8.6 9.5 7.6 7H5.4M12 9.5l-1.6 4.3h4.8",
  scooty: "M6.5 18a2.4 2.4 0 1 0 0-4.8 2.4 2.4 0 0 0 0 4.8ZM18 18a2.4 2.4 0 1 0 0-4.8 2.4 2.4 0 0 0 0 4.8ZM6.5 15.6h6.2l2.3-6.1h2.2l1 6.1M8.2 11.4h5M17.2 9.5 18 5.8h2",
  car: "M3.5 15.5v-2.6l1.8-4.6c.3-.7.9-1.1 1.6-1.1h10.2c.7 0 1.3.4 1.6 1.1l1.8 4.6v2.6c0 .6-.4 1-1 1h-15c-.6 0-1-.4-1-1ZM3.6 12.4h16.8M7.2 18.5v-2M16.8 18.5v-2M7 14.4h1.6M15.4 14.4H17",
  cycle: "M5.5 17.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7ZM18.5 17.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7ZM5.5 14l3.6-6h6.2L18.5 14M9.1 8l3.4 6H5.5M15.3 8l-.9-2.4h2.4M8.2 6h2.6",
  walk: "M13.2 5.6a1.7 1.7 0 1 0 0-3.4 1.7 1.7 0 0 0 0 3.4ZM12.4 8.2l-2 5.3 3 2.3.9 5.6M10.4 13.5l-2.9 3.6M12.4 8.2l2.6 3h3.2M12.4 8.2l-3.3 1.4-1.4 2.8",
  jetski: "M2.8 15.2h13.8l4.6-3.4h-5.8l-2.2-3.4H9.4l-1.2 3.4H4.1l-1.3 3.4ZM11 8.4l.8-2.6h2.6M2 19.2c1.3-1.1 2.7-1.1 4 0s2.7 1.1 4 0 2.7-1.1 4 0 2.7 1.1 4 0 2.7-1.1 4 0",
  // ui
  search: "M10.8 18.1a7.3 7.3 0 1 0 0-14.6 7.3 7.3 0 0 0 0 14.6ZM16.2 16.2l4.3 4.3",
  close: "M6 6l12 12M18 6 6 18",
  back: "M15 5l-7 7 7 7",
  chevron: "M9 6l6 6-6 6",
  up: "M6 15l6-6 6 6",
  swap: "M7 4v16M7 4 4 7M7 4l3 3M17 20V4M17 20l-3-3M17 20l3-3",
  locate: "M12 19.5a7.5 7.5 0 1 0 0-15 7.5 7.5 0 0 0 0 15ZM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6ZM12 2v2.5M12 19.5V22M2 12h2.5M19.5 12H22",
  layers: "m12 3.5 9 4.8-9 4.8-9-4.8 9-4.8ZM3 12.2l9 4.8 9-4.8M3 16.2l9 4.8 9-4.8",
  dice: "M6 3.5h12A2.5 2.5 0 0 1 20.5 6v12a2.5 2.5 0 0 1-2.5 2.5H6A2.5 2.5 0 0 1 3.5 18V6A2.5 2.5 0 0 1 6 3.5ZM8.5 8.5h.01M15.5 8.5h.01M12 12h.01M8.5 15.5h.01M15.5 15.5h.01",
  play: "M7 4.8v14.4c0 .8.9 1.3 1.6.8l11-7.2c.6-.4.6-1.2 0-1.6L8.6 4c-.7-.5-1.6 0-1.6.8Z",
  nav: "M12 2.5 4.5 20.2c-.2.5.3 1 .8.7L12 17l6.7 3.9c.5.3 1-.2.8-.7L12 2.5Z",
  stop: "M7 6h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1Z",
  mountain: "M2.5 19.5 9 8.5l3.8 6.2M10.7 11.8l2.9-4.6 7.9 12.3H2.5",
  warn: "M12 3.5 2.5 20h19L12 3.5ZM12 10v4.5M12 17.3h.01",
  leaf: "M5 19c0-8 5-13.5 14.5-14.5C19 14 13 19 5 19ZM5 19l7-7",
  water: "M12 3s6.5 7.2 6.5 11.3a6.5 6.5 0 0 1-13 0C5.5 10.2 12 3 12 3Z",
  sparkle: "M12 3.5 13.9 9l5.6 1.9-5.6 1.9L12 18.5l-1.9-5.7L4.5 10.9 10.1 9 12 3.5ZM19 16l.7 2 2 .7-2 .7-.7 2-.7-2-2-.7 2-.7.7-2Z",
  sun: "M12 16.5a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9ZM12 2v2M12 20v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M2 12h2M20 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4",
  moon: "M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5Z",
  auto: "M12 21a9 9 0 1 0 0-18v18ZM12 3a9 9 0 0 1 0 18",
  info: "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18ZM12 11v5.5M12 7.6h.01",
  pin: "M12 21s-6.5-5.6-6.5-11a6.5 6.5 0 0 1 13 0c0 5.4-6.5 11-6.5 11ZM12 12.2a2.4 2.4 0 1 0 0-4.8 2.4 2.4 0 0 0 0 4.8Z",
  dot: "M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z",
  clock: "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18ZM12 7v5l3 2",
  loop: "M17.5 7.5A7 7 0 1 0 19 12M17.5 3.5v4h-4",
  places: "M12 21s-6.5-5.6-6.5-11a6.5 6.5 0 0 1 13 0c0 5.4-6.5 11-6.5 11ZM9.5 9.8l1.8 1.8 3.4-3.4",
  volume: "M4 9.5v5h3.5L12 19V5L7.5 9.5H4ZM16 9a4 4 0 0 1 0 6M18.5 6.5a7.5 7.5 0 0 1 0 11",
  mute: "M4 9.5v5h3.5L12 19V5L7.5 9.5H4ZM16.5 9.5l5 5M21.5 9.5l-5 5",
  cube: "m12 2.8 8 4.6v9.2l-8 4.6-8-4.6V7.4l8-4.6ZM12 12l8-4.6M12 12v9.2M12 12 4 7.4",
  fresh: "M4 20c4-1 6-5 8-9s5-6 8-7M6 7.5h.01M9 4.5h.01M15 18.5h.01M18.5 15h.01",
  // manoeuvres
  t_straight: "M12 20V4M12 4 7 9M12 4l5 5",
  t_left: "M17 20v-7a3 3 0 0 0-3-3H5M5 10l4-4M5 10l4 4",
  t_right: "M7 20v-7a3 3 0 0 1 3-3h9M19 10l-4-4M19 10l-4 4",
  t_slight_left: "M15 20v-6l-7-7M8 7v5M8 7h5",
  t_slight_right: "M9 20v-6l7-7M16 7v5M16 7h-5",
  t_sharp_left: "M16 20V9L6 17M6 17h5M6 17v-5",
  t_sharp_right: "M8 20V9l10 8M18 17h-5M18 17v-5",
  t_uturn: "M8 20V9a4 4 0 0 1 8 0v4M16 13l-3-3M16 13l3-3",
  t_round: "M12 20v-4M12 16a4 4 0 1 0-4-4M8 12l-3-1M8 12l1-3",
  t_finish: "M6 21V4M6 4h11l-2.5 3.5L17 11H6",
};

export type IconName = keyof typeof P;

export function Icon({ name, size = 22, ...rest }: { name: IconName | string; size?: number } & SVGProps<SVGSVGElement>) {
  const d = P[name] ?? P.dot;
  const filled = name === "play" || name === "nav" || name === "stop";
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill={filled ? "currentColor" : "none"}
      stroke={filled ? "none" : "currentColor"}
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      {...rest}
    >
      <path d={d} />
    </svg>
  );
}

/** GraphHopper sign → manoeuvre icon. */
export function turnIcon(sign: number): IconName {
  switch (sign) {
    case -3:
      return "t_sharp_left";
    case -2:
      return "t_left";
    case -1:
    case -7:
      return "t_slight_left";
    case 1:
    case 7:
      return "t_slight_right";
    case 2:
      return "t_right";
    case 3:
      return "t_sharp_right";
    case -98:
    case -8:
    case 8:
      return "t_uturn";
    case 6:
    case -6:
      return "t_round";
    case 4:
    case 5:
      return "t_finish";
    default:
      return "t_straight";
  }
}

/** The wordmark: a dashed footpath between two points, and the name in Devanagari + Latin. */
export function Logo({ size = 30 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden="true">
      <defs>
        <linearGradient id="pd-logo" x1="0" y1="1" x2="1" y2="0">
          <stop offset="0" stopColor="#FF8A4C" />
          <stop offset="1" stopColor="#FFD23F" />
        </linearGradient>
      </defs>
      <rect width="64" height="64" rx="18" fill="var(--logo-bg, #0B0F15)" />
      <path d="M14 50c10-2 6-14 16-16s14 4 20-18" fill="none" stroke="url(#pd-logo)" strokeWidth="5" strokeLinecap="round" strokeDasharray="1 8.5" />
      <circle cx="14" cy="50" r="4.5" fill="#FF8A4C" />
      <circle cx="50" cy="16" r="4.5" fill="#FFD23F" />
    </svg>
  );
}
