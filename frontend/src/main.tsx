import { render } from "preact";
import { QueryClient, QueryClientProvider } from "@tanstack/preact-query";
import { App } from "./app.tsx";
import "./styles";

const client = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 5000, refetchOnWindowFocus: false } },
});
render(
  <QueryClientProvider client={client}>
    <App />
  </QueryClientProvider>,
  document.getElementById("app")!,
);
