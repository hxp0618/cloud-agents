import { useEffect, useState, type FormEvent } from "react";

import type {
  AgentApprovalInteraction,
  AgentUserInputInteraction,
  AgentUserInputQuestion,
} from "./agent";

type InteractionCardProps = Readonly<{
  interaction: AgentApprovalInteraction | AgentUserInputInteraction;
  active: boolean;
  disabled: boolean;
  resolved: boolean;
  onApproval: (decision: "accept" | "decline") => void;
  onUserInput: (answers: Readonly<Record<string, readonly string[]>>) => void;
}>;

function valuesFor(
  question: AgentUserInputQuestion,
  selected: Readonly<Record<string, readonly string[]>>,
  custom: Readonly<Record<string, string>>,
): readonly string[] {
  const values = [...(selected[question.id] ?? [])];
  const text = custom[question.id]?.trim();
  if (text) values.push(text);
  return values;
}

export function InteractionCard({
  interaction,
  active,
  disabled,
  resolved,
  onApproval,
  onUserInput,
}: InteractionCardProps) {
  const [selected, setSelected] = useState<Readonly<Record<string, readonly string[]>>>({});
  const [custom, setCustom] = useState<Readonly<Record<string, string>>>({});

  useEffect(() => {
    if (resolved) {
      setSelected({});
      setCustom({});
    }
  }, [resolved]);

  if (interaction.kind === "approval") {
    return (
      <article className="interaction-card approval-card">
        <header>
          <strong>Approval required</strong>
          <span>{resolved ? "Resolved" : active ? "Pending" : "Historical"}</span>
        </header>
        <p>{interaction.summary}</p>
        {interaction.details.length > 0 ? (
          <ul>
            {interaction.details.map((detail) => (
              <li key={detail}>{detail}</li>
            ))}
          </ul>
        ) : null}
        <InteractionAuthority interaction={interaction} />
        <div className="interaction-actions">
          <button
            className="button primary compact"
            type="button"
            disabled={disabled || resolved}
            onClick={() => onApproval("accept")}
          >
            Accept
          </button>
          <button
            className="button ghost compact danger-action"
            type="button"
            disabled={disabled || resolved}
            onClick={() => onApproval("decline")}
          >
            Decline
          </button>
        </div>
      </article>
    );
  }

  const answers = Object.fromEntries(
    interaction.questions.map((question) => [question.id, valuesFor(question, selected, custom)]),
  );
  const complete = Object.values(answers).every((values) => values.length > 0);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (complete) onUserInput(answers);
  }

  return (
    <form className="interaction-card user-input-card" onSubmit={submit}>
      <header>
        <strong>User input required</strong>
        <span>{resolved ? "Resolved" : active ? "Pending" : "Historical"}</span>
      </header>
      {interaction.questions.map((question) => {
        const selectedValues = selected[question.id] ?? [];
        return (
          <fieldset key={question.id} disabled={disabled || resolved}>
            <legend>{question.header}</legend>
            <p>{question.question}</p>
            {question.options.map((option) => (
              <label className="answer-option" key={option.label}>
                <input
                  type={question.multiSelect ? "checkbox" : "radio"}
                  name={`${interaction.requestId}:${question.id}`}
                  value={option.label}
                  checked={selectedValues.includes(option.label)}
                  onChange={(event) =>
                    setSelected((current) => {
                      const previous = current[question.id] ?? [];
                      const next = question.multiSelect
                        ? event.target.checked
                          ? [...previous, option.label]
                          : previous.filter((value) => value !== option.label)
                        : [option.label];
                      return { ...current, [question.id]: next };
                    })
                  }
                  onClick={() => {
                    if (!question.multiSelect)
                      setCustom((current) => ({ ...current, [question.id]: "" }));
                  }}
                />
                <span>
                  <strong>{option.label}</strong>
                  {option.description ? <small>{option.description}</small> : null}
                </span>
              </label>
            ))}
            {question.options.length === 0 || question.isOther ? (
              <label className="custom-answer">
                <span>{question.options.length === 0 ? "Answer" : "Other answer"}</span>
                <input
                  type={question.isSecret ? "password" : "text"}
                  autoComplete="off"
                  maxLength={2_000}
                  value={custom[question.id] ?? ""}
                  onChange={(event) => {
                    setCustom((current) => ({ ...current, [question.id]: event.target.value }));
                    if (!question.multiSelect && event.target.value !== "")
                      setSelected((current) => ({ ...current, [question.id]: [] }));
                  }}
                />
              </label>
            ) : null}
          </fieldset>
        );
      })}
      <InteractionAuthority interaction={interaction} />
      <div className="interaction-actions">
        <button
          className="button primary compact"
          type="submit"
          disabled={disabled || resolved || !complete}
        >
          Submit answers
        </button>
      </div>
    </form>
  );
}

function InteractionAuthority({
  interaction,
}: Readonly<{ interaction: AgentApprovalInteraction | AgentUserInputInteraction }>) {
  return (
    <dl className="interaction-authority">
      <div>
        <dt>Execution</dt>
        <dd>{interaction.executionId}</dd>
      </div>
      <div>
        <dt>Request</dt>
        <dd>{interaction.requestId}</dd>
      </div>
      <div>
        <dt>Generation</dt>
        <dd>{interaction.generation}</dd>
      </div>
    </dl>
  );
}
