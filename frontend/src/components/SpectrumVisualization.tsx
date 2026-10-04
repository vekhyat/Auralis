import { t } from "@/i18n";
import { useEffect, useRef, useState, forwardRef, useImperativeHandle } from "react";
import type { SpectrumData } from "@/types/api";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { loadAudioAnalysisPreferences, saveAudioAnalysisPreferences, type AnalyzerColorScheme, type AnalyzerFreqScale, type AnalyzerWindowFunction, } from "@/lib/audio-analysis-preferences";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, } from "@/components/ui/select";
export interface SpectrumVisualizationHandle {
    getCanvasDataURL: () => string | null;
}
type ColorScheme = AnalyzerColorScheme;
type FreqScale = AnalyzerFreqScale;
type WindowFunction = AnalyzerWindowFunction;
interface SpectrumVisualizationProps {
    sampleRate: number;
    duration: number;
    spectrumData?: SpectrumData;
    fileName?: string;
    onReAnalyze?: (fftSize: number, windowFunction: string) => void;
    isAnalyzingSpectrum?: boolean;
    spectrumProgress?: {
        percent: number;
        message: string;
    };
}
import { CANVAS_W, CANVAS_H, renderSpectrogramToCanvas } from "@/lib/spectrogram-renderer";
const COLOR_SCHEMES: {
    value: ColorScheme;
    label: string;
    gradient: string;
}[] = [
    { value: "spek", label: t("literal.spectrumVisualization.spek"), gradient: "linear-gradient(to right, #0f0040, #1e0080, #4000ff, #8000ff, #ff0080, #ff4000, #ff8000, #ffff00)" },
    { value: "viridis", label: t("literal.spectrumVisualization.viridis"), gradient: "linear-gradient(to right, #440154, #31688e, #35b779, #fde725)" },
    { value: "hot", label: t("literal.spectrumVisualization.hot"), gradient: "linear-gradient(to right, #000000, #ff0000, #ffff00, #ffffff)" },
    { value: "cool", label: t("literal.spectrumVisualization.cool"), gradient: "linear-gradient(to right, #000080, #0000ff, #00ffff, #ffffff)" },
    { value: "grayscale", label: t("literal.spectrumVisualization.grayscale"), gradient: "linear-gradient(to right, #000000, #ffffff)" },
];
export const SpectrumVisualization = forwardRef<SpectrumVisualizationHandle, SpectrumVisualizationProps>(({ sampleRate, duration, spectrumData, fileName, onReAnalyze, isAnalyzingSpectrum, spectrumProgress, }, ref) => {
    const canvasRef = useRef<HTMLCanvasElement>(null);
    const preferencesRef = useRef(loadAudioAnalysisPreferences());
    useImperativeHandle(ref, () => ({
        getCanvasDataURL: () => {
            if (!canvasRef.current)
                return null;
            return canvasRef.current.toDataURL("image/png");
        },
    }));
    const [freqScale, setFreqScale] = useState<FreqScale>(preferencesRef.current.freqScale);
    const [colorScheme, setColorScheme] = useState<ColorScheme>(preferencesRef.current.colorScheme);
    const [fftSize, setFftSize] = useState<string>(() => String(preferencesRef.current.fftSize));
    const [windowFunction, setWindowFunction] = useState<WindowFunction>(preferencesRef.current.windowFunction);
    useEffect(() => {
        if (spectrumData?.freq_bins) {
            setFftSize(String((spectrumData.freq_bins - 1) * 2));
        }
    }, [spectrumData]);
    useEffect(() => {
        saveAudioAnalysisPreferences({
            colorScheme,
            freqScale,
            fftSize: Number(fftSize),
            windowFunction,
        });
    }, [colorScheme, freqScale, fftSize, windowFunction]);
    useEffect(() => {
        const canvas = canvasRef.current;
        if (!canvas)
            return;
        const ctx = canvas.getContext("2d");
        if (!ctx)
            return;
        let canceled = false;
        const shouldCancel = () => canceled;
        if (spectrumData) {
            void renderSpectrogramToCanvas(canvas, {
                spectrumData,
                sampleRate,
                duration,
                freqScale,
                colorScheme,
                fileName,
                shouldCancel,
            });
        }
        else {
            ctx.fillStyle = "#000000";
            ctx.fillRect(0, 0, CANVAS_W, CANVAS_H);
            ctx.fillStyle = "#444444";
            ctx.font = "16px Arial";
            ctx.textAlign = "center";
            ctx.fillText("No spectrum data", CANVAS_W / 2, CANVAS_H / 2);
        }
        return () => {
            canceled = true;
        };
    }, [spectrumData, sampleRate, duration, freqScale, colorScheme, fileName]);
    const handleReAnalyze = (newFftSize: string, newWindowFunc: string) => {
        setFftSize(newFftSize);
        setWindowFunction(newWindowFunc as WindowFunction);
        if (onReAnalyze) {
            onReAnalyze(parseInt(newFftSize, 10), newWindowFunc);
        }
    };
    const spectrumPercent = Math.round(Math.max(0, Math.min(100, spectrumProgress?.percent ?? 0)));
    return (<div className="space-y-4">
            <div className="flex flex-wrap items-center gap-3 sm:gap-4 p-1">
                <div className="flex items-center gap-2">
                    <Label className="whitespace-nowrap text-sm font-medium">{t("translation.spectrumVisualization.colorScheme")}</Label>
                    <Select value={colorScheme} onValueChange={(v) => setColorScheme(v as ColorScheme)} disabled={isAnalyzingSpectrum}>
                        <SelectTrigger className="h-8 w-[130px] text-sm">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {COLOR_SCHEMES.map((scheme) => (<SelectItem key={scheme.value} value={scheme.value}>
                                    <div className="flex items-center gap-2">
                                        <div className="h-4 w-4 rounded-sm border opacity-90" style={{ backgroundImage: scheme.gradient }}/>
                                        <span>{scheme.label}</span>
                                    </div>
                                </SelectItem>))}
                        </SelectContent>
                    </Select>
                </div>

                <div className="h-6 w-px bg-border hidden sm:block mx-1"></div>

                <div className="flex items-center gap-2">
                    <Label className="whitespace-nowrap text-sm font-medium">{t("translation.spectrumVisualization.freqScale")}</Label>
                    <Select value={freqScale} onValueChange={(v) => setFreqScale(v as FreqScale)} disabled={isAnalyzingSpectrum}>
                        <SelectTrigger className="h-8 w-[95px] text-sm">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="linear">{t("literal.spectrumVisualization.linear")}</SelectItem>
                            <SelectItem value="log2">{t("literal.spectrumVisualization.log2")}</SelectItem>
                        </SelectContent>
                    </Select>
                </div>

                <div className="flex items-center gap-2">
                    <Label className="whitespace-nowrap text-sm font-medium">{t("translation.common.fftSize")}</Label>
                    <Select value={fftSize} onValueChange={(v) => handleReAnalyze(v, windowFunction)} disabled={isAnalyzingSpectrum}>
                        <SelectTrigger className="h-8 w-[90px] text-sm">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="512">512</SelectItem>
                            <SelectItem value="1024">1024</SelectItem>
                            <SelectItem value="2048">2048</SelectItem>
                            <SelectItem value="4096">4096</SelectItem>
                        </SelectContent>
                    </Select>
                </div>

                <div className="flex items-center gap-2">
                    <Label className="whitespace-nowrap text-sm font-medium">{t("translation.spectrumVisualization.window")}</Label>
                    <Select value={windowFunction} onValueChange={(v) => handleReAnalyze(fftSize, v)} disabled={isAnalyzingSpectrum}>
                        <SelectTrigger className="h-8 w-[120px] text-sm capitalize">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="hann">{t("literal.spectrumVisualization.hann")}</SelectItem>
                            <SelectItem value="hamming">{t("literal.spectrumVisualization.hamming")}</SelectItem>
                            <SelectItem value="blackman">{t("literal.spectrumVisualization.blackman")}</SelectItem>
                            <SelectItem value="rectangular">{t("literal.spectrumVisualization.rectangular")}</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
            </div>

            <div className="relative border border-white/10 rounded-lg overflow-hidden bg-black shadow-xl">
                {isAnalyzingSpectrum && (<div className="absolute inset-0 z-10 grid place-items-center bg-black/60 backdrop-blur-sm">
                        <div className="w-full max-w-xs space-y-2 px-4">
                            <div className="flex items-center justify-between text-sm text-foreground/90">
                                <span>{t("translation.spectrumVisualization.processing")}</span>
                                <span className="tabular-nums">{spectrumPercent}%</span>
                            </div>
                            <Progress value={spectrumPercent} className="h-2 w-full"/>
                        </div>
                    </div>)}
                <canvas ref={canvasRef} width={CANVAS_W} height={CANVAS_H} className="w-full h-auto" style={{ imageRendering: "auto" }}/>
            </div>
        </div>);
});
