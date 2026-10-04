export interface ParsedHistoryPayload {
    ok: boolean;
    items: unknown[];
}

export interface LegacyHistoryPlan<T> {
    items: T[] | null;
    saveItems: T[] | null;
    hadLegacy: boolean;
    legacyParseOk: boolean;
    persistedReadOk: boolean;
    persistedParseOk: boolean;
}

export function parseHistoryPayload(raw: string | null): ParsedHistoryPayload {
    if (raw == null || raw.trim() === "") {
        return { ok: true, items: [] };
    }
    try {
        const parsed: unknown = JSON.parse(raw);
        if (!Array.isArray(parsed)) {
            return { ok: false, items: [] };
        }
        return { ok: true, items: parsed };
    }
    catch {
        return { ok: false, items: [] };
    }
}

/**
 * Merge legacy localStorage history into the backend list.
 * saveItems is set only when both sides parsed, so a later failed save
 * cannot be mistaken for a confirmed migration.
 */
export function planLegacyHistoryMigration<T>(input: {
    legacyRaw: string | null;
    persistedRaw: string | null;
    persistedReadOk: boolean;
    normalize: (items: unknown[]) => T[];
}): LegacyHistoryPlan<T> {
    const hadLegacy = input.legacyRaw != null && input.legacyRaw.trim() !== "";
    const legacy = parseHistoryPayload(hadLegacy ? input.legacyRaw : null);
    const legacyParseOk = !hadLegacy || legacy.ok;
    const persisted = input.persistedReadOk
        ? parseHistoryPayload(input.persistedRaw)
        : { ok: false, items: [] as unknown[] };
    const persistedParseOk = input.persistedReadOk && persisted.ok;
    const plan: LegacyHistoryPlan<T> = {
        items: null,
        saveItems: null,
        hadLegacy,
        legacyParseOk,
        persistedReadOk: input.persistedReadOk,
        persistedParseOk,
    };
    if (!input.persistedReadOk || !persisted.ok) {
        if (hadLegacy && legacy.ok) {
            plan.items = input.normalize(legacy.items);
        }
        return plan;
    }
    if (hadLegacy && !legacy.ok) {
        plan.items = input.normalize(persisted.items);
        return plan;
    }
    const merged = input.normalize([...persisted.items, ...(hadLegacy ? legacy.items : [])]);
    plan.items = merged;
    plan.saveItems = merged;
    return plan;
}

export function shouldDiscardLegacyHistory(plan: {
    hadLegacy: boolean;
    legacyParseOk: boolean;
    persistedReadOk: boolean;
    persistedParseOk: boolean;
    saveConfirmed: boolean;
}): boolean {
    return plan.hadLegacy
        && plan.legacyParseOk
        && plan.persistedReadOk
        && plan.persistedParseOk
        && plan.saveConfirmed;
}
