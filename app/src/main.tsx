import React from "react";
import ReactDOM from "react-dom/client";
import { ThemeProvider } from "next-themes";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { DataProvider } from "@/lib/store";
import { UpdaterProvider } from "@/lib/updater";
import App from "./App";
import "./index.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ThemeProvider attribute="class" defaultTheme="system" enableSystem storageKey="teamsync-theme">
      <TooltipProvider delayDuration={300}>
        <DataProvider>
          <UpdaterProvider>
            <App />
          </UpdaterProvider>
          <Toaster position="bottom-right" richColors closeButton />
        </DataProvider>
      </TooltipProvider>
    </ThemeProvider>
  </React.StrictMode>,
);
