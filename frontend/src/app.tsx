import { useEffect } from "preact/hooks";
import { useQuery, useQueryClient } from "@tanstack/preact-query";
import { api } from "./core/api";
import type { Session } from "./core/api";
import { locale, text } from "./core/i18n";
import { syncFromURL } from "./core/navigation";
import { ConsoleShell } from "./components/ConsoleShell";
import { LoginPage } from "./pages/LoginPage";
import { s } from "./styles";

export function App() {
  const queryClient = useQueryClient();
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<Session>("/api/session"),
    staleTime: 0,
  });
  useEffect(() => {
    const listener = () => syncFromURL();
    window.addEventListener("popstate", listener);
    return () => window.removeEventListener("popstate", listener);
  }, []);
  useEffect(() => {
    if (session.data) {
      locale.value = session.data.locale;
      document.documentElement.lang = session.data.locale;
    }
  }, [session.data?.locale]);
  if (session.isLoading) return <div class={s.loginPage}>{text("loading")}</div>;
  if (session.isError || !session.data)
    return (
      <div class={s.loginPage}>
        <div class={s.loginCard}>
          {text("requestFailed")}: {String(session.error)}
        </div>
      </div>
    );
  if (!session.data.authenticated)
    return (
      <LoginPage
        csrf={session.data.csrf}
        onSuccess={() => void queryClient.invalidateQueries({ queryKey: ["session"] })}
      />
    );
  return (
    <ConsoleShell csrf={session.data.csrf} onLogout={() => void queryClient.invalidateQueries()} />
  );
}
