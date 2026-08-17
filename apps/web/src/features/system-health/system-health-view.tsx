import type {
  HealthComponent,
  HealthStatus,
  SystemHealthReport,
} from "./types";

const statusContent: Record<HealthStatus, { heading: string; label: string }> =
  {
    healthy: { heading: "All systems operational", label: "Healthy" },
    degraded: { heading: "Some systems need attention", label: "Degraded" },
    unavailable: { heading: "Health status unavailable", label: "Unavailable" },
  };

export function SystemHealthView({
  report,
}: {
  report: SystemHealthReport;
}): React.JSX.Element {
  const content = statusContent[report.status];
  const components: Array<{ name: string; component: HealthComponent }> = [
    {
      name: "Web",
      component: { status: "healthy", checkedAt: report.checkedAt },
    },
    { name: "API", component: report.components.api },
    { name: "PostgreSQL", component: report.components.database },
    { name: "Worker", component: report.components.worker },
  ];

  return (
    <main className="system-health" data-system-status={report.status}>
      <header>
        <p className="eyebrow">System health</p>
        <h1>{content.heading}</h1>
        <p>
          <span className={`status-badge status-${report.status}`}>
            {content.label}
          </span>
        </p>
        <p>
          Checked{" "}
          <time dateTime={report.checkedAt}>
            {new Date(report.checkedAt).toLocaleString()}
          </time>
        </p>
      </header>
      <section aria-label="Service status" className="health-grid">
        {components.map(({ name, component }) => (
          <article className="health-card" key={name}>
            <h2>{name}</h2>
            <p>
              <span className={`status-badge status-${component.status}`}>
                {component.status === "healthy" ? "Available" : "Unavailable"}
              </span>
            </p>
            <p>
              Updated{" "}
              <time dateTime={component.checkedAt}>
                {new Date(component.checkedAt).toLocaleString()}
              </time>
            </p>
          </article>
        ))}
      </section>
    </main>
  );
}
