// Sunrise/sunset (NOAA approximation) so the app turns to Dusk at the real sunset.

const rad = (d: number) => (d * Math.PI) / 180;
const deg = (r: number) => (r * 180) / Math.PI;

export function sunTimes(date: Date, lat: number, lng: number): { rise: Date; set: Date } | null {
  const day = Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate());
  const jd = day / 86400000 + 2440587.5;
  // Whole days since J2000 noon (the formula's epoch).
  const n = Math.ceil(jd - 2451545.0 + 0.0008);
  const jStar = n - lng / 360;
  const m = (357.5291 + 0.98560028 * jStar) % 360;
  const c = 1.9148 * Math.sin(rad(m)) + 0.02 * Math.sin(rad(2 * m)) + 0.0003 * Math.sin(rad(3 * m));
  const lambda = (m + c + 180 + 102.9372) % 360;
  const jTransit = 2451545.0 + jStar + 0.0053 * Math.sin(rad(m)) - 0.0069 * Math.sin(rad(2 * lambda));
  const decl = Math.asin(Math.sin(rad(lambda)) * Math.sin(rad(23.44)));
  const cosW = (Math.sin(rad(-0.833)) - Math.sin(rad(lat)) * Math.sin(decl)) / (Math.cos(rad(lat)) * Math.cos(decl));
  if (cosW < -1 || cosW > 1) return null;
  const w = deg(Math.acos(cosW));
  const toDate = (j: number) => new Date((j - 2440587.5) * 86400000);
  return { rise: toDate(jTransit - w / 360), set: toDate(jTransit + w / 360) };
}

export function isDark(now: Date, lat: number, lng: number): boolean {
  const t = sunTimes(now, lat, lng);
  if (!t) return false;
  // Dusk mood starts 20 minutes before sunset and lasts until 20 minutes after sunrise.
  return now.getTime() > t.set.getTime() - 20 * 60000 || now.getTime() < t.rise.getTime() + 20 * 60000;
}
