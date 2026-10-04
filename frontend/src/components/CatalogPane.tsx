import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowUpDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { FetchHistory, type HistoryItem } from "@/components/FetchHistory";
import { ArtworkCard } from "./ArtworkCard";
import { Music2 } from "lucide-react";
import { InspectorPane } from "./InspectorPane";
import type { SmartSearchController, ResultTab } from "@/hooks/smart-search-core";
import { cn } from "@/lib/utils";

function formatDuration(ms: number): string {
    const minutes = Math.floor(ms / 60000);
    const seconds = Math.floor((ms % 60000) / 1000);
    return `${minutes}:${seconds.toString().padStart(2, "0")}`;
}

interface CatalogPaneProps {
    controller: SmartSearchController;
    query: string;
    history: HistoryItem[];
    onHistorySelect: (item: HistoryItem) => void;
    onHistoryRemove: (id: string) => void;
    onRecentSearchSelect: (query: string) => void;
    onDownloadResult: (url: string) => void;
    /** Result URLs whose tracks are still being read for a download. */
    requestingDownloads: ReadonlySet<string>;
    hasMetadata: boolean;
}

/**
 * The left desk of the library page. Empty state is a ruled catalog with a
 * one-line hint; a query turns it into dense result rows with the selected
 * entry inspected on the right.
 */
export function CatalogPane({ controller, query, history, onHistorySelect, onHistoryRemove, onRecentSearchSelect, onDownloadResult, requestingDownloads, hasMetadata }: CatalogPaneProps) {
    const { t } = useTranslation();
    if (controller.inputKind === "search") {
        return <SearchResults controller={controller} query={query} onDownloadResult={onDownloadResult} requestingDownloads={requestingDownloads}/>;
    }
    if (hasMetadata) {
        return null;
    }
    return (<div className="flex min-h-full flex-col">
      {history.length === 0 && <div className="library-intro mb-5">
        <h1 className="text-3xl font-semibold tracking-tight">{t("translation.downloads.libraryTitle")}</h1>
        <p className="mt-3 max-w-[60ch] text-sm leading-6 text-muted-foreground">{t("translation.downloads.libraryHint")}</p>
      </div>}
      {history.length === 0 && <div className="my-8 flex min-h-52 items-center gap-6 rounded-xl border border-dashed bg-card p-8">
        <Music2 className="size-12 shrink-0 text-primary" strokeWidth={1} />
        <div><h2 className="text-lg font-semibold">{t("translation.downloads.emptyLibrary")}</h2><p className="mt-2 max-w-[52ch] text-sm leading-6 text-muted-foreground">{t("translation.downloads.emptyLibraryHint")}</p></div>
      </div>}

      {controller.recentSearches.length > 0 ? (<div className="mt-4 flex flex-wrap items-center gap-x-3 gap-y-1">
        <span className="text-[11px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.catalog.recentSearches")}</span>
        {controller.recentSearches.map((recentQuery) => (<button
          key={recentQuery}
          type="button"
          className="cursor-pointer text-[13px] text-foreground/80 underline decoration-border underline-offset-4 transition-colors hover:text-primary hover:decoration-primary"
          onClick={() => onRecentSearchSelect(recentQuery)}
        >
          {recentQuery}
        </button>))}
      </div>) : null}

      <div className="mt-6">
        <FetchHistory history={history} onSelect={onHistorySelect} onRemove={onHistoryRemove}/>
      </div>
    </div>);
}

const TAB_KEYS: ResultTab[] = ["tracks", "albums", "artists", "playlists"];

