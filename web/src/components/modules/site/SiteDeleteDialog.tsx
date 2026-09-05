"use client";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { SiteActions } from "./useSiteActions";

export function SiteDeleteDialog({ actions }: { actions: SiteActions }) {
  const {
    deleteConfirm,
    setDeleteConfirm,
    confirmDelete,
    deleteSite,
    deleteSiteAccount,
    archiveSite,
    batchAction,
  } = actions;
  return (
    <Dialog
      open={!!deleteConfirm}
      onOpenChange={(open) => {
        if (!open) setDeleteConfirm(null);
      }}
    >
      <DialogContent className="max-w-md rounded-3xl">
        <DialogHeader>
          <DialogTitle>
            {deleteConfirm?.type === "archive-site" ? "确认归档" : "确认删除"}
          </DialogTitle>
          <DialogDescription>
            {deleteConfirm?.type === "site"
              ? `确认删除站点「${deleteConfirm?.name}」及其所有账号和托管渠道？此操作不可撤销。`
              : deleteConfirm?.type === "archive-site"
                ? `确认归档站点「${deleteConfirm?.name}」？归档后将从主列表移除，托管渠道会被下线，账号和密钥会保留；可在『归档站点』中随时恢复。`
                : deleteConfirm?.type === "batch-site"
                  ? `确认删除已选的 ${deleteConfirm?.name} 个站点及其所有账号和托管渠道？此操作不可撤销。`
                  : `确认删除账号「${deleteConfirm?.name}」及其托管渠道？此操作不可撤销。`}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" className="rounded-xl" onClick={() => setDeleteConfirm(null)}>
            取消
          </Button>
          <Button
            variant="destructive"
            className="rounded-xl"
            onClick={confirmDelete}
            disabled={
              deleteSite.isPending ||
              deleteSiteAccount.isPending ||
              archiveSite.isPending ||
              batchAction.isPending
            }
          >
            {deleteConfirm?.type === "archive-site"
              ? archiveSite.isPending
                ? "归档中..."
                : "确认归档"
              : deleteSite.isPending || deleteSiteAccount.isPending || batchAction.isPending
                ? "删除中..."
                : "确认删除"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
