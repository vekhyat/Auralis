import { useRef } from "react";
import { t } from "@/i18n";
import { Clipboard, X } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";

export const OMNIBAR_PLACEHOLDER_KEY = "translation.omnibar.placeholder";

interface OmnibarSearchProps {
    value: string;
    busy?: boolean;
    onChange: (value: string) => void;
    onSubmit: () => void;
    className?: string;
}

/**
 * The primary control of the product: one field that accepts a pasted
 * streaming link or a plain search. Enter commits. No Fetch chrome —
 * validation and dialogs live in the smart-search controller.
 */
export function OmnibarSearch({ value, busy = false, onChange, onSubmit, className }: OmnibarSearchProps) {
    const inputRef = useRef<HTMLInputElement | null>(null);
    const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
        if (event.key === "Enter") {
            event.preventDefault();
            onSubmit();
            inputRef.current?.blur();
        }
        else if (event.key === "Escape") {
            inputRef.current?.blur();
        }
    };
    const handlePasteFromClipboard = async () => {
        try {
            const clipboardText = (await navigator.clipboard.readText()).trim();
            if (clipboardText) {
                onChange(clipboardText);
                inputRef.current?.focus();
            }
        }
        catch (error) {
            console.error("Failed to read clipboard:", error);
        }
    };
    return (<div className={cn("relative flex h-8 min-w-0 items-center", className)}>
      <input
        ref={inputRef}
        id="spotify-smart-search"
        type="text"
        spellCheck={false}
        autoComplete="off"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={handleKeyDown}
        placeholder={t(OMNIBAR_PLACEHOLDER_KEY)}
        aria-label={t(OMNIBAR_PLACEHOLDER_KEY)}
        className="h-full w-full rounded-[2px] border border-input bg-card pr-14 pl-3 text-[13px] text-foreground transition-colors outline-none placeholder:text-muted-foreground focus:border-primary focus:ring-1 focus:ring-ring/40"
      />
      <div className="absolute top-0 right-1.5 flex h-full items-center gap-0.5">
        {busy ? (<Spinner className="mr-1"/>) : null}
        {value && !busy ? (<button
          type="button"
          tabIndex={-1}
          className="flex size-6 cursor-pointer items-center justify-center rounded-[2px] text-muted-foreground transition-colors hover:text-foreground"
          aria-label={t("translation.migrated.SearchBar.clearSearchInput")}
          onClick={() => onChange("")}
        >
          <X className="size-3.5"/>
        </button>) : null}
        <button
          type="button"
          tabIndex={-1}
          className="flex size-6 cursor-pointer items-center justify-center rounded-[2px] text-muted-foreground transition-colors hover:text-foreground"
          aria-label={t("translation.searchBar.pasteClipboard")}
          onClick={() => void handlePasteFromClipboard()}
        >
          <Clipboard className="size-3.5"/>
        </button>
      </div>
    </div>);
}
