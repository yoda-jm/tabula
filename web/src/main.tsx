import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { startEvents } from "./store/events";
import { store } from "./store/store";
import "./styles.css";

void store.loadSchema();
startEvents(store);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
