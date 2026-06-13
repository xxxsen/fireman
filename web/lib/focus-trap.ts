const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function getFocusableElements(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
    (el) =>
      !el.hasAttribute("disabled") &&
      el.getAttribute("aria-hidden") !== "true" &&
      el.getAttribute("aria-disabled") !== "true" &&
      el.tabIndex >= 0,
  );
}

export function handleFocusTrapKeyDown(
  event: KeyboardEvent,
  container: HTMLElement,
): void {
  if (event.key !== "Tab") return;

  const focusable = getFocusableElements(container);
  if (focusable.length === 0) {
    event.preventDefault();
    return;
  }

  const first = focusable[0]!;
  const last = focusable[focusable.length - 1]!;
  const active = document.activeElement as HTMLElement;
  const activeIndex = focusable.indexOf(active);

  event.preventDefault();

  if (event.shiftKey) {
    if (activeIndex <= 0) {
      last.focus();
      return;
    }
    focusable[activeIndex - 1]!.focus();
    return;
  }

  if (activeIndex === -1 || activeIndex >= focusable.length - 1) {
    first.focus();
    return;
  }
  focusable[activeIndex + 1]!.focus();
}
