import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, } from "@/components/ui/dialog";
import type { SmartSearchController } from "@/hooks/smart-search-core";

/** Ruled paper dialogs for unsupported links; rendered once at the app root. */
export function SmartSearchDialogs({ controller }: {
    controller: Pick<SmartSearchController, "showInvalidUrlDialog" | "setShowInvalidUrlDialog" | "showNextDialog" | "setShowNextDialog" | "invalidUrl" | "resetNextDialogPrompt">;
}) {
    const { t } = useTranslation();
    const { invalidUrl } = controller;
    const invalidBlock = invalidUrl ? (<div className="border bg-muted/40 px-3 py-2 font-mono text-xs break-all text-muted-foreground">
      {invalidUrl}
    </div>) : null;
    const description = t("translation.migrated.SearchBar.pasteASpotifyLinkOrEnterPlain");
    return (<>
      <Dialog open={controller.showInvalidUrlDialog} onOpenChange={controller.setShowInvalidUrlDialog}>
        <DialogContent className="sm:max-w-106.25">
          <DialogHeader>
            <DialogTitle>{t("translation.migrated.SearchBar.unsupportedLink")}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>
          {invalidBlock}
          <DialogFooter>
            <Button onClick={() => controller.setShowInvalidUrlDialog(false)}>
              {t("translation.common.close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={controller.showNextDialog} onOpenChange={(open) => {
            if (!open) {
                controller.resetNextDialogPrompt();
            }
            controller.setShowNextDialog(open);
        }}>
        <DialogContent className="sm:max-w-115 [&>button]:hidden">
          <DialogHeader>
            <DialogTitle>{t("translation.migrated.SearchBar.unsupportedLink")}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>
          {invalidBlock}
          <DialogFooter>
            <Button onClick={() => {
                controller.resetNextDialogPrompt();
                controller.setShowNextDialog(false);
            }}>
              {t("translation.common.close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>);
}
