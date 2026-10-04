import { toast } from "sonner";
import { playSuccessSound, playErrorSound, playWarningSound, playInfoSound, } from "./audio";
import { logger } from "./logger";
import { getSettings } from "./settings";
const toastStyle = {
    className: "font-mono",
};
// Toasts that offer an action stay long enough to reach it.
const ACTION_TOAST_DURATION_MS = 5000;
type ToastData = Parameters<typeof toast.success>[1];
const isSfxEnabled = () => getSettings().sfxEnabled;
// Capitalize the first letter only; titles, artists, and names keep their case.
const asSentence = (message: string) => message.charAt(0).toLocaleUpperCase() + message.slice(1);
const withStyle = (data?: ToastData): ToastData => ({
    ...toastStyle,
    ...(data?.action && data.duration === undefined ? { duration: ACTION_TOAST_DURATION_MS } : {}),
    ...data,
});
export const toastWithSound = {
    success: (message: string, data?: ToastData) => {
        const msg = asSentence(message);
        logger.success(msg);
        if (isSfxEnabled())
            playSuccessSound();
        return toast.success(msg, withStyle(data));
    },
    error: (message: string, data?: ToastData) => {
        const msg = asSentence(message);
        logger.error(msg);
        if (isSfxEnabled())
            playErrorSound();
        return toast.error(msg, withStyle(data));
    },
    warning: (message: string, data?: ToastData) => {
        const msg = asSentence(message);
        logger.warning(msg);
        if (isSfxEnabled())
            playWarningSound();
        return toast.warning(msg, withStyle(data));
    },
    info: (message: string, data?: ToastData) => {
        const msg = asSentence(message);
        logger.info(msg);
        if (isSfxEnabled())
            playInfoSound();
        return toast.info(msg, withStyle(data));
    },
    message: (message: string, data?: ToastData) => {
        const msg = asSentence(message);
        logger.info(msg);
        if (isSfxEnabled())
            playInfoSound();
        return toast(msg, withStyle(data));
    },
    silentInfo: (message: string, data?: ToastData) => {
        const msg = asSentence(message);
        logger.info(msg);
        return toast.info(msg, withStyle(data));
    },
    dismiss: (id?: string | number) => toast.dismiss(id),
    toast: toast,
};
