import { Fragment, useEffect, useLayoutEffect, useRef, useState } from "react";
import { useI18n, type MessageKey, type Translate } from "./i18n";

export const navigationPages = [
  { id: "overview", label: "nav.overview", group: "nav.group.resources" },
  { id: "targets", label: "nav.targets", group: "nav.group.resources" },
  { id: "leases", label: "nav.leases", group: "nav.group.resources" },
  { id: "workers", label: "nav.workers", group: "nav.group.resources" },
  { id: "releases", label: "nav.releases", group: "nav.group.resources" },
  { id: "profiles", label: "nav.profiles", group: "nav.group.configuration" },
  { id: "storage", label: "nav.storagePolicies", group: "nav.group.configuration" },
  { id: "network", label: "nav.networkPolicies", group: "nav.group.configuration" },
  { id: "quotas", label: "nav.quotas", group: "nav.group.configuration" },
  { id: "maintenance", label: "nav.maintenance", group: "nav.group.operations" },
] as const satisfies readonly { id: string; label: MessageKey; group: MessageKey }[];
export type Page = (typeof navigationPages)[number]["id"];

const navigationIconPaths: Record<Page | "sidebar" | "search" | "arrow", string> = {
  overview: "M3 13h4l3-8 4 14 3-8h4",
  targets: "M3 3h18v7H3zM3 14h18v7H3zM7 6h.01M7 17h.01",
  leases: "M12 3 3 8v8l9 5 9-5V8zM3 8l9 5 9-5M12 13v8",
  workers: "M7 7h10v10H7zM10 1v6M14 1v6M10 17v6M14 17v6M1 10h6M1 14h6M17 10h6M17 14h6",
  releases: "m3 7 9-4 9 4-9 4zM3 12l9 4 9-4M3 17l9 4 9-4",
  profiles: "M4 3h16v18H4zM8 7h8M8 12h8M8 17h4",
  storage: "M3 4h18v6H3zM3 14h18v6H3zM7 7h.01M7 17h.01",
  network: "M9 3h6v6H9zM2 15h6v6H2zM16 15h6v6h-6zM12 9v3M5 15v-3h14v3",
  quotas: "M3 20V4M3 20h18M7 16v-4M12 16V8M17 16V5",
  maintenance: "M20 11a8 8 0 1 0-2 7M20 4v7h-7",
  search: "M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14M15 15l6 6",
  arrow: "M4 12h16M14 6l6 6-6 6",
  sidebar: "M3 3h18v18H3zM9 3v18",
};

export function NavigationIcon({ name }: { name: Page | "sidebar" | "search" | "arrow" }) {
  return (
    <svg
      width={name === "sidebar" ? 20 : 16}
      height={name === "sidebar" ? 20 : 16}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <path d={navigationIconPaths[name]} />
    </svg>
  );
}

export function matchingNavigation(page: Page, query: string, t: Translate) {
  const words = query.trim().toLocaleLowerCase().split(/\s+/u);
  return navigationPages.filter(
    (entry) =>
      entry.id !== page &&
      words.every((word) =>
        t("command.goTo", { page: t(entry.label) })
          .toLocaleLowerCase()
          .includes(word),
      ),
  );
}

