// Sunrise/sunset (NOAA equations), used to switch the theme and map style at dusk.
const rad = (d: number) => (d * Math.PI) / 180;
const deg = (r: number) => (r * 180) / Math.PI;

function julian(date: Date): number {
  return date.getTime() / 86400000 + 2440587.5;
}

function sunDeclinationAndEqTime(jd: number): { decl: number; eqTime: number } {
  const t = (jd - 2451545) / 36525;
  const l0 = (((280.46646 + t * (36000.76983 + 0.0003032 * t)) % 360) + 360) % 360;
  const m = 357.52911 + t * (35999.05029 - 0.0001537 * t);
  const e = 0.016708634 - t * (0.000042037 + 0.0000001267 * t);
  const c =
    Math.sin(rad(m)) * (1.914602 - t * (0.004817 + 0.000014 * t)) +
    Math.sin(rad(2 * m)) * (0.019993 - 0.000101 * t) +
    Math.sin(rad(3 * m)) * 0.000289;
  const app = l0 + c - 0.00569 - 0.00478 * Math.sin(rad(125.04 - 1934.136 * t));
  const e0 = 23 + (26 + (21.448 - t * (46.815 + t * (0.00059 - t * 0.001813))) / 60) / 60;
  const eps = e0 + 0.00256 * Math.cos(rad(125.04 - 1934.136 * t));
  const decl = deg(Math.asin(Math.sin(rad(eps)) * Math.sin(rad(app))));
  let y = Math.tan(rad(eps) / 2);
  y *= y;
  const eq =
    y * Math.sin(2 * rad(l0)) -
    2 * e * Math.sin(rad(m)) +
    4 * e * y * Math.sin(rad(m)) * Math.cos(2 * rad(l0)) -
    0.5 * y * y * Math.sin(4 * rad(l0)) -
    1.25 * e * e * Math.sin(2 * rad(m));
  return { decl, eqTime: deg(eq) * 4 };
}

/** Sunrise and sunset for the UTC day of `day`, or null in polar day/night. */
export function sunTimes(day: Date, lat: number, lng: number): { sunrise: Date; sunset: Date } | null {
  const d0 = Date.UTC(day.getUTCFullYear(), day.getUTCMonth(), day.getUTCDate());
  const jd = julian(new Date(d0 + 12 * 3600000));
  const { decl, eqTime } = sunDeclinationAndEqTime(jd);
  const cosHA = Math.cos(rad(90.833)) / (Math.cos(rad(lat)) * Math.cos(rad(decl))) - Math.tan(rad(lat)) * Math.tan(rad(decl));
  if (cosHA < -1 || cosHA > 1) return null;
  const ha = deg(Math.acos(cosHA));
  const noon = 720 - 4 * lng - eqTime;
  return { sunrise: new Date(d0 + (noon - 4 * ha) * 60000), sunset: new Date(d0 + (noon + 4 * ha) * 60000) };
}

export function isDaylight(at: Date, lat: number, lng: number): boolean {
  const solar = new Date(at.getTime() + (lng / 15) * 3600000);
  const t = sunTimes(solar, lat, lng);
  if (!t) return lat * (at.getUTCMonth() >= 3 && at.getUTCMonth() <= 8 ? 1 : -1) > 0;
  return at >= t.sunrise && at < t.sunset;
}
