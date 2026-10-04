import { t } from "@/i18n";
import { ArtworkCard } from "./ArtworkCard";
import { X } from "lucide-react";
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
    return (<section className="space-y-5">
      <h2 className="text-xl font-semibold tracking-tight">
        {history.length === 1 ? t("translation.migrated.FetchHistory.recentFetch") : t("translation.migrated.FetchHistory.recentFetches")}
      </h2>
      <div className="artwork-grid grid grid-cols-2 gap-5 sm:grid-cols-3 xl:grid-cols-5">
        {history.map((item) => (<ArtworkCard
          key={item.id}
          cover={item.image || undefined}
          title={item.name}
          subtitle={item.artist}
          meta={<span className="flex items-baseline gap-3">
              <span>{getTypeLabel(item.type)}</span>
              <span>{formatRelativeTime(item.timestamp)}</span>
            </span>}
          onClick={() => onSelect(item)}
          trailing={<button
            type="button"
            className="absolute top-4 right-4 flex size-7 cursor-pointer items-center justify-center rounded-full border bg-background text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100 group-focus-within:opacity-100"
            aria-label={t("translation.catalog.remove")}
            onClick={() => onRemove(item.id)}
          >
            <X className="size-3.5" />
          </button>}
        />))}
      </div>
    </section>);
}
