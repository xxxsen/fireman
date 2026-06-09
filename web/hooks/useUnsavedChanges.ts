"use client";

import { useCallback, useEffect, useState } from "react";
import { setGlobalDirty } from "@/lib/unsavedGuard";

export function useUnsavedChanges() {
  const [dirty, setDirty] = useState(false);

  const markDirty = useCallback(() => {
    setDirty(true);
    setGlobalDirty(true);
  }, []);
  const markClean = useCallback(() => {
    setDirty(false);
    setGlobalDirty(false);
  }, []);

  useEffect(() => {
    setGlobalDirty(dirty);
  }, [dirty]);

  useEffect(() => {
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (!dirty) return;
      e.preventDefault();
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [dirty]);

  const confirmLeave = useCallback((): boolean => {
    if (!dirty) return true;
    return window.confirm("有未保存的修改，确定离开吗？");
  }, [dirty]);

  return { dirty, markDirty, markClean, confirmLeave };
}
