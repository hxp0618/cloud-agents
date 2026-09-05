import { useEffect, useRef, useState, type ReactNode } from "react";

export function AdminSidebar({
  open,
  onOpenChange,
  label,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  label: string;
  children: ReactNode;
}) {
  const [mobile, setMobile] = useState(() => window.matchMedia("(max-width: 767px)").matches);
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const media = window.matchMedia("(max-width: 767px)");
    const resize = () => {
      setMobile(media.matches);
      onOpenChange(false);
    };
    media.addEventListener("change", resize);
    return () => media.removeEventListener("change", resize);
  }, [onOpenChange]);

  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    if (open) {
      dialog.showModal();
      dialog.querySelector<HTMLElement>("nav button.active")?.focus();
    } else {
      dialog.close();
    }
    return () => dialog.close();
  }, [open, mobile]);

  const sidebar = <aside className="sidebar">{children}</aside>;
  if (!mobile) return sidebar;
  return (
    <dialog
      ref={ref}
      className="mobile-nav-dialog"
      aria-label={label}
      onCancel={(event) => {
        event.preventDefault();
        onOpenChange(false);
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onOpenChange(false);
      }}
    >
      {sidebar}
    </dialog>
  );
}
