import { useEffect, useEffectEvent, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

export function SuccessToast({
  message,
  closeLabel,
  onDismiss,
  returnFocus,
}: Readonly<{
  message: string;
  closeLabel: string;
  onDismiss: () => void;
  returnFocus?: HTMLElement | null;
}>) {
  const element = useRef<HTMLDivElement>(null);
  const previousFocus = useRef<Element | null>(null);
  const remaining = useRef(4000);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [hidden, setHidden] = useState(document.hidden);
  const [leaving, setLeaving] = useState(false);
  const dismiss = useEffectEvent(onDismiss);
  const [container, setContainer] = useState<Element>(document.body);

  useLayoutEffect(() => {
    // Resolve after React commits: a dialog selected during render may be removed by that commit.
    const target = [...document.querySelectorAll("dialog:modal")].at(-1) ?? document.body;
    if (target !== container) {
      setContainer(target);
      return;
    }
    if (element.current?.isConnected && !element.current.matches(":popover-open")) {
      previousFocus.current =
        returnFocus && container.contains(returnFocus) ? returnFocus : document.activeElement;
      element.current.showPopover();
    }
  });
  useEffect(() => {
    const closed = () => dismiss();
    container.addEventListener("close", closed);
    return () => container.removeEventListener("close", closed);
  }, [container]);
  useEffect(() => {
    const changed = () => setHidden(document.hidden);
    document.addEventListener("visibilitychange", changed);
    return () => document.removeEventListener("visibilitychange", changed);
  }, []);
  useEffect(() => {
    if (!leaving) return;
    const timer = window.setTimeout(
      () => dismiss(),
      window.matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 200,
    );
    return () => window.clearTimeout(timer);
  }, [leaving]);
  useEffect(() => {
    if (leaving || hovered || focused || hidden) return;
    const started = performance.now();
    const timer = window.setTimeout(() => setLeaving(true), remaining.current);
    return () => {
      window.clearTimeout(timer);
      remaining.current = Math.max(0, remaining.current - (performance.now() - started));
    };
  }, [leaving, hovered, focused, hidden]);

  return createPortal(
    <div
      ref={element}
      popover="manual"
      className="success-toast"
      data-leaving={leaving}
      onPointerEnter={() => setHovered(true)}
      onPointerLeave={() => setHovered(false)}
      onFocus={() => setFocused(true)}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false);
      }}
    >
      <svg className="toast-icon" viewBox="0 0 16 16" aria-hidden="true">
        <circle cx="8" cy="8" r="7" fill="currentColor" />
        <path
          d="m4.5 8 2.3 2.3 4.7-4.7"
          fill="none"
          stroke="var(--toast-background)"
          strokeWidth="1.5"
        />
      </svg>
      <span role="status" aria-atomic="true">
        {message}
      </span>
      <button
        className="toast-close"
        type="button"
        aria-label={closeLabel}
        onClick={() => {
          const previous = previousFocus.current;
          if (previous instanceof HTMLElement && previous.isConnected)
            previous.focus({ preventScroll: true });
          setLeaving(true);
        }}
      >
        <svg
          width="12"
          height="12"
          viewBox="0 0 24 24"
          aria-hidden="true"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
        >
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
    </div>,
    container,
  );
}
