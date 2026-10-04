import { lazy } from "react";

/**
 * Same contract as React.lazy, called again on retry.
 * A rejected lazy component keeps its failed promise, so each attempt needs a new one.
 */
export const createLazyPage: typeof lazy = (load) => lazy(load);
