import { useEffect, useRef, useState } from "react";
import { api, desktopRuntime, errorMessage } from "./api";
import type { ModelWriteInput } from "../types";

export function useCodexModelWrite(
  input: ModelWriteInput,
  enabled: boolean,
  paused: boolean,
  onUpdated?: () => void,
) {
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [written, setWritten] = useState(false);
  const [retry, setRetry] = useState(0);
  const generation = useRef(0);
  const queue = useRef(Promise.resolve());
  const latest = useRef(onUpdated);
  latest.current = onUpdated;
  const signature = JSON.stringify(input);
  const lastWritten = useRef("");
  useEffect(() => {
    const request = ++generation.current;
    setWritten(lastWritten.current === signature);
    setFailure("");
    if (
      !enabled ||
      paused ||
      !desktopRuntime ||
      lastWritten.current === signature
    )
      return () => {
        generation.current++;
      };
    const timer = setTimeout(() => {
      queue.current = queue.current.then(async () => {
        if (request !== generation.current) return;
        setBusy(true);
        try {
          const result = await api.writeModels(
            JSON.parse(signature) as ModelWriteInput,
          );
          if (request !== generation.current) return;
          if (!result.applied)
            throw new Error("当前供应商已切换，请重新打开配置");
          lastWritten.current = signature;
          setWritten(true);
          latest.current?.();
        } catch (error) {
          if (request === generation.current) setFailure(errorMessage(error));
        } finally {
          setBusy(false);
        }
      });
    }, 500);
    return () => {
      clearTimeout(timer);
      generation.current++;
    };
  }, [signature, enabled, paused, retry]);
  return {
    busy,
    failure,
    written: written && enabled,
    retry: () => setRetry((value) => value + 1),
  };
}
