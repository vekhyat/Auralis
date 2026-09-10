import { t, translateMessage } from "@/i18n";
import { useEffect, useRef, useState } from "react";
import { GetPreviewURL } from "@/../wailsjs/go/main/App";
import { getPreviewVolume } from "@/lib/preview";
import { createPreviewPlayback, type PreviewPlayback } from "@/lib/preview-player";
import { toast } from "sonner";
export function usePreview() {
    const [loadingPreview, setLoadingPreview] = useState<string | null>(null);
    const [playingTrack, setPlayingTrack] = useState<string | null>(null);
    const currentPlaybackRef = useRef<PreviewPlayback | null>(null);
    const previewRequestRef = useRef(0);
    const stopCurrentAudio = () => {
        if (!currentPlaybackRef.current) {
            return;
        }
        currentPlaybackRef.current.destroy();
        currentPlaybackRef.current = null;
    };
    useEffect(() => {
        return () => {
            previewRequestRef.current += 1;
            stopCurrentAudio();
        };
    }, []);
    const playPreview = async (trackId: string, trackName: string) => {
        let requestId = 0;
        try {
            const currentAudio = currentPlaybackRef.current?.audio;
            if (playingTrack === trackId && currentAudio) {
                previewRequestRef.current += 1;
                stopCurrentAudio();
                setPlayingTrack(null);
                setLoadingPreview(null);
                return;
            }
            previewRequestRef.current += 1;
            requestId = previewRequestRef.current;
            if (currentAudio) {
                stopCurrentAudio();
                setPlayingTrack(null);
            }
            setLoadingPreview(trackId);
            const previewURL = await GetPreviewURL(trackId);
            if (requestId !== previewRequestRef.current) {
                return;
            }
            if (!previewURL) {
                toast.error(t("translation.download.previewNotAvailable"), {
                    description: t("translation.download.noPreviewFoundValue1", { value1: trackName }),
                });
                setLoadingPreview(null);
                return;
            }
            const playback = await createPreviewPlayback(previewURL, getPreviewVolume());
            if (requestId !== previewRequestRef.current) {
                playback.destroy();
                return;
            }
            const audio = playback.audio;
            const onLoadedData = () => {
                if (requestId !== previewRequestRef.current) {
                    return;
                }
                setLoadingPreview(null);
                setPlayingTrack(trackId);
            };
            const onEnded = () => {
                if (requestId !== previewRequestRef.current) {
                    return;
                }
                if (currentPlaybackRef.current?.audio === audio) {
                    currentPlaybackRef.current.destroy();
                    currentPlaybackRef.current = null;
                    setPlayingTrack(null);
                }
            };
            const onError = () => {
                if (requestId !== previewRequestRef.current) {
                    return;
                }
                toast.error(t("translation.download.failedPlayPreview"), {
                    description: t("translation.download.couldNotPlayPreviewValue1", { value1: trackName }),
                });
                setLoadingPreview(null);
                setPlayingTrack(null);
                if (currentPlaybackRef.current?.audio === audio) {
                    currentPlaybackRef.current.destroy();
                    currentPlaybackRef.current = null;
                }
            };
            audio.addEventListener("loadeddata", onLoadedData);
            audio.addEventListener("ended", onEnded);
            audio.addEventListener("error", onError);
            const innerDestroy = playback.destroy;
            playback.destroy = () => {
                audio.removeEventListener("loadeddata", onLoadedData);
                audio.removeEventListener("ended", onEnded);
                audio.removeEventListener("error", onError);
                innerDestroy();
            };
            currentPlaybackRef.current = playback;
            await audio.play();
        }
        catch (error: unknown) {
            if (requestId !== 0 && requestId !== previewRequestRef.current) {
                return;
            }
            stopCurrentAudio();
            console.error("Preview error:", error);
            toast.error(t("translation.download.previewNotAvailable"), {
                description: error instanceof Error ? translateMessage(error.message) : t("translation.download.couldNotLoadPreviewValue1", { value1: trackName }),
            });
            setLoadingPreview(null);
            setPlayingTrack(null);
        }
    };
    const stopPreview = () => {
        previewRequestRef.current += 1;
        stopCurrentAudio();
        setPlayingTrack(null);
        setLoadingPreview(null);
    };
    return {
        playPreview,
        stopPreview,
        loadingPreview,
        playingTrack,
    };
}
