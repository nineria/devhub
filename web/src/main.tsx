import { Component, StrictMode, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "./App";
import "./styles.css";

const client = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 1000 } },
});

/** A crash shows a message and a way out, never a blank page. */
class Boundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <main className="mx-auto max-w-160 px-4 py-16">
        <h1 className="text-[34px] font-bold">Something went wrong</h1>
        <p className="mt-2 text-label-2">The page hit an error. Reloading usually fixes it.</p>
        <button
          type="button"
          className="mt-6 min-h-[50px] rounded-(--radius-control) bg-accent px-6 text-[17px] font-semibold text-on-accent"
          onClick={() => location.reload()}
        >
          Reload
        </button>
      </main>
    );
  }
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={client}>
      <Boundary>
        <App />
      </Boundary>
    </QueryClientProvider>
  </StrictMode>,
);
