import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import type { DeploymentTarget } from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { useI18n } from "./i18n";
import { NavigationIcon } from "./navigation";

type Kind = DeploymentTarget["spec"]["targetKind"];
type Phase = DeploymentTarget["spec"]["observedPhase"];
type Option<T extends string> = Readonly<{ value: T; label: string }>;

// Shared by the current filter entry, its two submenus and the two active-condition chips.
function useFilterPopover(submenu = false) {
  const id = useId();
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  useLayoutEffect(() => {
    if (!open) return;
    const position = () => {
      const menu = panel.current;
      const button = trigger.current;
      if (!menu || !button) return;
      const rect = button.getBoundingClientRect();
      const width = menu.offsetWidth;
      const height = menu.offsetHeight;
      let left = submenu ? rect.right + 4 : rect.left;
      if (left + width > innerWidth - 8 && submenu) left = rect.left - width - 4;
      let top = submenu ? rect.top : rect.bottom + 4;
      if (top + height > innerHeight - 8)
        top = submenu ? innerHeight - height - 8 : rect.top - height - 4;
      menu.style.left = `${Math.max(8, Math.min(left, innerWidth - width - 8))}px`;
      menu.style.top = `${Math.max(8, top)}px`;
    };
    position();
    const observer = new ResizeObserver(position);
    if (panel.current) observer.observe(panel.current);
    if (trigger.current?.parentElement) observer.observe(trigger.current.parentElement);
    window.addEventListener("resize", position);
    window.addEventListener("scroll", position, true);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", position);
      window.removeEventListener("scroll", position, true);
    };
  }, [open, submenu]);
  return { id, trigger, panel, open, setOpen };
}

function FilterOptions<T extends string>({
  name,
  label,
  options,
  value,
  onChange,
  onRemove,
  chip = false,
}: {
  name: string;
  label: string;
  options: readonly Option<T>[];
  value: readonly T[];
  onChange: (value: readonly T[]) => void;
  onRemove: () => void;
  chip?: boolean;
}) {
  const { t, number } = useI18n();
  const popover = useFilterPopover(!chip);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const matches = options.filter((option) =>
    option.label.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()),
  );
  const selected = matches[active] ?? matches[0];
  const labels = value.map(
    (entry) => options.find((option) => option.value === entry)?.label ?? entry,
  );
  const summary =
    labels.slice(0, 2).join(", ") + (labels.length > 2 ? `, +${number(labels.length - 2)}` : "");
  const update = (next: readonly T[]) => {
    if (chip && next.length === 0) {
      popover.panel.current?.hidePopover();
      onRemove();
    } else onChange(next);
  };
  const toggle = (entry: T) =>
    update(value.includes(entry) ? value.filter((item) => item !== entry) : [...value, entry]);
  useEffect(() => {
    popover.panel.current
      ?.querySelector('[data-active="true"]')
      ?.scrollIntoView({ block: "nearest" });
  }, [selected?.value, popover.panel]);
  return (
    <div className={chip ? "target-filter-chip" : "target-filter-submenu"} data-filter={name}>
      <button
        ref={popover.trigger}
        type="button"
        popoverTarget={popover.id}
        popoverTargetAction={chip ? "toggle" : "show"}
        role={chip ? undefined : "menuitem"}
        className={chip ? "target-filter-chip-label" : "target-filter-category"}
        aria-haspopup="dialog"
        aria-expanded={popover.open}
        title={chip ? `${label}: ${summary}` : undefined}
        onKeyDown={(event) => {
          if (!chip && event.key === "ArrowRight") {
            event.preventDefault();
            if (!popover.open) event.currentTarget.click();
          }
        }}
        onPointerMove={(event) => {
          if (!chip && event.pointerType === "mouse" && !popover.open) event.currentTarget.click();
        }}
      >
        {!chip ? <NavigationIcon name={name === "kind" ? "targets" : "overview"} /> : null}
        {label}
        {chip ? (
          <>
            : <strong>{summary}</strong>
          </>
        ) : (
          <span className="filter-submenu-arrow" aria-hidden="true">
            ›
          </span>
        )}
      </button>
      {chip ? (
        <button
          className="target-filter-chip-clear"
          type="button"
          aria-label={t("target.filter.remove", { label })}
          onClick={() => {
            popover.panel.current?.hidePopover();
            onRemove();
          }}
        >
          <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
            <path d="m3 3 6 6m0-6-6 6" fill="none" stroke="currentColor" strokeWidth="1.5" />
          </svg>
        </button>
      ) : null}
      <div
        id={popover.id}
        ref={popover.panel}
        popover="auto"
        role="dialog"
        aria-label={label}
        className={`target-filter-options${chip ? " chip-options" : ""}`}
        onBeforeToggle={(event) => {
          if (event.target === event.currentTarget && event.newState === "open") {
            setQuery("");
            setActive(0);
          }
        }}
        onToggle={(event) => {
          if (event.target !== event.currentTarget) return;
          const open = event.newState === "open";
          popover.setOpen(open);
          if (open) {
            event.currentTarget.querySelector("input")?.focus();
          }
        }}
        onKeyDown={(event) => {
          if (
            (event.key === "Escape" || (!chip && event.key === "ArrowLeft")) &&
            !event.nativeEvent.isComposing
          ) {
            event.preventDefault();
            event.stopPropagation();
            popover.panel.current?.hidePopover();
            popover.trigger.current?.focus();
          }
        }}
      >
        <div className="target-filter-search">
          <NavigationIcon name="search" />
          <input
            role="combobox"
            aria-label={t("target.filter.search", { label })}
            aria-expanded={popover.open}
            aria-controls={`${popover.id}-list`}
            aria-autocomplete="list"
            aria-activedescendant={selected ? `${popover.id}-${selected.value}` : undefined}
            placeholder={t("command.search")}
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setActive(0);
            }}
            onKeyDown={(event) => {
              if (event.nativeEvent.isComposing) return;
              if (event.key === "ArrowDown" || event.key === "ArrowUp") {
                event.preventDefault();
                if (matches.length)
                  setActive((current) =>
                    Math.max(
                      0,
                      Math.min(matches.length - 1, current + (event.key === "ArrowDown" ? 1 : -1)),
                    ),
                  );
              } else if (event.key === "Enter") {
                event.preventDefault();
                if (selected) toggle(selected.value);
              }
            }}
          />
          <button type="button" onClick={() => update([])}>
            {t("target.filter.clearGroup")}
          </button>
        </div>
        <div
          id={`${popover.id}-list`}
          role="listbox"
          aria-label={label}
          aria-multiselectable="true"
          className="target-filter-list"
        >
          {matches.map((option, index) => (
            <div
              key={option.value}
              id={`${popover.id}-${option.value}`}
              role="option"
              aria-selected={value.includes(option.value)}
              data-active={selected?.value === option.value}
              data-value={option.value}
              className="target-filter-option"
              onPointerMove={() => setActive(index)}
              onPointerDown={(event) => event.preventDefault()}
              onClick={() => toggle(option.value)}
            >
              <span
                className={`filter-checkbox${value.includes(option.value) ? " checked" : ""}`}
                aria-hidden="true"
              >
                <svg width="14" height="14" viewBox="0 0 16 16">
                  <path d="m3 8 3 3 7-7" fill="none" stroke="currentColor" strokeWidth="1.5" />
                </svg>
              </span>
              {option.label}
            </div>
          ))}
        </div>
        {matches.length === 0 ? (
          <div className="target-filter-empty" role="status">
            {t("command.empty", { query })}
          </div>
        ) : null}
      </div>
    </div>
  );
}

