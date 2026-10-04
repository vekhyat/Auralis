import { Component, lazy, type ReactNode } from "react";
import { t } from "@/i18n";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { loadDebugLoggerPage, loadDevicesPage, loadHistoryPage, loadLibraryHealthPage, loadQueuePage, loadSettingsPage, } from "@/lib/page-loaders";

export const SettingsPage = lazy(loadSettingsPage);
export const DebugLoggerPage = lazy(loadDebugLoggerPage);
export const HistoryPage = lazy(loadHistoryPage);
export const QueuePage = lazy(loadQueuePage);
export const DevicesPage = lazy(loadDevicesPage);
export const LibraryHealthPage = lazy(loadLibraryHealthPage);

export function PageLoading() {
    return (<div role="status" aria-live="polite" aria-busy="true" className="flex items-center gap-2 border border-border bg-background px-3 py-3 text-sm text-muted-foreground">
      <Spinner role="presentation" aria-hidden="true" aria-label={undefined}/>
      <span>{t("translation.common.loading")}</span>
    </div>);
}

interface PageErrorBoundaryProps {
    resetKey: string;
    onRetry: () => void;
    children: ReactNode;
}

interface PageErrorBoundaryState {
    error: Error | null;
}

export class PageErrorBoundary extends Component<PageErrorBoundaryProps, PageErrorBoundaryState> {
    state: PageErrorBoundaryState = { error: null };

    static getDerivedStateFromError(error: Error): PageErrorBoundaryState {
        return { error };
    }

    componentDidCatch(error: Error) {
        console.error("Failed to load page:", error);
    }

    componentDidUpdate(previous: PageErrorBoundaryProps) {
        if (previous.resetKey !== this.props.resetKey && this.state.error) {
            this.setState({ error: null });
        }
    }

    render() {
        if (this.state.error) {
            return (<div role="alert" className="border border-border bg-background px-4 py-3 text-sm">
        <p className="text-destructive">{t("translation.app.pageLoadFailed")}</p>
        <div className="mt-3">
          <Button type="button" variant="outline" onClick={this.props.onRetry}>
            {t("translation.queue.retry")}
          </Button>
        </div>
      </div>);
        }
        return this.props.children;
    }
}
