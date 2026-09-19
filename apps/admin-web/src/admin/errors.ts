import type { MessageKey } from "../i18n";

export class AdminUIError extends Error {
  constructor(readonly messageKey: MessageKey) {
    super(messageKey);
  }
}
