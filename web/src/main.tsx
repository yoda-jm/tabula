import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { startEvents } from "./store/events";
import { store } from "./store/store";
import "./styles.css";

// Ask who we are first: a device without access gets no state or events.
async function boot() {
  let who = await store.loadAccess();
  for (let wait = 1000; !who; wait = Math.min(wait * 2, 10000)) {
    await new Promise((r) => setTimeout(r, wait)); // tabula not reachable yet
    who = await store.loadAccess();
  }
  if (who.role === "none") return;
  void store.loadSchema();
  startEvents(store);
}
void boot();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
