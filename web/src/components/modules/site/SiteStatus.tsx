"use client";

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/animate-ui/components/animate/tooltip";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { type ComponentProps } from "react";
import {
  formatDateTime,
  isCloudflareProtectionMessage,
  statusDotClass,
  statusLabel,
} from "./site-display";

export function SiteMetric({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-lg font-semibold">{value}</div>
    </div>
  );
}

export function CompactMetric({ label, value }: { label: string; value: number | string }) {
  return (
    <span className="inline-flex items-baseline gap-1 text-xs text-muted-foreground">
      <span>{label}</span>
      <span className="font-semibold text-foreground">{value}</span>
    </span>
  );
}

export function ExecutionSummary({
  label,
  status,
  at,
  message,
}: {
  label: string;
  status: string;
  at?: string | null;
  message?: string | null;
}) {
  const text = [`上次${label} ${formatDateTime(at)}`, statusLabel(status)];
  if (message) {
    text.push(message);
  }

  const cloudflareProtected = isCloudflareProtectionMessage(message);
  const summary = text.join(" · ");

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex items-start gap-2 text-xs text-muted-foreground">
          <span
            className={cn(
              "mt-1 size-2 shrink-0 rounded-full",
              cloudflareProtected ? "bg-amber-500" : statusDotClass(status),
            )}
          />
          <span className="min-w-0 truncate">
            {cloudflareProtected ? "Cloudflare 保护 · " : ""}
            {summary}
          </span>
        </div>
      </TooltipTrigger>
      <TooltipContent className="max-w-sm">{summary}</TooltipContent>
    </Tooltip>
  );
}

export function StaticSummary({
  tone = "muted",
  text,
}: {
  tone?: "muted" | "warning";
  text: string;
}) {
  return (
    <div
      className={cn(
        "flex items-start gap-2 text-xs",
        tone === "warning" ? "text-amber-700 dark:text-amber-300" : "text-muted-foreground",
      )}
    >
      <span
        className={cn(
          "mt-1 size-2 shrink-0 rounded-full",
          tone === "warning" ? "bg-amber-500" : "bg-muted-foreground/40",
        )}
      />
      <span className="min-w-0 truncate">{text}</span>
    </div>
  );
}

export function IconActionButton({
  label,
  className,
  ...props
}: ComponentProps<typeof Button> & { label: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          size="icon-sm"
          variant="outline"
          className={cn("rounded-xl", className)}
          aria-label={label}
          title={label}
          {...props}
        />
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
