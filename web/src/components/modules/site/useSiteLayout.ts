"use client";

import { useJumpStore } from "@/stores/jump";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { estimateVisibleSiteCardHeight } from "./site-summary";
import { SitePendingJump, VisibleSite } from "./types";

export function useSiteLayout(
  visibleSites: VisibleSite[],
  pendingSiteJump: SitePendingJump | null,
) {
  const [expandedSiteIds, setExpandedSiteIds] = useState<Set<number>>(() => new Set());

  const [siteCardHeights, setSiteCardHeights] = useState<Record<number, number>>({});

  const cardObserversRef = useRef<Map<number, ResizeObserver>>(new Map());

  const cardElementsRef = useRef<Map<number, HTMLElement>>(new Map());

  const cardMeasureRefCallbacks = useRef<Map<number, (node: HTMLElement | null) => void>>(
    new Map(),
  );

  const accountElementsRef = useRef<Map<number, HTMLElement>>(new Map());

  const [highlightedSiteId, setHighlightedSiteId] = useState<number | null>(null);

  const [highlightedAccountId, setHighlightedAccountId] = useState<number | null>(null);

  const clearPendingJump = useJumpStore((state) => state.clearPending);

  const setSiteCardMeasureRef = useCallback((siteID: number, node: HTMLElement | null) => {
    const observers = cardObserversRef.current;
    const elements = cardElementsRef.current;
    const currentNode = elements.get(siteID);

    if (currentNode === node) {
      return;
    }

    if (currentNode) {
      observers.get(siteID)?.disconnect();
      observers.delete(siteID);
      elements.delete(siteID);
    }

    if (!node) {
      return;
    }

    elements.set(siteID, node);
    const observer = new ResizeObserver((entries) => {
      const nextHeight = Math.round(
        entries[0]?.contentRect.height ?? node.getBoundingClientRect().height,
      );
      setSiteCardHeights((current) =>
        current[siteID] === nextHeight ? current : { ...current, [siteID]: nextHeight },
      );
    });
    observer.observe(node);
    observers.set(siteID, observer);

    const initialHeight = Math.round(node.getBoundingClientRect().height);
    setSiteCardHeights((current) =>
      current[siteID] === initialHeight ? current : { ...current, [siteID]: initialHeight },
    );
  }, []);

  const getSiteCardMeasureRef = useCallback(
    (siteID: number) => {
      const existing = cardMeasureRefCallbacks.current.get(siteID);
      if (existing) {
        return existing;
      }

      const callback = (node: HTMLElement | null) => {
        setSiteCardMeasureRef(siteID, node);
      };
      cardMeasureRefCallbacks.current.set(siteID, callback);
      return callback;
    },
    [setSiteCardMeasureRef],
  );

  const setAccountElementRef = useCallback((accountId: number, node: HTMLElement | null) => {
    const elements = accountElementsRef.current;
    if (node) {
      elements.set(accountId, node);
      return;
    }
    elements.delete(accountId);
  }, []);

  const flashTarget = useCallback((target: "site" | "account", id: number) => {
    if (target === "site") {
      setHighlightedSiteId(id);
      window.setTimeout(() => {
        setHighlightedSiteId((current) => (current === id ? null : current));
      }, 1800);
      return;
    }

    setHighlightedAccountId(id);
    window.setTimeout(() => {
      setHighlightedAccountId((current) => (current === id ? null : current));
    }, 1800);
  }, []);

  function toggleSiteExpanded(siteId: number, forceExpanded: boolean) {
    if (forceExpanded) return;
    setExpandedSiteIds((current) => {
      const next = new Set(current);
      if (next.has(siteId)) next.delete(siteId);
      else next.add(siteId);
      return next;
    });
  }

  useEffect(() => {
    const observerMap = cardObserversRef.current;
    const elementMap = cardElementsRef.current;
    const callbackMap = cardMeasureRefCallbacks.current;
    const accountMap = accountElementsRef.current;
    return () => {
      for (const observer of observerMap.values()) {
        observer.disconnect();
      }
      observerMap.clear();
      elementMap.clear();
      callbackMap.clear();
      accountMap.clear();
    };
  }, []);

  useEffect(() => {
    if (!pendingSiteJump) return;

    const { requestId, target } = pendingSiteJump;
    const targetSiteId = target.siteId;
    const siteVisible = visibleSites.some((item) => item.site.id === targetSiteId);
    if (!siteVisible) return;

    const node =
      target.kind === "site-account"
        ? accountElementsRef.current.get(target.accountId)
        : cardElementsRef.current.get(target.siteId);
    if (!node) return;

    const timer = window.setTimeout(() => {
      node.scrollIntoView({ behavior: "smooth", block: "center" });
      flashTarget("site", target.siteId);
      if (target.kind === "site-account") {
        setExpandedSiteIds((current) => {
          if (current.has(target.siteId)) return current;
          const next = new Set(current);
          next.add(target.siteId);
          return next;
        });
        flashTarget("account", target.accountId);
      }
      clearPendingJump(requestId);
    }, 80);

    return () => window.clearTimeout(timer);
  }, [pendingSiteJump, visibleSites, clearPendingJump, flashTarget]);

  const masonryColumns = useMemo<[VisibleSite[], VisibleSite[]]>(() => {
    const left: VisibleSite[] = [];
    const right: VisibleSite[] = [];
    let leftHeight = 0;
    let rightHeight = 0;

    for (const item of visibleSites) {
      const isExpanded = item.forceExpanded || expandedSiteIds.has(item.site.id);
      const estimatedHeight =
        siteCardHeights[item.site.id] ?? estimateVisibleSiteCardHeight(item, isExpanded);
      if (leftHeight <= rightHeight) {
        left.push(item);
        leftHeight += estimatedHeight;
      } else {
        right.push(item);
        rightHeight += estimatedHeight;
      }
    }

    return [left, right];
  }, [visibleSites, expandedSiteIds, siteCardHeights]);

  return {
    expandedSiteIds,
    setExpandedSiteIds,
    getSiteCardMeasureRef,
    setAccountElementRef,
    highlightedSiteId,
    highlightedAccountId,
    toggleSiteExpanded,
    masonryColumns,
  };
}

export type SiteLayout = ReturnType<typeof useSiteLayout>;
