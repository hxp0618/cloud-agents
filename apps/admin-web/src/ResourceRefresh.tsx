import type { ReactNode } from "react";

export function ResourceRefresh({
  loading,
  label,
  columns,
  children,
}: Readonly<{
  loading: boolean;
  label: string;
  columns: readonly string[];
  children: ReactNode;
}>) {
  return (
    <section className="resource-refresh" aria-label={label} aria-busy={loading}>
      {loading ? (
        <div className="panel target-list-panel" aria-hidden="true">
          <div className="table-scroll">
            <table className="resource-skeleton">
              <thead>
                <tr>
                  {columns.map((column) => (
                    <th key={column}>{column}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {Array.from({ length: 6 }, (_, row) => (
                  <tr key={row}>
                    {columns.map((column) => (
                      <td key={column}>
                        <span className="skeleton" />
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ) : null}
      {/* Keep the last authority and its pagination mounted while the request is pending. */}
      <div className="resource-snapshot" hidden={loading}>
        {children}
      </div>
    </section>
  );
}
