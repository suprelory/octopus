import { useMutation, useQueryClient } from "@tanstack/react-query";
import { authenticatedFetch } from "../client";
import { logger } from "@/lib/logger";
import { AllAPIHubImportResult, MetAPIImportResult } from './site-types';
import { invalidateSiteQueries } from './site-query';

function extractResponseMessage(payload: unknown, fallback: string) {
  if (
    payload &&
    typeof payload === "object" &&
    "message" in payload &&
    typeof payload.message === "string"
  ) {
    return payload.message;
  }
  return fallback;
}

function extractResponseData<T>(payload: unknown): T | undefined {
  if (payload && typeof payload === "object" && "data" in payload) {
    return (payload as { data?: T }).data;
  }
  return undefined;
}

export function useImportAllAPIHub() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (payload: { file?: File | null; text?: string }) => {
      const hasFile = !!payload.file;
      const hasText = !!payload.text?.trim();
      if (!hasFile && !hasText) {
        throw new Error("请选择 JSON 文件或粘贴导出内容");
      }

      const headers: Record<string, string> = {};
      let body: BodyInit;

      if (payload.file) {
        const form = new FormData();
        form.append("file", payload.file);
        body = form;
      } else {
        headers["Content-Type"] = "application/json";
        body = payload.text!.trim();
      }

      return authenticatedFetch(
        '/api/v1/site/import/all-api-hub',
        {
          method: "POST",
          headers,
          body,
        },
        async (response) => {
          const contentType = response.headers.get("content-type") || "";
          const data = contentType.includes("application/json")
            ? await response.json()
            : await response.text();

          if (!response.ok) {
            throw new Error(
              extractResponseMessage(
                data,
                typeof data === "string" ? data : response.statusText,
              ),
            );
          }

          const result =
            extractResponseData<AllAPIHubImportResult>(data) ??
            (data as AllAPIHubImportResult);
          return {
            ...result,
            warnings: result.warnings ?? [],
          };
        },
      );
    },
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("导入 All API Hub 账号失败:", error),
  });
}

export function useImportMetAPI() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (payload: { file?: File | null; text?: string }) => {
      const hasFile = !!payload.file;
      const hasText = !!payload.text?.trim();
      if (!hasFile && !hasText) {
        throw new Error("请选择 JSON 文件或粘贴导出内容");
      }

      const headers: Record<string, string> = {};
      let body: BodyInit;

      if (payload.file) {
        const form = new FormData();
        form.append("file", payload.file);
        body = form;
      } else {
        headers["Content-Type"] = "application/json";
        body = payload.text!.trim();
      }

      return authenticatedFetch('/api/v1/site/import/metapi', {
        method: "POST",
        headers,
        body,
      }, async (response) => {
        const contentType = response.headers.get("content-type") || "";
        const data = contentType.includes("application/json")
          ? await response.json()
          : await response.text();

        if (!response.ok) {
          throw new Error(
            extractResponseMessage(
              data,
              typeof data === "string" ? data : response.statusText,
            ),
          );
        }

        const result =
          extractResponseData<MetAPIImportResult>(data) ??
          (data as MetAPIImportResult);
        return {
          ...result,
          warnings: result.warnings ?? [],
        };
      });
    },
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("导入 metapi 站点失败:", error),
  });
}
