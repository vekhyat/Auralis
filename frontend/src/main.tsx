import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { MotionConfig } from "motion/react";
import "./index.css";
import App from "./App.tsx";
import { Toaster } from "@/components/ui/sonner";
import { initializeQueuePersistence, flushQueuePersistence } from "@/lib/queue";
import { FrontendReady, FinishClose } from "../wailsjs/go/main/App";
import { EventsOn } from "../wailsjs/runtime/runtime";
import { toast } from "sonner";
import { t } from "@/i18n";
import "@/i18n";
async function bootstrap() {
    try {
        await initializeQueuePersistence();
    }
    catch (err) {
        console.error("Failed to initialize queue persistence:", err);
    }
    EventsOn("app:before-close", async () => {
        let saved = false;
        try {
            saved = await flushQueuePersistence();
        }
        catch (error) {
            console.error("Failed to save queue before closing:", error);
        }
        if (!saved) {
            toast.error(t("translation.lyricsManager.saveFailed"), {
                description: t("translation.app.unsavedChanges"),
                duration: 8000,
            });
        }
        await FinishClose(saved);
    });
    await FrontendReady();
    createRoot(document.getElementById("root")!).render(<StrictMode>
        <MotionConfig reducedMotion="user">
          <App />
          <Toaster position="bottom-left" duration={1000}/>
        </MotionConfig>
      </StrictMode>);
}
void bootstrap();
