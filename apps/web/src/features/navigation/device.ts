// Voice (Web Speech API) and screen wake lock for navigation (SPEC §6.4).
import type { Lang } from "../../state/store";
import { BCP47 } from "../../lib/i18n";

export function speak(text: string, lang: Lang) {
  if (!("speechSynthesis" in window)) return;
  const u = new SpeechSynthesisUtterance(text);
  u.lang = BCP47[lang];
  const voice = speechSynthesis.getVoices().find((v) => v.lang === u.lang) ?? speechSynthesis.getVoices().find((v) => v.lang.startsWith(lang));
  if (voice) u.voice = voice;
  u.rate = 1;
  speechSynthesis.cancel();
  speechSynthesis.speak(u);
}

export function stopSpeaking() {
  if ("speechSynthesis" in window) speechSynthesis.cancel();
}

type Sentinel = { release: () => Promise<void> };

/** Keeps the screen on while navigating; re-acquires after tab switches. Returns release. */
export function holdWakeLock(): () => void {
  const wl = (navigator as Navigator & { wakeLock?: { request: (t: "screen") => Promise<Sentinel> } }).wakeLock;
  if (!wl) return () => {};
  let sentinel: Sentinel | null = null;
  let active = true;
  const acquire = () => {
    if (!active || document.visibilityState !== "visible") return;
    wl.request("screen").then((s) => (sentinel = s)).catch(() => {});
  };
  acquire();
  document.addEventListener("visibilitychange", acquire);
  return () => {
    active = false;
    document.removeEventListener("visibilitychange", acquire);
    void sentinel?.release().catch(() => {});
  };
}