export function ResourceNavigation({
  page,
  counts,
  onNavigate,
  onSearch,
}: {
  page: Page;
  counts: Partial<Record<Page, number>>;
  onNavigate: (page: Page) => void;
  onSearch: () => void;
}) {
  const { t, number } = useI18n();
  const shortcut = /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘ K" : "Ctrl K";
  return (
    <>
      <div className="sidebar-search">
        <button
          type="button"
          className="button outline"
          onClick={onSearch}
          title={t("command.open")}
          aria-label={t("command.open")}
          aria-haspopup="dialog"
        >
          <NavigationIcon name="search" />
          <span className="nav-label">{t("command.search")}</span>
          <kbd>{shortcut}</kbd>
        </button>
      </div>
      <nav aria-label={t("nav.resources")}>
        {(["nav.group.resources", "nav.group.configuration", "nav.group.operations"] as const).map(
          (group, index) => (
            <Fragment key={group}>
              {index > 0 ? <hr /> : null}
              <div className="navigation-group" role="group" aria-label={t(group)}>
                {navigationPages
                  .filter((entry) => entry.group === group)
                  .map((entry) => (
                    <button
                      key={entry.id}
                      type="button"
                      data-page={entry.id}
                      className={page === entry.id ? "active" : ""}
                      aria-current={page === entry.id ? "page" : undefined}
                      onClick={() => onNavigate(entry.id)}
                      title={t(entry.label)}
                    >
                      <NavigationIcon name={entry.id} />
                      <span className="nav-label">{t(entry.label)}</span>
                      {counts[entry.id] !== undefined ? <b>{number(counts[entry.id]!)}</b> : null}
                    </button>
                  ))}
              </div>
            </Fragment>
          ),
        )}
      </nav>
    </>
  );
}

export function NavigationCommands({
  page,
  onNavigate,
  onClose,
}: {
  page: Page;
  onNavigate: (page: Page) => void;
  onClose: () => void;
}) {
  const { t, number } = useI18n();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const matches = matchingNavigation(page, query, t);
  const selected = matches[active] ?? matches[0];
  useLayoutEffect(() => {
    const dialog = dialogRef.current!;
    dialog.showModal();
    dialog.querySelector("input")?.focus();
    return () => dialog.close();
  }, []);
  useEffect(() => {
    dialogRef.current
      ?.querySelector('[aria-selected="true"]')
      ?.scrollIntoView({ block: "nearest" });
  }, [selected?.id]);

  return (
    <dialog
      ref={dialogRef}
      className="navigation-commands"
      aria-label={t("command.title")}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="command-surface">
        <div className="command-input">
          <NavigationIcon name="search" />
          <input
            role="combobox"
            aria-label={t("command.search")}
            aria-expanded="true"
            aria-controls="navigation-command-results"
            aria-autocomplete="list"
            aria-activedescendant={selected ? `command-${selected.id}` : undefined}
            value={query}
            placeholder={t("command.placeholder")}
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => {
              setQuery(event.target.value);
              setActive(0);
            }}
            onKeyDown={(event) => {
              if (event.nativeEvent.isComposing) return;
              if (event.key === "ArrowDown" || event.key === "ArrowUp") {
                event.preventDefault();
                if (matches.length)
                  setActive(
                    (current) =>
                      (current + (event.key === "ArrowDown" ? 1 : matches.length - 1)) %
                      matches.length,
                  );
              } else if (event.key === "Enter") {
                event.preventDefault();
                if (selected) onNavigate(selected.id);
              }
            }}
          />
        </div>
        <div
          id="navigation-command-results"
          role="listbox"
          aria-label={t("command.navigation")}
          className="command-results"
        >
          {matches.length ? (
            <div role="group" aria-label={t("command.navigation")}>
              <div className="command-group-label" aria-hidden="true">
                {t("command.navigation")}
              </div>
              {matches.map((entry, index) => (
                <button
                  key={entry.id}
                  id={`command-${entry.id}`}
                  type="button"
                  role="option"
                  aria-selected={selected?.id === entry.id}
                  tabIndex={-1}
                  className="command-option"
                  onPointerMove={() => setActive(index)}
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => onNavigate(entry.id)}
                >
                  <NavigationIcon name="arrow" />
                  <span>{t("command.goTo", { page: t(entry.label) })}</span>
                </button>
              ))}
            </div>
          ) : (
            <p className="command-empty" role="status">
              {t("command.empty", { query })}
            </p>
          )}
        </div>
        <footer className="command-footer">
          <span>
            <kbd>↑</kbd> <kbd>↓</kbd> {t("command.keyboardHint")}
          </span>
          <span aria-live="polite">
            {t(matches.length === 1 ? "command.resultOne" : "command.results", {
              count: number(matches.length),
            })}
          </span>
        </footer>
      </div>
    </dialog>
  );
}