function SearchResults({ controller, query, onDownloadResult, requestingDownloads }: {
    controller: SmartSearchController;
    query: string;
    onDownloadResult: (url: string) => void;
    requestingDownloads: ReadonlySet<string>;
}) {
    const { t } = useTranslation();
    const [selectedId, setSelectedId] = useState<string | null>(null);
    const [lastResults, setLastResults] = useState(controller.searchResults);
    // Adjust-state-during-render pattern: a new result set invalidates the selection.
    if (lastResults !== controller.searchResults) {
        setLastResults(controller.searchResults);
        setSelectedId(null);
    }
    const tabs = TAB_KEYS
        .map((key) => ({ key, count: controller.getTabCount(key) }))
        .filter((tab) => tab.count > 0);
    const resultsForTab = controller.sortedResults[controller.activeTab];
    const selected = resultsForTab.find((item) => item.id === selectedId) ?? resultsForTab[0] ?? null;
    const sortOptions = getSortOptions(controller.activeTab, t);

    if (!controller.isSearching && !controller.hasAnyResults) {
        return (<div className="py-10 text-sm text-muted-foreground">
          {t("translation.migrated.SearchBar.noResultsFoundFor")}“{query}”
        </div>);
    }
    return (<div className="search-layout flex items-start gap-8">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 border-b border-border pb-2">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
            {tabs.map((tab) => (<button
              key={tab.key}
              type="button"
              onClick={() => {
                controller.setActiveTab(tab.key);
                setSelectedId(null);
            }}
              className={cn(
                  "cursor-pointer text-[13px] transition-colors",
                  controller.activeTab === tab.key
                      ? "font-semibold text-primary underline decoration-primary underline-offset-[6px]"
                      : "text-muted-foreground hover:text-foreground",
              )}
            >
              {getPluralTabLabel(tab.key, t)}
              <span className="ml-1 font-mono text-[11px] tabular-nums opacity-75">{tab.count}</span>
            </button>))}
          </div>
          <div className="flex items-center gap-2">
            <Select value={controller.sortOrder} onValueChange={controller.setSortOrder}>
              <SelectTrigger className="h-8 w-fit gap-1.5 bg-background text-xs">
                <ArrowUpDown className="size-3.5 text-muted-foreground"/>
                <SelectValue placeholder={t("translation.common.sortBy")}/>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="default">{t("translation.common.default")}</SelectItem>
                {sortOptions.map((option) => (<SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>))}
              </SelectContent>
            </Select>
          </div>
        </div>

        {controller.isSearching ? (<div className="flex items-center gap-2 py-8 text-sm text-muted-foreground">
          <Spinner />
          <span>{t("translation.searchBar.searching")}</span>
        </div>) : (<div className="artwork-grid mt-6 grid grid-cols-2 gap-4 sm:grid-cols-3 xl:grid-cols-4">
          {resultsForTab.map((item) => (<ArtworkCard
            key={item.id}
            cover={item.images || undefined}
            title={item.name}
            subtitle={getResultSubtitle(item)}
            meta={getResultMeta(item)}
            selected={selected?.id === item.id}
            onClick={() => setSelectedId(item.id)}
            onDoubleClick={() => controller.fetchResult(item.external_urls)}
          />))}
        </div>)}

        {controller.tabHasMore && !controller.isSearching ? (<div className="pt-3">
          <Button variant="outline" size="sm" onClick={() => void controller.loadMore()} disabled={controller.isLoadingMore}>
            {controller.isLoadingMore ? (<Spinner />) : null}
            {t("translation.searchBar.loadMore")}
          </Button>
        </div>) : null}
      </div>

      {selected ? (<InspectorPane
        eyebrow={getSingularTypeLabel(selected.type, t)}
        title={selected.name}
        subtitle={selected.artists || selected.owner || undefined}
        cover={selected.images || undefined}
        rows={buildInspectorRows(selected, t)}
        actions={selected.type === "artist" ? (<Button size="sm" disabled={controller.isSearching} onClick={() => controller.fetchResult(selected.external_urls)}>
            {t("translation.catalog.discography")}
          </Button>) : (<>
          <Button size="sm" disabled={controller.isSearching || requestingDownloads.has(selected.external_urls)} onClick={() => onDownloadResult(selected.external_urls)}>
            {requestingDownloads.has(selected.external_urls) ? <Spinner /> : null}
            {t("translation.downloads.download")}
          </Button>
          <Button size="sm" variant="outline" disabled={controller.isSearching} onClick={() => controller.fetchResult(selected.external_urls)}>
            {t("translation.catalog.chooseTracks")}
          </Button>
        </>)}
      />) : null}
    </div>);
}

