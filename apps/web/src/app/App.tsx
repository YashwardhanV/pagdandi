import { Component, lazy, Suspense, useEffect, type ReactNode } from "react";
import { createBrowserRouter, Outlet, RouterProvider, useRouteError } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WifiOff } from "lucide-react";
import { useOnline } from "../lib/hooks";
import { useT } from "../lib/i18n";
import { useScheme } from "../lib/theme";
import { useApp } from "../state/store";
import { ErrorState, Toaster } from "../components/ui";
import { LocateExplainer } from "../components/LocateExplainer";
import HomePage from "../features/home/HomePage";
import SearchPage from "../features/search/SearchPage";
import RoutePage from "../features/route/RoutePage";

const MapView = lazy(() => import("../features/map/MapView"));
const NavScreen = lazy(() => import("../features/navigation/NavScreen"));
const AddShortcut = lazy(() => import("../features/shortcuts/AddShortcut"));
const ShortcutDetail = lazy(() => import("../features/shortcuts/ShortcutDetail"));
const ExplorePage = lazy(() => import("../features/explore/ExplorePage"));
const ProfilePage = lazy(() => import("../features/profile/ProfilePage"));
const SignInPage = lazy(() => import("../features/profile/SignInPage"));
const OfflinePacks = lazy(() => import("../features/offline/OfflinePacks"));
const ModerationQueue = lazy(() => import("../features/admin/ModerationQueue"));

export const queryClient = new QueryClient({
  defaultOptions: { queries: { refetchOnWindowFocus: false, retry: 1 } },
});

class MapBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  override state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  override render() {
    // The UI keeps working without a map (e.g. no WebGL); never block the app on it.
    return this.state.failed ? <div className="absolute inset-0 bg-surface-2" aria-hidden /> : this.props.children;
  }
}

function OfflineBanner() {
  const t = useT();
  const online = useOnline();
  if (online) return null;
  return (
    <div role="status" className="fixed inset-x-3 top-[max(76px,calc(env(safe-area-inset-top)+64px))] z-30 flex items-center gap-2 rounded-md bg-ink px-3 py-2 text-sm text-bg shadow-float lg:left-[412px] lg:max-w-md" data-testid="offline-banner">
      <WifiOff size={16} strokeWidth={1.75} aria-hidden />
      {t("common.offline")}
    </div>
  );
}

function Shell() {
  useScheme();
  const lang = useApp((s) => s.lang);
  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);
  return (
    <div className="fixed inset-0 overflow-hidden bg-bg">
      <div className="absolute inset-0 lg:left-[400px]">
        <MapBoundary>
          <Suspense fallback={<div className="absolute inset-0 bg-bg" aria-hidden />}>
            <MapView />
          </Suspense>
        </MapBoundary>
      </div>
      <OfflineBanner />
      <Suspense fallback={null}>
        <Outlet />
      </Suspense>
      <LocateExplainer />
      <Toaster />
    </div>
  );
}

function RouteErrorPage() {
  const err = useRouteError() as Error;
  return (
    <div className="flex h-full items-center justify-center bg-bg">
      <ErrorState message={err?.message ?? "Something went wrong."} onRetry={() => location.assign("/")} retryLabel="Home" />
    </div>
  );
}

const router = createBrowserRouter([
  {
    element: <Shell />,
    errorElement: <RouteErrorPage />,
    children: [
      { index: true, element: <HomePage /> },
      { path: "search", element: <SearchPage /> },
      { path: "route", element: <RoutePage /> },
      { path: "nav/:routeId/:optionId", element: <NavScreen /> },
      { path: "s/:id", element: <ShortcutDetail /> },
      { path: "add", element: <AddShortcut /> },
      { path: "explore", element: <ExplorePage /> },
      { path: "explore/:regionId", element: <ExplorePage /> },
      { path: "me", element: <ProfilePage /> },
      { path: "signin", element: <SignInPage /> },
      { path: "offline", element: <OfflinePacks /> },
      { path: "admin", element: <ModerationQueue /> },
      { path: "*", element: <HomePage /> },
    ],
  },
]);

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
