'use client';

import type { LogSiteActionTarget } from '@/api/endpoints/log';
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from '@/components/ui/alert-dialog';

export function LogDisableDialog({
    target,
    open,
    pending,
    onOpenChange,
    onConfirm,
}: {
    target: LogSiteActionTarget | null;
    open: boolean;
    pending: boolean;
    onOpenChange: (open: boolean) => void;
    onConfirm: () => void;
}) {
    if (!target?.can_disable_model) return null;

    return (
        <AlertDialog open={open} onOpenChange={onOpenChange}>
            <AlertDialogContent>
                <AlertDialogHeader>
                    <AlertDialogTitle>确认禁用站点模型</AlertDialogTitle>
                    <AlertDialogDescription>
                        将在 {target.site_name} / {target.account_name} / {target.group_name} 中禁用模型 {target.model_name}。
                        禁用后对应投影渠道和分组会刷新为最新状态。
                    </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                    <AlertDialogCancel disabled={pending}>取消</AlertDialogCancel>
                    <AlertDialogAction onClick={onConfirm} disabled={pending} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
                        {pending ? '禁用中...' : '确认禁用'}
                    </AlertDialogAction>
                </AlertDialogFooter>
            </AlertDialogContent>
        </AlertDialog>
    );
}
