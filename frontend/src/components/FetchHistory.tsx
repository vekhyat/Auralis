import { t } from "@/i18n";
import { CatalogRow } from "./CatalogRow";
import { formatRelativeTime } from "@/lib/relative-time";

export interface HistoryItem {
    id: string;
    url: string;
    type: "track" | "album" | "playlist" | "artist";
    name: string;
    artist: string;
    image: string;
    is_explicit?: boolean;
    timestamp: number;
}
interface FetchHistoryProps {
    history: HistoryItem[];
    onSelect: (item: HistoryItem) => void;
    onRemove: (id: string) => void;
}
function getTypeLabel(type: string): string {
    switch (type) {
        case "track":
            return t("translation.artistInfo.track");
        case "album":
            return t("translation.common.album");
        case "playlist":
            return t("translation.playlistInfo.playlist");
        case "artist":
            return t("translation.common.artist");
        default:
            return type;
    }
}
/**
 * Recent fetches as a ruled catalog list: 28px cover, title, artist,
 * type as a word, quiet time. No cards, no type-color chips.
 */
export function FetchHistory({ history, onSelect, onRemove }: FetchHistoryProps) {
    if (history.length === 0)
        return null;
    return (<section className="space-y-1">
      <h2 className="text-[11px] font-semibold tracking-widest uppercase text-muted-foreground">
        {history.length === 1 ? t("translation.migrated.FetchHistory.recentFetch") : t("translation.migrated.FetchHistory.recentFetches")}
      </h2>
      <div className="border-t border-border">
        {history.map((item) => (<CatalogRow
          key={item.id}
          cover={item.image || undefined}
          coverFallback={item.type.slice(0, 2).toUpperCase()}
          title={item.name}
          subtitle={item.artist}
          explicit={item.is_explicit}
          meta={<span className="flex items-baseline gap-3">
              <span>{getTypeLabel(item.type)}</span>
              <span>{formatRelativeTime(item.timestamp)}</span>
            </span>}
          onClick={() => onSelect(item)}
          trailing={<button
            type="button"
            className="cursor-pointer px-2 py-1 text-[11px] text-muted-foreground transition-colors hover:text-destructive"
            aria-label={t("translation.catalog.remove")}
            onClick={() => onRemove(item.id)}
          >
            {t("translation.catalog.remove")}
          </button>}
        />))}
      </div>
    </section>);
}
