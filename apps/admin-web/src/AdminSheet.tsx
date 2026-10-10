import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "./i18n";

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

// A menu button that opens a sheet and returns focus to itself when the sheet closes.
export function SheetTrigger({
  label,
  openOnMount = false,
  onClose,
  children,
}: Readonly<{
  label: string;
  openOnMount?: boolean;
  onClose?: () => void;
  children: (trigger: HTMLElement, close: () => void) => ReactNode;
}>) {
  const button = useRef<HTMLButtonElement>(null);
  const [trigger, setTrigger] = useState<HTMLElement | null>(null);
  useEffect(() => {
    if (openOnMount) setTrigger(button.current);
  }, []);
  return (
    <>
      <button ref={button} type="button" onClick={(event) => setTrigger(event.currentTarget)}>
        {label}
      </button>
      {trigger === null
        ? null
        : children(trigger, () => {
            setTrigger(null);
            onClose?.();
          })}
    </>
  );
}

export function SheetHeading({
  id,
  title,
  subject,
  disabled = false,
  onClose,
}: Readonly<{
  id: string;
  title: ReactNode;
  subject?: ReactNode;
  disabled?: boolean;
  onClose: () => void;
}>) {
  const { t } = useI18n();
  return (
    <div className="sheet-heading">
      <h2 id={id}>{title}</h2>
      <button
        className="icon-button"
        type="button"
        aria-label={t("action.close")}
        disabled={disabled}
        onClick={onClose}
      >
        ×
      </button>
      {subject === undefined ? null : <p>{subject}</p>}
    </div>
  );
}
