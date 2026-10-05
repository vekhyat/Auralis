import type { devices, syncengine } from "../../../wailsjs/go/models";

export interface ProgressEvent {
    phase: string;
    op_total?: number;
    done?: number;
    bytes?: number;
    name?: string;
    skipped?: number;
    error?: string;
}

export interface ValidationResult {
    valid: boolean;
    errorKey?: string;
    message?: string;
}

export type SyncTargetKind = "adb" | "drive" | "folder";

export interface SelectionEditorProps {
    rules: syncengine.Selection;
    onChange: (next: syncengine.Selection) => void;
    disabled?: boolean;
}

export interface TargetValidation {
    targetFolder: ValidationResult;
    recentDays: ValidationResult;
    isValid: boolean;
}

export type { devices, syncengine };
