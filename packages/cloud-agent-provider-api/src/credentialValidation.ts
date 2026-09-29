export type CredentialStringOptions = Readonly<{
  readonly allowNull?: boolean;
  readonly punctuation?: string;
  readonly singleLineMessage?: boolean;
}>;

export type CredentialBaseUrlOptions = CredentialStringOptions &
  Readonly<{
    readonly required?: boolean;
    readonly validateHttp?: boolean;
  }>;

export function optionalCredentialString(
  value: unknown,
  label: string,
  options: CredentialStringOptions = {},
): string | undefined {
  if (value === undefined || (value === null && options.allowNull !== false)) return undefined;
  if (typeof value !== "string" || !value.trim() || /[\r\n\0]/u.test(value)) {
    throw new Error(
      `${label} must be a non-empty ${options.singleLineMessage === false ? "string" : "single-line string"}${options.punctuation ?? ""}`,
    );
  }
  return value.trim();
}

export function requiredCredentialString(
  value: unknown,
  label: string,
  options: CredentialStringOptions = {},
): string {
  const result = optionalCredentialString(value, label, options);
  if (!result) throw new Error(`${label} is required${options.punctuation ?? ""}`);
  return result;
}

export function assertCredentialKeys(
  payload: Record<string, unknown>,
  allowed: ReadonlyArray<string>,
  label: string,
  punctuation = "",
): void {
  const allowedKeys = new Set(allowed);
  const extra = Object.keys(payload).find((key) => !allowedKeys.has(key));
  if (extra) throw new Error(`${label} contains unsupported field ${extra}${punctuation}`);
}

export function credentialBaseUrl(
  payload: Record<string, unknown>,
  label: string,
  options: CredentialBaseUrlOptions = {},
): string | undefined {
  const lower = optionalCredentialString(payload.baseUrl, `${label} baseUrl`, options);
  const upper = optionalCredentialString(payload.baseURL, `${label} baseURL`, options);
  if (lower && upper && lower !== upper) {
    throw new Error(
      `${label} contains conflicting baseUrl and baseURL values${options.punctuation ?? ""}`,
    );
  }
  const value = lower ?? upper;
  if (!value) {
    if (options.required) throw new Error(`${label} requires baseUrl${options.punctuation ?? ""}`);
    return undefined;
  }
  if (!options.validateHttp) return value;
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error(`${label} baseUrl protocol is unsupported${options.punctuation ?? ""}`);
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") {
    throw new Error(`${label} baseUrl protocol is unsupported${options.punctuation ?? ""}`);
  }
  return url.toString().replace(/\/$/u, "");
}