function getPluralTabLabel(tab: string, t: (key: string) => string): string {
    switch (tab) {
        case "tracks":
        case "track":
            return t("translation.common.tracks");
        case "albums":
        case "album":
            return t("translation.common.albums");
        case "artists":
        case "artist":
            return t("translation.common.artists");
        case "playlists":
        case "playlist":
            return t("translation.common.playlists");
        default:
            return tab;
    }
}

function getSingularTypeLabel(type: string, t: (key: string) => string): string {
    switch (type) {
        case "track":
            return t("translation.artistInfo.track");
        case "album":
            return t("translation.common.album");
        case "artist":
            return t("translation.common.artist");
        case "playlist":
            return t("translation.playlistInfo.playlist");
        default:
            return type;
    }
}

function getResultSubtitle(item: { artists?: string; owner?: string; album_name?: string; type: string }): string | undefined {
    if (item.type === "artist")
        return undefined;
    return item.artists || item.owner || item.album_name || undefined;
}

function getResultMeta(item: { duration_ms?: number; release_date?: string; total_tracks?: number }): string {
    if (item.duration_ms)
        return formatDuration(item.duration_ms);
    if (item.release_date)
        return item.release_date.slice(0, 4);
    if (item.total_tracks)
        return String(item.total_tracks);
    return "";
}

type TFunc = (key: string, opts?: Record<string, unknown>) => string;

function buildInspectorRows(item: { type: string; artists?: string; owner?: string; release_date?: string; total_tracks?: number; duration_ms?: number; album_name?: string }, t: TFunc) {
    const rows: Array<{ label: string; value: string }> = [];
    if (item.album_name && item.type === "track")
        rows.push({ label: t("translation.common.album"), value: item.album_name });
    if (item.release_date)
        rows.push({ label: t("translation.trackInfo.releaseDate"), value: item.release_date });
    if (item.total_tracks)
        rows.push({ label: t("translation.artistInfo.tracks"), value: item.total_tracks.toLocaleString() });
    if (item.duration_ms)
        rows.push({ label: t("translation.trackList.duration"), value: formatDuration(item.duration_ms) });
    return rows;
}

function getSortOptions(tab: ResultTab, t: TFunc): Array<{ value: string; label: string }> {
    switch (tab) {
        case "tracks":
            return [
                { value: "title-asc", label: t("translation.common.titleZ") },
                { value: "title-desc", label: t("translation.common.titleZ2") },
                { value: "artist-asc", label: t("translation.common.artistZ") },
                { value: "artist-desc", label: t("translation.common.artistZ2") },
                { value: "duration-desc", label: t("translation.searchBar.durationLongest") },
                { value: "duration-asc", label: t("translation.searchBar.durationShortest") },
            ];
        case "albums":
            return [
                { value: "title-asc", label: t("translation.common.titleZ") },
                { value: "title-desc", label: t("translation.common.titleZ2") },
                { value: "artist-asc", label: t("translation.common.artistZ") },
                { value: "artist-desc", label: t("translation.common.artistZ2") },
                { value: "year-desc", label: t("translation.searchBar.yearNewest") },
                { value: "year-asc", label: t("translation.searchBar.yearOldest") },
            ];
        case "artists":
            return [
                { value: "name-asc", label: t("translation.searchBar.nameZ") },
                { value: "name-desc", label: t("translation.searchBar.nameZ2") },
            ];
        case "playlists":
            return [
                { value: "title-asc", label: t("translation.common.titleZ") },
                { value: "title-desc", label: t("translation.common.titleZ2") },
                { value: "owner-asc", label: t("translation.searchBar.ownerZ") },
                { value: "owner-desc", label: t("translation.searchBar.ownerZ2") },
            ];
    }
}
