import { useQuery } from "@tanstack/react-query";
import { api } from "../api";

export const runtimeConfigKeys = {
  all: () => ["runtimes", "config"] as const,
  forRuntime: (runtimeId: string) => [...runtimeConfigKeys.all(), runtimeId] as const,
  readRequest: (runtimeId: string, requestId: string) => [...runtimeConfigKeys.forRuntime(runtimeId), "read", requestId] as const,
};

export function useRuntimeConfigs(runtimeId: string | undefined) {
  return useQuery({
    queryKey: runtimeConfigKeys.forRuntime(runtimeId!),
    queryFn: () => api.getRuntimeConfigs(runtimeId!),
    enabled: !!runtimeId && typeof window !== "undefined",
  });
}
