let dirty = false;

export function setGlobalDirty(value: boolean) {
  dirty = value;
}

export function isGloballyDirty() {
  return dirty;
}

export function confirmLeaveIfDirty(): boolean {
  if (!dirty) return true;
  return window.confirm("有未保存的修改，确定离开吗？");
}
