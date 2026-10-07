import { useQuery } from "@tanstack/react-query";
import { api, desktopRuntime } from "./api";

export function useAppUpdate() {
  const source = useQuery({
    queryKey: ["update-source"],
    queryFn: api.updateSource,
    staleTime: Infinity,
    retry: false,
  });
  const check = useQuery({
    queryKey: ["app-update", source.data?.repository],
    queryFn: api.checkUpdate,
    enabled: desktopRuntime && !!source.data?.repository,
    staleTime: 6 * 60 * 60 * 1000,
    refetchInterval: 6 * 60 * 60 * 1000,
    refetchIntervalInBackground: false,
    retry: false,
  });
  return { source, check };
}
export type AppUpdateState = ReturnType<typeof useAppUpdate>;