export function TargetFilters({
  kinds,
  phases,
  onKinds,
  onPhases,
}: {
  kinds: readonly Kind[];
  phases: readonly Phase[];
  onKinds: (value: readonly Kind[]) => void;
  onPhases: (value: readonly Phase[]) => void;
}) {
  const { t } = useI18n();
  const popover = useFilterPopover();
  const kindOptions = (["docker", "kubernetes", "ssh"] as const).map((value) => ({
    value,
    label: t(`target.kind.${value}`),
  }));
  const phaseOptions = (["unprobed", "probing", "ready", "unavailable"] as const).map((value) => ({
    value,
    label: t(`phase.${value}`),
  }));
  const options = (chip: boolean) => (
    <>
      {!chip || kinds.length > 0 ? (
        <FilterOptions
          chip={chip}
          name="kind"
          label={t("table.kind")}
          options={kindOptions}
          value={kinds}
          onChange={onKinds}
          onRemove={() => {
            popover.trigger.current?.focus();
            onKinds([]);
          }}
        />
      ) : null}
      {!chip || phases.length > 0 ? (
        <FilterOptions
          chip={chip}
          name="phase"
          label={t("table.status")}
          options={phaseOptions}
          value={phases}
          onChange={onPhases}
          onRemove={() => {
            popover.trigger.current?.focus();
            onPhases([]);
          }}
        />
      ) : null}
    </>
  );
  return (
    <>
      <button
        ref={popover.trigger}
        className="button outline target-filters-trigger"
        type="button"
        popoverTarget={popover.id}
        aria-label={t("target.filter.open")}
        aria-haspopup="menu"
        aria-expanded={popover.open}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          aria-hidden="true"
        >
          <path d="M3 6h18M6 12h12M10 18h4" />
        </svg>
        <span>{t("target.filter.open")}</span>
      </button>
      <div
        id={popover.id}
        ref={popover.panel}
        popover="auto"
        className="target-filter-menu"
        role="menu"
        aria-label={t("target.filter.open")}
        onToggle={(event) => {
          if (event.target !== event.currentTarget) return;
          popover.setOpen(event.newState === "open");
          if (event.newState === "open")
            event.currentTarget
              .querySelector<HTMLButtonElement>(".target-filter-category")
              ?.focus();
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape" && !event.nativeEvent.isComposing) {
            event.preventDefault();
            event.stopPropagation();
            popover.panel.current?.hidePopover();
            popover.trigger.current?.focus();
            return;
          }
          if (
            !(event.target instanceof HTMLElement) ||
            !event.target.matches(".target-filter-category")
          )
            return;
          if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
          event.preventDefault();
          const buttons = [
            ...event.currentTarget.querySelectorAll<HTMLButtonElement>(".target-filter-category"),
          ];
          const index = buttons.indexOf(event.target as HTMLButtonElement);
          const next =
            event.key === "Home"
              ? 0
              : event.key === "End"
                ? buttons.length - 1
                : Math.max(
                    0,
                    Math.min(buttons.length - 1, index + (event.key === "ArrowDown" ? 1 : -1)),
                  );
          buttons[next]?.focus();
        }}
      >
        {options(false)}
      </div>
      {kinds.length || phases.length ? (
        <div className="target-filter-chips" aria-label={t("target.filter.active")}>
          {options(true)}
        </div>
      ) : null}
    </>
  );
}
