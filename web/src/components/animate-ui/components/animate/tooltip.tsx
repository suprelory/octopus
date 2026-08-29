import * as React from 'react';

import {
  TooltipProvider as TooltipProviderPrimitive,
  Tooltip as TooltipPrimitive,
  TooltipTrigger as TooltipTriggerPrimitive,
  TooltipContent as TooltipContentPrimitive,
  TooltipArrow as TooltipArrowPrimitive,
  type TooltipProviderProps as TooltipProviderPrimitiveProps,
  type TooltipProps as TooltipPrimitiveProps,
  type TooltipTriggerProps as TooltipTriggerPrimitiveProps,
  type TooltipContentProps as TooltipContentPrimitiveProps,
} from '@/components/animate-ui/primitives/animate/tooltip';
import { cn } from '@/lib/utils';

type TooltipProviderProps = TooltipProviderPrimitiveProps;

function TooltipProvider({ openDelay = 0, ...props }: TooltipProviderProps) {
  return <TooltipProviderPrimitive openDelay={openDelay} {...props} />;
}

type TooltipProps = TooltipPrimitiveProps;

function Tooltip({ sideOffset = 10, ...props }: TooltipProps) {
  return <TooltipPrimitive sideOffset={sideOffset} {...props} />;
}

type TooltipTriggerProps = TooltipTriggerPrimitiveProps;

function TooltipTrigger({ ...props }: TooltipTriggerProps) {
  return <TooltipTriggerPrimitive {...props} />;
}

type TooltipContentProps = Omit<TooltipContentPrimitiveProps, 'asChild'> & {
  children: React.ReactNode;
};

function TooltipContent({
  className,
  children,
  ...props
}: TooltipContentProps) {
  return (
    <TooltipContentPrimitive
      className={cn(
        'z-50 w-fit bg-muted text-muted-foreground  rounded-md',
        className,
      )}
      {...props}
    >
      {/*
        Plain divs: the inner one previously carried `layout="preserve-aspect"`,
        which mounts a projection node per tooltip and re-measures on update. The
        content is static once shown, and the shared `layoutId` on the overlay
        already animates the box between triggers.
      */}
      <div className="overflow-hidden px-3 py-1.5 text-xs text-balance">
        <div>{children}</div>
      </div>
      <TooltipArrowPrimitive
        className="fill-muted size-3 data-[side='bottom']:translate-y-[1px] data-[side='right']:translate-x-[1px] data-[side='left']:translate-x-[-1px] data-[side='top']:translate-y-[-1px]"
        tipRadius={2}
      />
    </TooltipContentPrimitive>
  );
}

export {
  TooltipProvider,
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  type TooltipProviderProps,
  type TooltipProps,
  type TooltipTriggerProps,
  type TooltipContentProps,
};
