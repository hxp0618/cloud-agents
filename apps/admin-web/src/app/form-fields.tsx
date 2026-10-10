import { useLayoutEffect, useRef, type ReactNode } from "react";
import { targetIdentifierPattern } from "../admin";
import { useI18n } from "../i18n";

const namePattern = new RegExp(`^(?:${targetIdentifierPattern})$`, "u");

export function NameField({
  label,
  value,
  onChange,
  takenMessage = "",
  help,
  placeholder,
  disabled = false,
  autoFocus = false,
}: Readonly<{
  label: string;
  value: string;
  onChange: (value: string) => void;
  takenMessage?: string;
  help?: string;
  placeholder?: string;
  disabled?: boolean;
  autoFocus?: boolean;
}>) {
  const { t } = useI18n();
  const input = useRef<HTMLInputElement>(null);
  useLayoutEffect(() => input.current?.setCustomValidity(takenMessage), [takenMessage]);
  const error =
    takenMessage || (value === "" || namePattern.test(value) ? "" : t("form.nameHint"));
  return (
    <label>
      <span>{label}</span>
      <input
        ref={input}
        value={value}
        pattern={targetIdentifierPattern}
        title={t("form.nameHint")}
        maxLength={128}
        required
        spellCheck={false}
        autoComplete="off"
        placeholder={placeholder}
        disabled={disabled}
        aria-invalid={error === "" ? undefined : true}
        autoFocus={autoFocus}
        data-sheet-autofocus={autoFocus ? true : undefined}
        onChange={(event) => onChange(event.target.value)}
      />
      {error === "" ? (
        help === undefined ? null : <small>{help}</small>
      ) : (
        <small className="danger-text" role="alert">
          {error}
        </small>
      )}
    </label>
  );
}

export function Suggestions({
  id,
  values,
}: Readonly<{ id: string; values: readonly (string | undefined)[] }>) {
  const options = [...new Set(values.filter((value): value is string => Boolean(value)))].sort();
  return (
    <datalist id={id}>
      {options.map((value) => (
        <option key={value} value={value} />
      ))}
    </datalist>
  );
}

export function DurationSelect({
  label,
  value,
  options,
  onChange,
  disabled = false,
}: Readonly<{
  label: string;
  value: string;
  options: readonly number[];
  onChange: (value: string) => void;
  disabled?: boolean;
}>) {
  const { duration } = useI18n();
  return (
    <label>
      <span>{label}</span>
      <select value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)}>
        {options.map((seconds) => (
          <option key={seconds} value={String(seconds)}>
            {duration(seconds)}
          </option>
        ))}
      </select>
    </label>
  );
}

export function ChoiceList({
  name,
  legend,
  help,
  empty,
  options,
  selected,
  onChange,
}: Readonly<{
  name: string;
  legend: string;
  help?: string;
  empty: string;
  options: readonly Readonly<{ value: string; label: string; detail?: string }>[];
  selected: readonly string[];
  onChange: (selected: readonly string[]) => void;
}>) {
  return (
    <fieldset className="choice-list">
      <legend>{legend}</legend>
      {options.length === 0 ? <p className="field-help">{empty}</p> : null}
      {options.map((option) => (
        <label key={option.value} className="confirmation-check">
          <input
            type="checkbox"
            name={name}
            checked={selected.includes(option.value)}
            required={selected.length === 0}
            onChange={(event) =>
              onChange(
                event.target.checked
                  ? [...selected, option.value]
                  : selected.filter((value) => value !== option.value),
              )
            }
          />
          <span>
            {option.label}
            {option.detail ? <small>{option.detail}</small> : null}
          </span>
        </label>
      ))}
      {help ? <small>{help}</small> : null}
    </fieldset>
  );
}

export function AdvancedFields({ children }: Readonly<{ children: ReactNode }>) {
  const { t } = useI18n();
  return (
    <details className="form-advanced">
      <summary>{t("form.advanced")}</summary>
      <div className="resource-form-group">{children}</div>
    </details>
  );
}
