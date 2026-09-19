import { useLayoutEffect, useRef, type ReactNode } from "react";

export function AdminSheet({
  label,
  onClose,
  children,
  confirmation = false,
  returnFocus,
  feedback,
}: Readonly<{
  label: string;
  onClose: () => void;
  children: ReactNode;
  confirmation?: boolean;
  returnFocus?: HTMLElement | null;
  feedback: ReactNode;
}>) {
  const ref = useRef<HTMLDialogElement>(null);
  const triggerRef = useRef(returnFocus);

  useLayoutEffect(() => {
    const dialog = ref.current;
    if (dialog === null) return;
    if (!dialog.open) {
      dialog.showModal();
      dialog.querySelector<HTMLElement>("[data-sheet-autofocus]")?.focus();
    }
    return () => {
      // Close before DOM removal so native focus restoration can return to the trigger.
      if (dialog.open) dialog.close();
      // Async previews disable their trigger before opening, so the native dialog cannot capture it.
      if (triggerRef.current?.isConnected) triggerRef.current.focus();
    };
  }, []);

  return (
    <dialog
      ref={ref}
      className={`admin-sheet${confirmation ? " confirmation-dialog" : ""}`}
      aria-label={label}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      {children}
      {feedback}
    </dialog>
  );
}
