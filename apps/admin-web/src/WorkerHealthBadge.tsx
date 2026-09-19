import type { Worker } from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { useI18n } from "./i18n";

export function WorkerHealthBadge({ worker }: Readonly<{ worker: Worker }>) {
  const { t } = useI18n();
  const state = worker.spec.health?.state ?? "not-observed";
  return (
    <span
      className={`phase ${state === "online" ? "success" : state === "unavailable" ? "danger" : state === "expired" ? "running" : ""}`}
    >
      <i />
      {t(`worker.health.${state}`)}
    </span>
  );
}
