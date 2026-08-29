/**
 * tw-animate-css class strings for overlays that enter with plain CSS keyframes.
 *
 * These replaced motion's shared-layout (`layoutId`) morph on list rows: the morph
 * had to measure trigger + panel on the main thread on every open, once per rendered
 * row, whereas `enter` only touches opacity/transform and stays on the compositor.
 *
 * Safe to combine with `-translate-x-1/2` centering — Tailwind v4 emits that as the
 * standalone `translate` property, so the keyframe's `transform` does not clobber it.
 */
export const OVERLAY_ENTRANCE = 'animate-in fade-in-0 zoom-in-95 duration-150 ease-out';

/** Backdrops fade only; scaling a full-viewport layer is wasted compositing. */
export const BACKDROP_ENTRANCE = 'animate-in fade-in-0 duration-150 ease-out';

/**
 * For confirm bars that replace a button in place at the right edge of a row. The
 * slide is only geometrically motivated when the overlay is right-anchored; full-bleed
 * `inset-0` overlays should use {@link OVERLAY_ENTRANCE}.
 */
export const OVERLAY_ENTRANCE_FROM_RIGHT = `${OVERLAY_ENTRANCE} slide-in-from-right-1`;
