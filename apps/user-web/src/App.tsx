import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  createHTTPClient,
  type Client,
  type Project,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  connectionErrorMessage,
  loadConnectionData,
  readSavedConnection,
  writeSavedConnection,
  type SavedConnection,
} from "./connection";

type ConnectionStatus = "disconnected" | "connecting" | "connected" | "error";

function initialConnection(): SavedConnection {
  const saved = readSavedConnection(window.sessionStorage);
  return {
    endpoint: saved.endpoint || window.location.origin,
    tenantId: saved.tenantId,
    projectId: saved.projectId,
  };
}

function statusLabel(status: ConnectionStatus): string {
  switch (status) {
    case "connected":
      return "Connected";
    case "connecting":
      return "Connecting";
    case "error":
      return "Connection failed";
    default:
      return "Disconnected";
  }
}

export function App() {
  const [connection, setConnection] = useState(initialConnection);
  const [token, setToken] = useState("");
  const [projects, setProjects] = useState<readonly Project[]>([]);
  const [tenantName, setTenantName] = useState("");
  const [status, setStatus] = useState<ConnectionStatus>("disconnected");
  const [error, setError] = useState("");
  const clientRef = useRef<Client | null>(null);
  const requestRef = useRef<AbortController | null>(null);
  const connectingRef = useRef(false);

  useEffect(() => () => requestRef.current?.abort(), []);

  const selectedProject = projects.find(({ metadata }) => metadata.uid === connection.projectId);
  const connected = status === "connected";

  function updateConnection(field: keyof SavedConnection, value: string) {
    setConnection((current) => ({ ...current, [field]: value }));
  }

  function disconnect() {
    requestRef.current?.abort();
    requestRef.current = null;
    clientRef.current = null;
    connectingRef.current = false;
    setToken("");
    setProjects([]);
    setTenantName("");
    setError("");
    setStatus("disconnected");
  }

  async function connect(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (connectingRef.current) return;

    const endpoint = connection.endpoint.trim().replace(/\/+$/u, "");
    const tenantId = connection.tenantId.trim();
    const bearerToken = token.trim();
    if (endpoint === "" || tenantId === "" || bearerToken === "") {
      setStatus("error");
      setError("Endpoint, tenant, and bearer token are required.");
      return;
    }

    connectingRef.current = true;
    setStatus("connecting");
    setError("");
    const controller = new AbortController();
    requestRef.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);

    try {
      const client = createHTTPClient(endpoint, bearerToken);
      const data = await loadConnectionData(client, tenantId, signal);
      const activeProjects = data.projects.filter(({ spec }) => spec.state === "active");
      const projectId = activeProjects.some(({ metadata }) => metadata.uid === connection.projectId)
        ? connection.projectId
        : (activeProjects[0]?.metadata.uid ?? "");
      const nextConnection = { endpoint, tenantId, projectId };

      clientRef.current = client;
      setProjects(data.projects);
      setTenantName(data.tenant.spec.displayName);
      setConnection(nextConnection);
      writeSavedConnection(window.sessionStorage, nextConnection);
      setToken("");
      setStatus("connected");
    } catch (cause) {
      clientRef.current = null;
      setStatus(controller.signal.aborted ? "disconnected" : "error");
      setError(controller.signal.aborted ? "" : connectionErrorMessage(cause));
    } finally {
      if (requestRef.current === controller) requestRef.current = null;
      connectingRef.current = false;
    }
  }

  function selectProject(projectId: string) {
    const nextConnection = { ...connection, projectId };
    setConnection(nextConnection);
    writeSavedConnection(window.sessionStorage, nextConnection);
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand" aria-label="Cloud Agents Console">
          <span className="brand-mark" aria-hidden="true">
            CA
          </span>
          <span>
            <strong>Cloud Agents</strong>
            <small>User Console</small>
          </span>
        </div>
        <div className="context-strip" aria-label="Current Control Plane context">
          <span className="context-item">
            <small>Control Plane</small>
            <strong title={connection.endpoint}>{connection.endpoint || "Not configured"}</strong>
          </span>
          <span className="context-item">
            <small>Tenant</small>
            <strong>{tenantName || connection.tenantId || "Not selected"}</strong>
          </span>
          <label className="context-item project-picker">
            <small>Project</small>
            <select
              aria-label="Current project"
              value={connection.projectId}
              disabled={!connected || projects.length === 0}
              onChange={(event) => selectProject(event.target.value)}
            >
              {projects.length === 0 ? (
                <option value={connection.projectId}>
                  {connection.projectId ? `Saved: ${connection.projectId}` : "No active project"}
                </option>
              ) : null}
              {projects.map((project) => (
                <option
                  key={project.metadata.uid}
                  value={project.metadata.uid}
                  disabled={project.spec.state !== "active"}
                >
                  {project.spec.displayName} · {project.spec.state}
                </option>
              ))}
            </select>
          </label>
        </div>
        <div className="connection-actions">
          <span className={`connection-state state-${status}`} role="status" aria-live="polite">
            <span className="status-dot" aria-hidden="true" />
            {statusLabel(status)}
          </span>
          {connected ? (
            <button className="button ghost compact" type="button" onClick={disconnect}>
              Disconnect
            </button>
          ) : null}
        </div>
      </header>

      {connected ? (
        <div className="workspace">
          <aside className="left-rail" aria-label="Targets and leases">
            <section className="panel rail-section">
              <div className="panel-heading">
                <span>
                  <small>Infrastructure</small>
                  <h2>Deployment Targets</h2>
                </span>
                <button className="icon-button" type="button" disabled aria-label="Register target">
                  +
                </button>
              </div>
              <div className="empty-state compact-empty">
                <span className="empty-glyph">01</span>
                <strong>No targets loaded</strong>
                <p>Register and probe Docker, Kubernetes, or SSH in the next setup step.</p>
              </div>
            </section>
            <section className="panel rail-section lease-section">
              <div className="panel-heading">
                <span>
                  <small>Runtime</small>
                  <h2>Environment Leases</h2>
                </span>
              </div>
              <div className="empty-row">
                <span className="status-dot neutral" aria-hidden="true" />
                No active lease
              </div>
            </section>
          </aside>

          <main className="conversation panel">
            <div className="conversation-toolbar">
              <div>
                <small>Agent workspace</small>
                <h1>{selectedProject?.spec.displayName ?? "Select a project"}</h1>
              </div>
              <div className="toolbar-controls">
                <label>
                  <span>Provider</span>
                  <select aria-label="Agent provider" disabled>
                    <option>Codex</option>
                    <option>Claude Code</option>
                  </select>
                </label>
                <button className="button secondary compact" type="button" disabled>
                  New session
                </button>
              </div>
            </div>
            <div className="conversation-empty">
              <div className="agent-orbit" aria-hidden="true">
                <span>CA</span>
              </div>
              <strong>Infrastructure first, then a real turn</strong>
              <p>Choose a target and ready lease before starting a Codex or Claude Code session.</p>
              <div className="phase-line" aria-label="Agent workflow">
                <span className="complete">Control Plane</span>
                <span>Target</span>
                <span>Lease</span>
                <span>Session</span>
              </div>
            </div>
            <form className="prompt-bar" aria-label="Agent prompt">
              <label className="sr-only" htmlFor="prompt">
                Prompt
              </label>
              <textarea
                id="prompt"
                placeholder="A ready lease is required before sending a turn"
                rows={2}
                disabled
              />
              <button className="button primary" type="submit" disabled>
                Send
              </button>
            </form>
          </main>

          <aside className="activity panel" aria-label="Activity and interactions">
            <div className="panel-heading activity-heading">
              <span>
                <small>Live state</small>
                <h2>Activity</h2>
              </span>
              <span className="live-badge">Idle</span>
            </div>
            <dl className="status-table">
              <div>
                <dt>Target</dt>
                <dd>Not selected</dd>
              </div>
              <div>
                <dt>Lease</dt>
                <dd>Not created</dd>
              </div>
              <div>
                <dt>Worker</dt>
                <dd>Offline</dd>
              </div>
              <div>
                <dt>Execution</dt>
                <dd>Idle</dd>
              </div>
            </dl>
            <div className="timeline-empty">
              <span className="timeline-rule" aria-hidden="true" />
              <strong>Event timeline</strong>
              <p>Agent messages, tool events, approvals, and user input will appear here.</p>
            </div>
          </aside>
        </div>
      ) : (
        <main className="connect-view">
          <section className="connect-card panel" aria-labelledby="connect-title">
            <div className="eyebrow">Secure browser connection</div>
            <h1 id="connect-title">Connect to Control Plane</h1>
            <p className="lede">
              Select a tenant and project without storing your bearer token in browser storage.
            </p>
            <form onSubmit={connect} className="connect-form">
              <label>
                <span>Control Plane endpoint</span>
                <input
                  type="url"
                  value={connection.endpoint}
                  onChange={(event) => updateConnection("endpoint", event.target.value)}
                  placeholder="https://agents.example.com"
                  autoComplete="url"
                  required
                  disabled={status === "connecting"}
                />
                <small>Use this page origin when `/v1` is reverse proxied.</small>
              </label>
              <label>
                <span>Tenant ID</span>
                <input
                  value={connection.tenantId}
                  onChange={(event) => updateConnection("tenantId", event.target.value)}
                  placeholder="tenant-local"
                  autoComplete="off"
                  spellCheck={false}
                  required
                  disabled={status === "connecting"}
                />
              </label>
              <label>
                <span>Bearer token</span>
                <input
                  type="password"
                  value={token}
                  onChange={(event) => setToken(event.target.value)}
                  placeholder="Kept in memory only"
                  autoComplete="off"
                  spellCheck={false}
                  required
                  disabled={status === "connecting"}
                />
                <small>Cleared from the form after connection and never written to storage.</small>
              </label>
              {error ? (
                <div className="error-banner" role="alert">
                  <strong>Connection failed</strong>
                  <span>{error}</span>
                </div>
              ) : null}
              <div className="form-actions">
                <button className="button primary" type="submit" disabled={status === "connecting"}>
                  {status === "connecting" ? "Validating tenant and loading projects…" : "Connect"}
                </button>
                {status === "connecting" ? (
                  <button className="button ghost" type="button" onClick={disconnect}>
                    Cancel
                  </button>
                ) : null}
              </div>
            </form>
          </section>

          <aside className="connection-notes" aria-label="Connection behavior">
            <div className="ambient-card">
              <span className="note-index">01</span>
              <strong>Server authority</strong>
              <p>Projects are reloaded from Control Plane after every browser connection.</p>
            </div>
            <div className="ambient-card">
              <span className="note-index">02</span>
              <strong>Memory-only token</strong>
              <p>
                Reloading the page requires the bearer token again; non-secret context is restored.
              </p>
            </div>
            <div className="ambient-card">
              <span className="note-index">03</span>
              <strong>Bounded requests</strong>
              <p>Connection work can be cancelled and times out after 15 seconds.</p>
            </div>
          </aside>
        </main>
      )}
    </div>
  );
}
